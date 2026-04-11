package main

import (
	"sync/atomic"
	"testing"
)

func TestParsePPid(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"basic", "Name:\tfoo\nPPid:\t201\nState:\tZ\n", 201},
		{"first_line", "PPid:\t1\n", 1},
		{"missing", "Name:\tfoo\nState:\tR\n", 0},
		{"empty", "", 0},
		{"unparseable", "PPid:\tabc\n", 0},
		{"trailing_no_newline", "PPid:\t42", 42},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parsePPid([]byte(c.in)); got != c.want {
				t.Errorf("parsePPid(%q) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}

func TestTrackPidLifecycle(t *testing.T) {
	// Snapshot then restore counter so this test can run alongside others.
	startCount := atomic.LoadInt64(&trackedPidCount)
	defer func() {
		atomic.StoreInt64(&trackedPidCount, startCount)
		// Best-effort cleanup of any leftover entries from this test.
		trackedPids.Range(func(k, _ any) bool {
			if pid, ok := k.(int); ok && (pid == 91111 || pid == 91112) {
				trackedPids.Delete(k)
			}
			return true
		})
	}()

	// pid<=0 must be a no-op for both track and untrack.
	trackPid(0)
	trackPid(-1)
	if got := atomic.LoadInt64(&trackedPidCount); got != startCount {
		t.Fatalf("trackPid(<=0) changed count: got %d, want %d", got, startCount)
	}
	untrackPid(0)
	untrackPid(-7)

	// Idempotent track.
	trackPid(91111)
	trackPid(91111)
	if got := atomic.LoadInt64(&trackedPidCount); got != startCount+1 {
		t.Fatalf("double trackPid did not stay at +1: got %d, want %d", got, startCount+1)
	}
	if _, ok := trackedPids.Load(91111); !ok {
		t.Fatalf("trackPid(91111) not in map")
	}

	// Add a second.
	trackPid(91112)
	if got := atomic.LoadInt64(&trackedPidCount); got != startCount+2 {
		t.Fatalf("second trackPid count wrong: got %d, want %d", got, startCount+2)
	}

	// Untrack one, idempotent.
	untrackPid(91111)
	untrackPid(91111)
	if got := atomic.LoadInt64(&trackedPidCount); got != startCount+1 {
		t.Fatalf("double untrackPid count wrong: got %d, want %d", got, startCount+1)
	}
	if _, ok := trackedPids.Load(91111); ok {
		t.Fatalf("trackPid(91111) still in map after untrack")
	}

	untrackPid(91112)
	if got := atomic.LoadInt64(&trackedPidCount); got != startCount {
		t.Fatalf("final untrackPid count wrong: got %d, want %d", got, startCount)
	}
}

func TestReapOrphansOnceSelfPid(t *testing.T) {
	// Pid 1 is never us in a normal test environment, so reapOrphansOnce(1)
	// should report no zombies (nothing in /proc has PPid==1 in this binary's
	// process tree, assuming the test isn't running as PID 1).  This is a
	// smoke test that the function returns without panicking and respects
	// the PPid filter.
	reaped, zombies := reapOrphansOnce(1)
	if reaped < 0 || zombies < 0 {
		t.Fatalf("negative counts: reaped=%d zombies=%d", reaped, zombies)
	}
	// Cannot assert reaped==0 hard because a real PID 1 child might exist
	// on a CI runner; but no panic and non-negative is the contract.
}
