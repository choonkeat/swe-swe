package main

import (
	"testing"
	"time"
)

// Regression: Close() holds s.mu across its teardown section and used to call
// clearVhostPin(), which takes s.mu again. Go mutexes are not reentrant, so
// every session end self-deadlocked (goroutine parked forever holding s.mu),
// and -- because sessionReaper reads reapable() under sessionsMu -- one such
// session wedged the global lock and hung the whole server.
func TestCloseWithVhostPinReturns(t *testing.T) {
	s := &Session{UUID: "vhost-pin-close"}
	s.setVhostPin("app1", 5000)

	done := make(chan struct{})
	go func() {
		s.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Session.Close deadlocked with a vhost pin set (clearVhostPin re-locks s.mu)")
	}

	if s.getVhostPin() != nil {
		t.Fatal("Close must clear the vhost pin")
	}
}
