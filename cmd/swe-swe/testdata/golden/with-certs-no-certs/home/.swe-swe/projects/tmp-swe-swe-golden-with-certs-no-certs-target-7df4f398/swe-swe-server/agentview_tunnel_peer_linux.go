//go:build linux

package main

// Linux peer identification for the reverse tunnel's loopback listeners.
// SO_PEERCRED only exists for unix sockets, so for TCP the peer pid is
// resolved the /proc way: the accepted conn's RemoteAddr is the peer's LOCAL
// endpoint -- find that socket's inode in /proc/net/tcp{,6}, then find which
// process holds that inode via /proc/<pid>/fd. Prior art for the ancestry
// walk: broker.go findSessionForPID.

import (
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// tunnelPeerGuard admits only connections from processes inside the session's
// browser process tree (chromium and children; pids recorded at spawn). Fail
// closed: an unresolvable peer is rejected -- the backend image is Linux and
// /proc is always there.
func tunnelPeerGuard(sess *backendSession, conn net.Conn) error {
	pid, err := findTCPPeerPID(conn.RemoteAddr())
	if err != nil {
		return fmt.Errorf("peer pid: %w", err)
	}
	roots := make(map[int]bool)
	if sess.procs != nil {
		for _, p := range sess.procs.pids {
			roots[p] = true
		}
	}
	if len(roots) == 0 {
		return fmt.Errorf("session has no tracked browser pids")
	}
	if !pidHasAncestorIn(pid, roots) {
		return fmt.Errorf("pid %d is not in the session's browser process tree", pid)
	}
	return nil
}

// pidHasAncestorIn walks pid's PPid chain (pid itself included) looking for
// any member of roots. Bounded walk, same shape as findSessionForPID.
func pidHasAncestorIn(pid int, roots map[int]bool) bool {
	for steps := 0; pid > 1 && steps < 64; steps++ {
		if roots[pid] {
			return true
		}
		data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
		if err != nil {
			return false
		}
		next := parsePPid(data)
		if next == 0 || next == pid {
			return false
		}
		pid = next
	}
	return false
}

// findTCPPeerPID resolves which local process owns the socket whose LOCAL
// address is remote (i.e. the peer of a loopback connection we accepted).
func findTCPPeerPID(remote net.Addr) (int, error) {
	tcpAddr, ok := remote.(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("not a TCP address: %v", remote)
	}
	inode, err := findSocketInode(tcpAddr)
	if err != nil {
		return 0, err
	}
	return findPIDForSocketInode(inode)
}

// findSocketInode scans /proc/net/tcp and tcp6 for a socket bound locally to
// addr and returns its inode.
func findSocketInode(addr *net.TCPAddr) (uint64, error) {
	for _, table := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		data, err := os.ReadFile(table)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n")[1:] {
			fields := strings.Fields(line)
			if len(fields) < 10 {
				continue
			}
			ip, port, ok := parseProcNetHexAddr(fields[1])
			if !ok || port != addr.Port || !ip.Equal(addr.IP) {
				continue
			}
			inode, err := strconv.ParseUint(fields[9], 10, 64)
			if err != nil || inode == 0 {
				continue // TIME_WAIT etc. have no inode
			}
			return inode, nil
		}
	}
	return 0, fmt.Errorf("no /proc/net/tcp entry for %s", addr)
}

// parseProcNetHexAddr decodes /proc/net/tcp's "HEXIP:HEXPORT" local_address
// column. IPv4 is one little-endian u32; IPv6 is four little-endian u32
// groups.
func parseProcNetHexAddr(s string) (net.IP, int, bool) {
	ipHex, portHex, found := strings.Cut(s, ":")
	if !found {
		return nil, 0, false
	}
	port64, err := strconv.ParseUint(portHex, 16, 16)
	if err != nil {
		return nil, 0, false
	}
	raw, err := hex.DecodeString(ipHex)
	if err != nil || (len(raw) != 4 && len(raw) != 16) {
		return nil, 0, false
	}
	ip := make(net.IP, len(raw))
	for group := 0; group < len(raw); group += 4 {
		for i := 0; i < 4; i++ {
			ip[group+i] = raw[group+3-i]
		}
	}
	return ip, int(port64), true
}

// findPIDForSocketInode scans /proc/<pid>/fd for a "socket:[inode]" link.
func findPIDForSocketInode(inode uint64) (int, error) {
	want := fmt.Sprintf("socket:[%d]", inode)
	procs, err := os.ReadDir("/proc")
	if err != nil {
		return 0, err
	}
	for _, entry := range procs {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		fdDir := "/proc/" + entry.Name() + "/fd"
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue // other-user process or already gone
		}
		for _, fd := range fds {
			link, err := os.Readlink(fdDir + "/" + fd.Name())
			if err == nil && link == want {
				return pid, nil
			}
		}
	}
	return 0, fmt.Errorf("no process owns socket inode %d", inode)
}
