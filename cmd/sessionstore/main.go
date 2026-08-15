// Command sessionstore writes a short-lived session into an in-memory
// store and reads it back, demonstrating the session package end to end.
package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"example.com/vol07/session"
)

// demoTTL keeps the written session deliberately short-lived.
const demoTTL = 5 * time.Second

func run(w io.Writer) error {
	store := session.New()

	sess, err := store.Create([]byte("hello session"), demoTTL)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	fmt.Fprintf(w, "created id=%s ttl=%s expires=%s\n",
		sess.ID, demoTTL, sess.ExpiresAt.Format(time.RFC3339))

	payload, ok := store.Get(sess.ID)
	if !ok {
		return fmt.Errorf("session %s not readable right after create", sess.ID)
	}
	fmt.Fprintf(w, "read id=%s payload=%q\n", sess.ID, payload)
	return nil
}

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "sessionstore:", err)
		os.Exit(1)
	}
}
