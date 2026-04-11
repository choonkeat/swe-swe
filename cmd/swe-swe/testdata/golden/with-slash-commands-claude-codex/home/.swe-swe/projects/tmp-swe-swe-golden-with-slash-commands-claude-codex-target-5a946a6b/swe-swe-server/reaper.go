package main

import (
	"bytes"
	"log"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// trackedPids holds pids that some Go *exec.Cmd will eventually call Wait()
// on. The orphan reaper MUST skip these -- wait4'ing them would race with
// cmd.Wait() and yield ECHILD with ProcessState unset (the bug commit
// 3e89e8cf2 worked around by deleting the SIGCHLD reaper entirely).
var trackedPids sync.Map // map[int]struct{}

// trackedPidCount tracks the size of trackedPids without iterating it.
var trackedPidCount int64

// trackPid registers a pid as owned by a Go cmd.Wait() caller.  The orphan
// reaper will not call wait4 on this pid.  Safe to call with pid=0 (no-op).
func trackPid(pid int) {
	if pid <= 0 {
		return
	}
	if _, loaded := trackedPids.LoadOrStore(pid, struct{}{}); !loaded {
		atomic.AddInt64(&trackedPidCount, 1)
	}
}

// untrackPid removes a pid from the tracked set.  Call right after the
// owning cmd.Wait() returns.  Safe to call with pid=0 (no-op) and idempotent.
func untrackPid(pid int) {
	if pid <= 0 {
		return
	}
	if _, loaded := trackedPids.LoadAndDelete(pid); loaded {
		atomic.AddInt64(&trackedPidCount, -1)
	}
}

// orphanReaperInterval controls how often startOrphanReaper scans /proc.
// 5s is a comfortable cadence: orphans don't need millisecond reaping, and
// the bound on accumulated zombies is "however many die per 5s window".
var orphanReaperInterval = 5 * time.Second

// orphanReaperMetricEvery controls how often a [REAPER] metric line is
// logged.  12 * 5s = 60s.  Logs current zombie count for observability so
// future leaks are visible without grepping ps.
const orphanReaperMetricEvery = 12

// startOrphanReaper polls /proc every orphanReaperInterval for processes in
// state Z whose PPid is us, and reaps any that aren't in trackedPids.
// Skipping tracked pids guarantees we never collide with a Go cmd.Wait()
// in flight, which was the original race that motivated removing the
// SIGCHLD-driven wildcard reaper.
func startOrphanReaper() {
	go func() {
		defer recoverGoroutine("orphan reaper")
		t := time.NewTicker(orphanReaperInterval)
		defer t.Stop()
		self := os.Getpid()
		tick := 0
		for range t.C {
			reaped, zombies := reapOrphansOnce(self)
			tick++
			if reaped > 0 {
				log.Printf("[REAPER] tick reaped %d orphans (zombies_remaining=%d tracked=%d)",
					reaped, zombies-reaped, atomic.LoadInt64(&trackedPidCount))
			}
			if tick%orphanReaperMetricEvery == 0 {
				log.Printf("[REAPER] metric: zombies=%d tracked_pids=%d",
					zombies, atomic.LoadInt64(&trackedPidCount))
			}
		}
	}()
	log.Printf("[REAPER] orphan reaper started (interval=%s)", orphanReaperInterval)
}

// reapOrphansOnce scans /proc once.  Returns (reaped, zombieCandidates) where
// zombieCandidates is the total number of Z-state processes parented to self
// that were considered (including any that were skipped because they were in
// trackedPids).
func reapOrphansOnce(self int) (reaped, zombies int) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, 0
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		data, err := os.ReadFile("/proc/" + e.Name() + "/status")
		if err != nil {
			// Race with the process exiting between ReadDir and ReadFile;
			// harmless -- it'll be gone by next tick or already reaped.
			continue
		}
		if !bytes.Contains(data, []byte("State:\tZ")) {
			continue
		}
		if parsePPid(data) != self {
			continue
		}
		zombies++
		if _, tracked := trackedPids.Load(pid); tracked {
			// A Go cmd.Wait() owns this pid -- do not race with it.
			continue
		}
		var ws syscall.WaitStatus
		rpid, err := syscall.Wait4(pid, &ws, syscall.WNOHANG, nil)
		if err != nil {
			// ECHILD is expected if another goroutine grabbed it first
			// (e.g., a tracked-set member that hadn't been registered yet).
			// Anything else is worth a one-line note.
			if !errIsECHILD(err) {
				log.Printf("[REAPER] wait4 pid=%d failed: %v", pid, err)
			}
			continue
		}
		if rpid > 0 {
			reaped++
			log.Printf("[REAPER] reaped orphan pid=%d exit=%d sig=%v",
				rpid, ws.ExitStatus(), ws.Signal())
		}
	}
	return reaped, zombies
}

// parsePPid extracts the integer following "PPid:\t" from /proc/<pid>/status.
// Returns 0 if absent or unparseable.
func parsePPid(status []byte) int {
	const key = "PPid:\t"
	i := bytes.Index(status, []byte(key))
	if i < 0 {
		return 0
	}
	rest := status[i+len(key):]
	end := bytes.IndexByte(rest, '\n')
	if end < 0 {
		end = len(rest)
	}
	v, _ := strconv.Atoi(string(bytes.TrimSpace(rest[:end])))
	return v
}

// errIsECHILD reports whether err is or wraps syscall.ECHILD.
func errIsECHILD(err error) bool {
	if err == nil {
		return false
	}
	if errno, ok := err.(syscall.Errno); ok {
		return errno == syscall.ECHILD
	}
	return false
}
