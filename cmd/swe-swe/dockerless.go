package main

import "fmt"

// dockerlessGOOSGuard reports whether a dockerless init is allowed for the
// given GOOS. The embedded payload binaries are static-Linux only (Phase 1),
// and the server still relies on two Linux-only couplings -- the abstract
// unix socket `@swe-swe-broker` and util-linux `script -T/-I/-O` recording
// flags -- so dumping them on macOS/Windows would produce a broken setup.
// Mac-native dockerless (darwin binaries + portable couplings) is Phase 6.
func dockerlessGOOSGuard(goos string) error {
	if goos != "linux" {
		return fmt.Errorf("swe-swe init --dockerless is supported on a Linux host only for now (this is a %s build); use Docker mode here, or see Phase 6 for native macOS support", goos)
	}
	return nil
}
