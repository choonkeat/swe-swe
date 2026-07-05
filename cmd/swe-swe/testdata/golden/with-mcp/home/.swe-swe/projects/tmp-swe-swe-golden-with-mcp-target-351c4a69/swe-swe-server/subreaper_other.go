//go:build !linux

package main

import "log"

// startSubreaper is a no-op off Linux. PR_SET_CHILD_SUBREAPER and the /proc
// orphan reaper (reaper.go) are Linux-specific. On macOS, orphaned descendants
// reparent to launchd, which reaps them, so the server does not accumulate
// zombies -- but it also cannot adopt orphans for its own cleanup. Acceptable
// for Phase 6 (Mac-native dockerless); revisit with a kqueue-based reaper if
// orphan tracking proves necessary.
func startSubreaper() {
	log.Printf("[SUBREAPER] not supported on this platform -- skipping (orphans reparent to the OS init)")
}
