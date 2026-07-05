//go:build linux

package main

import (
	"net"
	"syscall"
)

// peerPID returns the kernel-reported pid of the connection's peer via
// SO_PEERCRED. Cannot be forged by the peer. Linux-only: SO_PEERCRED gives the
// peer's pid, which the broker walks through /proc to map to a session.
func peerPID(c *net.UnixConn) (int, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return 0, err
	}
	var ucred *syscall.Ucred
	var sockErr error
	ctlErr := raw.Control(func(fd uintptr) {
		ucred, sockErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	})
	if ctlErr != nil {
		return 0, ctlErr
	}
	if sockErr != nil {
		return 0, sockErr
	}
	return int(ucred.Pid), nil
}
