//go:build linux

package main

// Linux peer-identification tests for the tunnel's accept guard: a real TCP
// loopback pair where the test process is both peer and allowed ancestor.

import (
	"net"
	"os"
	"testing"
)

func TestFindTCPPeerPIDSelfConnection(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	pid, err := findTCPPeerPID(server.RemoteAddr())
	if err != nil {
		t.Fatalf("findTCPPeerPID: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("peer pid = %d, want own pid %d", pid, os.Getpid())
	}

	// Ancestry: the test process stands in for the chromium tree root.
	if !pidHasAncestorIn(pid, map[int]bool{os.Getpid(): true}) {
		t.Error("self pid not accepted as its own ancestor")
	}
	if pidHasAncestorIn(pid, map[int]bool{999999999: true}) {
		t.Error("bogus root accepted as ancestor")
	}
}

func TestFindTCPPeerPIDIPv6SelfConnection(t *testing.T) {
	ln, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("no IPv6 loopback: %v", err)
	}
	defer ln.Close()

	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Skipf("no IPv6 loopback dial: %v", err)
	}
	defer client.Close()
	server, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	pid, err := findTCPPeerPID(server.RemoteAddr())
	if err != nil {
		t.Fatalf("findTCPPeerPID (v6): %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("peer pid = %d, want own pid %d", pid, os.Getpid())
	}
}
