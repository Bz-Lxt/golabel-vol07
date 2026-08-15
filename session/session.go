// Package session provides an in-memory, concurrency-safe session store
// with time-based expiration.
package session

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// DefaultLocation is the timezone used for session timestamps. It resolves
// to Asia/Shanghai and falls back to a fixed GMT+8 zone when the host has
// no timezone database installed.
var DefaultLocation = loadDefaultLocation()

func loadDefaultLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("GMT+8", 8*60*60)
	}
	return loc
}

// ErrInvalidTTL is returned when a non-positive TTL is supplied.
var ErrInvalidTTL = errors.New("session: ttl must be positive")

// ErrIDCollision is returned when the id generator keeps producing
// identifiers that already exist in the store.
var ErrIDCollision = errors.New("session: generated id already exists")

// maxIDAttempts bounds how many times Create retries id generation before
// reporting a collision.
const maxIDAttempts = 8

// Session is a snapshot of a stored session.
type Session struct {
	ID        string
	Payload   []byte
	CreatedAt time.Time
	ExpiresAt time.Time
}

// entry is the internal mutable record held by the store.
type entry struct {
	payload   []byte
	createdAt time.Time
	expiresAt time.Time
}

// Clock reports the current time. It can be replaced in tests.
type Clock func() time.Time

// IDGenerator produces a new session identifier.
type IDGenerator func() (string, error)

// Option configures a Store.
type Option func(*Store)

// WithClock overrides the time source used by the store.
func WithClock(c Clock) Option {
	return func(s *Store) {
		if c != nil {
			s.now = c
		}
	}
}

// WithIDGenerator overrides the identifier generator used by Create.
func WithIDGenerator(g IDGenerator) Option {
	return func(s *Store) {
		if g != nil {
			s.newID = g
		}
	}
}

// Store is an in-memory session store safe for concurrent use.
type Store struct {
	mu       sync.RWMutex
	now      Clock
	newID    IDGenerator
	sessions map[string]entry
}

// New returns an empty Store. Unless a custom clock is supplied, timestamps
// are recorded in the Asia/Shanghai timezone.
func New(opts ...Option) *Store {
	s := &Store{
		now:      func() time.Time { return time.Now().In(DefaultLocation) },
		newID:    randomID,
		sessions: make(map[string]entry),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Create stores payload under a freshly generated identifier that expires
// after ttl. The payload is copied, so the caller keeps ownership of the
// slice it passed in. It returns a snapshot of the stored session.
func (s *Store) Create(payload []byte, ttl time.Duration) (Session, error) {
	if ttl <= 0 {
		return Session{}, ErrInvalidTTL
	}
	now := s.now()
	stored := append([]byte(nil), payload...)

	s.mu.Lock()
	defer s.mu.Unlock()
	for attempt := 0; attempt < maxIDAttempts; attempt++ {
		id, err := s.newID()
		if err != nil {
			return Session{}, fmt.Errorf("session: generate id: %w", err)
		}
		if _, exists := s.sessions[id]; exists {
			continue
		}
		s.sessions[id] = entry{
			payload:   stored,
			createdAt: now,
			expiresAt: now.Add(ttl),
		}
		return Session{
			ID:        id,
			Payload:   stored,
			CreatedAt: now,
			ExpiresAt: now.Add(ttl),
		}, nil
	}
	return Session{}, ErrIDCollision
}

// Get returns a copy of the payload stored under id. The second return
// value is false when the session is missing or has expired; expired
// payloads are never returned.
func (s *Store) Get(id string) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.sessions[id]
	if !ok || expired(e, s.now()) {
		return nil, false
	}
	return append([]byte(nil), e.payload...), true
}

// Touch extends the lifetime of an existing, unexpired session so it
// expires ttl from now. It reports whether the session was found and
// renewed. Expired or unknown sessions are left untouched.
func (s *Store) Touch(id string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		return false, ErrInvalidTTL
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.sessions[id]
	now := s.now()
	if !ok || expired(e, now) {
		return false, nil
	}
	e.expiresAt = now.Add(ttl)
	s.sessions[id] = e
	return true, nil
}

// Revoke removes the session under id, making it immediately unreadable.
// Revoking an unknown id is a no-op.
func (s *Store) Revoke(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

// List returns the identifiers of all sessions that have not expired yet,
// in sorted order. The returned slice is a fresh copy owned by the caller.
func (s *Store) List() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := s.now()
	ids := make([]string, 0, len(s.sessions))
	for id, e := range s.sessions {
		if expired(e, now) {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// expired reports whether e has reached its expiry instant at time now.
func expired(e entry, now time.Time) bool {
	return !now.Before(e.expiresAt)
}

// randomID returns a 128-bit random identifier encoded as hex.
func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
