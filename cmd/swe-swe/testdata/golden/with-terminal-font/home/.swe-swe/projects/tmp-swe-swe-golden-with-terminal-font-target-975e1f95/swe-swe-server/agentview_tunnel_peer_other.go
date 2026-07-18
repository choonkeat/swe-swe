//go:build !linux

package main

import "net"

// tunnelPeerGuard fails OPEN off Linux, mirroring the broker's stance: the
// production backend image is Linux (where the /proc-based guard is always
// on); non-Linux builds exist only for local development.
func tunnelPeerGuard(sess *backendSession, conn net.Conn) error {
	return nil
}
