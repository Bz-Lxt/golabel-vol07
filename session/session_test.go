package session

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"
)

// fakeClock is a manually advanced Clock for deterministic expiry tests.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 8, 15, 12, 0, 0, 0, DefaultLocation)}
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

func TestCreateAndGet(t *testing.T) {
	s := New()
	payload := []byte(`{"user":"alice"}`)

	sess, err := s.Create(payload, time.Minute)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sess.ID == "" {
		t.Fatal("Create returned empty id")
	}
	if got := time.Until(sess.ExpiresAt); got <= 0 || got > time.Minute {
		t.Fatalf("ExpiresAt %v not within ttl from now", sess.ExpiresAt)
	}

	// Mutating the caller's slice must not change the stored payload.
	payload[0] = 'X'

	got, ok := s.Get(sess.ID)
	if !ok {
		t.Fatalf("Get(%q): not found", sess.ID)
	}
	if string(got) != `{"user":"alice"}` {
		t.Fatalf("Get payload = %q", got)
	}

	// The returned payload is a copy; mutating it must not leak back.
	got[0] = 'Y'
	again, ok := s.Get(sess.ID)
	if !ok || string(again) != `{"user":"alice"}` {
		t.Fatalf("stored payload changed by caller, got %q", again)
	}
}

func TestCreateRejectsNonPositiveTTL(t *testing.T) {
	s := New()
	for _, ttl := range []time.Duration{0, -time.Second} {
		if _, err := s.Create([]byte("x"), ttl); !errors.Is(err, ErrInvalidTTL) {
			t.Fatalf("Create(ttl=%v) error = %v, want ErrInvalidTTL", ttl, err)
		}
	}
}

func TestGetTreatsExpiredAsMissing(t *testing.T) {
	clock := newFakeClock()
	s := New(WithClock(clock.Now))

	sess, err := s.Create([]byte("secret"), 10*time.Second)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	clock.Advance(11 * time.Second)

	if got, ok := s.Get(sess.ID); ok {
		t.Fatalf("Get returned expired payload %q", got)
	}
	if ids := s.List(); len(ids) != 0 {
		t.Fatalf("List after expiry = %v, want empty", ids)
	}
}

func TestTouchExtendsOnlyLiveSessions(t *testing.T) {
	clock := newFakeClock()
	s := New(WithClock(clock.Now))

	sess, err := s.Create([]byte("data"), 30*time.Second)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	clock.Advance(20 * time.Second)
	ok, err := s.Touch(sess.ID, 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("Touch = (%v, %v), want (true, nil)", ok, err)
	}

	// Past the original expiry, but still within the renewed window.
	clock.Advance(20 * time.Second)
	if _, ok := s.Get(sess.ID); !ok {
		t.Fatal("Get after Touch: session lost before renewed expiry")
	}

	// Once expired, Touch must not resurrect the session.
	clock.Advance(31 * time.Second)
	ok, err = s.Touch(sess.ID, time.Minute)
	if err != nil || ok {
		t.Fatalf("Touch expired = (%v, %v), want (false, nil)", ok, err)
	}
	if _, ok := s.Get(sess.ID); ok {
		t.Fatal("expired session readable after Touch")
	}

	if _, err := s.Touch("missing", time.Minute); err != nil {
		t.Fatalf("Touch missing id error = %v", err)
	}
	if _, err := s.Touch(sess.ID, 0); !errors.Is(err, ErrInvalidTTL) {
		t.Fatalf("Touch(ttl=0) error = %v, want ErrInvalidTTL", err)
	}
}

func TestRevokeMakesSessionUnreadable(t *testing.T) {
	s := New()
	sess, err := s.Create([]byte("bye"), time.Minute)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	s.Revoke(sess.ID)

	if _, ok := s.Get(sess.ID); ok {
		t.Fatal("Get after Revoke: session still readable")
	}
	if ids := s.List(); len(ids) != 0 {
		t.Fatalf("List after Revoke = %v, want empty", ids)
	}

	// Revoking twice and revoking unknown ids must be harmless.
	s.Revoke(sess.ID)
	s.Revoke("never-existed")
}

func TestListReturnsOnlyLiveIDsAsCopy(t *testing.T) {
	clock := newFakeClock()
	s := New(WithClock(clock.Now))

	live, err := s.Create([]byte("a"), time.Minute)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	short, err := s.Create([]byte("b"), 5*time.Second)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	clock.Advance(10 * time.Second)

	ids := s.List()
	want := []string{live.ID}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("List = %v, want %v (short-lived id %q must be filtered)", ids, want, short.ID)
	}

	// Mutating the returned slice must not affect the store.
	ids[0] = "tampered"
	if got := s.List(); !reflect.DeepEqual(got, want) {
		t.Fatalf("List after caller mutation = %v, want %v", got, want)
	}
}

func TestCreateWithDeterministicIDGenerator(t *testing.T) {
	s := New(WithIDGenerator(func() (string, error) { return "fixed-id", nil }))
	if _, err := s.Create([]byte("x"), time.Minute); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := s.Create([]byte("y"), time.Minute); !errors.Is(err, ErrIDCollision) {
		t.Fatalf("second Create error = %v, want ErrIDCollision", err)
	}
}

func TestCreatePropagatesIDGeneratorError(t *testing.T) {
	boom := errors.New("boom")
	s := New(WithIDGenerator(func() (string, error) { return "", boom }))
	if _, err := s.Create([]byte("x"), time.Minute); !errors.Is(err, boom) {
		t.Fatalf("Create error = %v, want wrapped %v", err, boom)
	}
}

func TestDefaultLocationIsGMTPlus8(t *testing.T) {
	_, offset := time.Now().In(DefaultLocation).Zone()
	if offset != 8*60*60 {
		t.Fatalf("DefaultLocation offset = %d, want %d", offset, 8*60*60)
	}
}

func TestConcurrentAccess(t *testing.T) {
	s := New()

	ids := make([]string, 8)
	for i := range ids {
		sess, err := s.Create([]byte("seed"), time.Minute)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		ids[i] = sess.ID
	}

	var wg sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				id := ids[(n+i)%len(ids)]
				switch i % 5 {
				case 0:
					s.Get(id)
				case 1:
					if _, err := s.Touch(id, time.Minute); err != nil {
						t.Errorf("Touch: %v", err)
					}
				case 2:
					s.List()
				case 3:
					if _, err := s.Create([]byte(fmt.Sprintf("w%d-i%d", n, i)), time.Minute); err != nil {
						t.Errorf("Create: %v", err)
					}
				case 4:
					s.Revoke(id)
				}
			}
		}(worker)
	}
	wg.Wait()
}
