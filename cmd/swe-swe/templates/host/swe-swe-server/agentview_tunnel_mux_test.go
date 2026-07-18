package main

// Tests for the Agent View reverse-tunnel stream mux (agentview_tunnel_mux.go).
// The mux is exercised over net.Pipe-backed fake message conns, never a real
// WebSocket, so these tests are fast and deterministic.

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"
)

// pipeMsgConn frames messages over a net.Conn (1-byte type + 4-byte big-endian
// length + payload) to stand in for a *websocket.Conn in tests.
type pipeMsgConn struct {
	c   net.Conn
	wmu sync.Mutex
}

func (p *pipeMsgConn) WriteMessage(mt int, data []byte) error {
	p.wmu.Lock()
	defer p.wmu.Unlock()
	var hdr [5]byte
	hdr[0] = byte(mt)
	binary.BigEndian.PutUint32(hdr[1:], uint32(len(data)))
	if _, err := p.c.Write(hdr[:]); err != nil {
		return err
	}
	_, err := p.c.Write(data)
	return err
}

func (p *pipeMsgConn) ReadMessage() (int, []byte, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(p.c, hdr[:]); err != nil {
		return 0, nil, err
	}
	n := binary.BigEndian.Uint32(hdr[1:])
	buf := make([]byte, n)
	if _, err := io.ReadFull(p.c, buf); err != nil {
		return 0, nil, err
	}
	return int(hdr[0]), buf, nil
}

func (p *pipeMsgConn) Close() error { return p.c.Close() }

func newPipeMsgConnPair() (*pipeMsgConn, *pipeMsgConn) {
	a, b := net.Pipe()
	return &pipeMsgConn{c: a}, &pipeMsgConn{c: b}
}

func TestTunnelFrameRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		stream  uint32
		payload []byte
	}{
		{1, []byte("hello")},
		{0xdeadbeef, []byte{}},
		{7, make([]byte, 65536)},
	} {
		b := encodeTunnelFrame(tc.stream, tc.payload)
		id, payload, err := decodeTunnelFrame(b)
		if err != nil {
			t.Fatalf("decode(%d): %v", tc.stream, err)
		}
		if id != tc.stream {
			t.Errorf("stream id: got %d want %d", id, tc.stream)
		}
		if len(payload) != len(tc.payload) {
			t.Errorf("payload len: got %d want %d", len(payload), len(tc.payload))
		}
	}
	if _, _, err := decodeTunnelFrame([]byte{1, 2}); err == nil {
		t.Error("decode of short frame should error")
	}
}

func TestTunnelControlMarshal(t *testing.T) {
	// Wire format is fixed by the task spec; parse the spec's literal examples.
	var sync1 tunnelControl
	if err := json.Unmarshal([]byte(`{"op":"sync","ports":[1977,3000]}`), &sync1); err != nil {
		t.Fatal(err)
	}
	if sync1.Op != "sync" || !reflect.DeepEqual(sync1.Ports, []int{1977, 3000}) {
		t.Errorf("sync parse: %+v", sync1)
	}

	var sr tunnelControl
	if err := json.Unmarshal([]byte(`{"op":"sync-result","bound":[3000],"refused":[{"port":6080,"reason":"reserved"}]}`), &sr); err != nil {
		t.Fatal(err)
	}
	if sr.Op != "sync-result" || !reflect.DeepEqual(sr.Bound, []int{3000}) ||
		len(sr.Refused) != 1 || sr.Refused[0].Port != 6080 || sr.Refused[0].Reason != "reserved" {
		t.Errorf("sync-result parse: %+v", sr)
	}

	var op tunnelControl
	if err := json.Unmarshal([]byte(`{"op":"open","stream":7,"port":3000}`), &op); err != nil {
		t.Fatal(err)
	}
	if op.Op != "open" || op.Stream != 7 || op.Port != 3000 {
		t.Errorf("open parse: %+v", op)
	}

	// Round-trip: marshal must reproduce the same fields.
	b, err := json.Marshal(tunnelControl{Op: "close", Stream: 7})
	if err != nil {
		t.Fatal(err)
	}
	var cl tunnelControl
	if err := json.Unmarshal(b, &cl); err != nil {
		t.Fatal(err)
	}
	if cl.Op != "close" || cl.Stream != 7 {
		t.Errorf("close round-trip: %+v", cl)
	}
}

// muxPair wires two muxes over net.Pipe and runs both read loops.
type muxPair struct {
	a, b *tunnelMux
}

func newMuxPair(t *testing.T, onOpenB func(*tunnelStream, int), onControlA, onControlB func(tunnelControl)) *muxPair {
	t.Helper()
	ca, cb := newPipeMsgConnPair()
	a := newTunnelMux(ca, nil, onControlA)
	b := newTunnelMux(cb, onOpenB, onControlB)
	go a.run()
	go b.run()
	t.Cleanup(func() {
		a.close()
		b.close()
	})
	return &muxPair{a: a, b: b}
}

func TestTunnelMuxInterleavedStreams(t *testing.T) {
	// B echoes every opened stream.
	echo := func(s *tunnelStream, port int) {
		go func() {
			io.Copy(s, s)
			s.Close()
		}()
	}
	p := newMuxPair(t, echo, nil, nil)

	s1, err := p.a.openStream(3000)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := p.a.openStream(4000)
	if err != nil {
		t.Fatal(err)
	}

	// Interleave writes across the two streams, then read both echoes.
	if _, err := s1.Write([]byte("one-first")); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Write([]byte("two-first")); err != nil {
		t.Fatal(err)
	}
	if _, err := s1.Write([]byte("|one-second")); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Write([]byte("|two-second")); err != nil {
		t.Fatal(err)
	}

	readN := func(s *tunnelStream, n int) string {
		buf := make([]byte, n)
		if _, err := io.ReadFull(s, buf); err != nil {
			t.Fatalf("read: %v", err)
		}
		return string(buf)
	}
	if got := readN(s2, len("two-first|two-second")); got != "two-first|two-second" {
		t.Errorf("s2 echo: %q", got)
	}
	if got := readN(s1, len("one-first|one-second")); got != "one-first|one-second" {
		t.Errorf("s1 echo: %q", got)
	}
}

func TestTunnelMuxClosePropagation(t *testing.T) {
	opened := make(chan *tunnelStream, 1)
	p := newMuxPair(t, func(s *tunnelStream, port int) { opened <- s }, nil, nil)

	// Opener closes: peer reads buffered data, then EOF.
	s, err := p.a.openStream(3000)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write([]byte("bye")); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	var peer *tunnelStream
	select {
	case peer = <-opened:
	case <-time.After(2 * time.Second):
		t.Fatal("peer stream never opened")
	}
	data, err := io.ReadAll(peer)
	if err != nil {
		t.Fatalf("peer read after close: %v", err)
	}
	if string(data) != "bye" {
		t.Errorf("peer read: %q", data)
	}

	// Acceptor closes: opener sees EOF on read and an error on later writes.
	s2, err := p.a.openStream(3000)
	if err != nil {
		t.Fatal(err)
	}
	var peer2 *tunnelStream
	select {
	case peer2 = <-opened:
	case <-time.After(2 * time.Second):
		t.Fatal("peer2 never opened")
	}
	peer2.Close()
	if data, err := io.ReadAll(s2); err != nil || len(data) != 0 {
		t.Errorf("opener read after peer close: data=%q err=%v", data, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := s2.Write([]byte("x")); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("write to peer-closed stream never errored")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTunnelMuxDataAfterCloseDropped(t *testing.T) {
	ca, cb := newPipeMsgConnPair()
	b := newTunnelMux(cb, func(s *tunnelStream, port int) {}, nil)
	go b.run()
	t.Cleanup(func() { b.close() })

	// Raw frames from the "A" side: data for a stream that was never opened,
	// then open+close+more data. Neither may panic B's read loop.
	if err := ca.WriteMessage(tunnelBinaryMessage, encodeTunnelFrame(99, []byte("orphan"))); err != nil {
		t.Fatal(err)
	}
	openMsg, _ := json.Marshal(tunnelControl{Op: "open", Stream: 5, Port: 3000})
	if err := ca.WriteMessage(tunnelTextMessage, openMsg); err != nil {
		t.Fatal(err)
	}
	closeMsg, _ := json.Marshal(tunnelControl{Op: "close", Stream: 5})
	if err := ca.WriteMessage(tunnelTextMessage, closeMsg); err != nil {
		t.Fatal(err)
	}
	if err := ca.WriteMessage(tunnelBinaryMessage, encodeTunnelFrame(5, []byte("late"))); err != nil {
		t.Fatal(err)
	}
	// B must still respond to control traffic afterwards.
	got := make(chan tunnelControl, 1)
	b.onControl = func(c tunnelControl) { got <- c }
	syncMsg, _ := json.Marshal(tunnelControl{Op: "sync", Ports: []int{8080}})
	if err := ca.WriteMessage(tunnelTextMessage, syncMsg); err != nil {
		t.Fatal(err)
	}
	select {
	case c := <-got:
		if c.Op != "sync" || !reflect.DeepEqual(c.Ports, []int{8080}) {
			t.Errorf("sync after junk: %+v", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("mux stopped processing after data-for-closed-stream")
	}
}

func TestTunnelMuxSyncRoundTrip(t *testing.T) {
	gotB := make(chan tunnelControl, 1)
	gotA := make(chan tunnelControl, 1)
	p := newMuxPair(t, nil,
		func(c tunnelControl) { gotA <- c },
		func(c tunnelControl) { gotB <- c })

	if err := p.a.sendSync([]int{1977, 3000}); err != nil {
		t.Fatal(err)
	}
	select {
	case c := <-gotB:
		if c.Op != "sync" || !reflect.DeepEqual(c.Ports, []int{1977, 3000}) {
			t.Errorf("sync received: %+v", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("sync never arrived")
	}

	if err := p.b.sendSyncResult([]int{3000}, []tunnelRefusal{{Port: 1977, Reason: "reserved"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case c := <-gotA:
		if c.Op != "sync-result" || !reflect.DeepEqual(c.Bound, []int{3000}) ||
			len(c.Refused) != 1 || c.Refused[0].Reason != "reserved" {
			t.Errorf("sync-result received: %+v", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("sync-result never arrived")
	}
}

func TestTunnelMuxBackpressureSlowStreamDoesNotBlockOthers(t *testing.T) {
	// Shrink the per-stream buffer bound so the test is fast.
	old := tunnelStreamBufMax
	tunnelStreamBufMax = 16 * 1024
	defer func() { tunnelStreamBufMax = old }()

	type openedStream struct {
		s    *tunnelStream
		port int
	}
	opened := make(chan openedStream, 2)
	p := newMuxPair(t, func(s *tunnelStream, port int) {
		opened <- openedStream{s, port}
		if port == 4000 {
			// Echo only the fast stream; the 3000 stream is never read.
			go func() {
				io.Copy(s, s)
				s.Close()
			}()
		}
	}, nil, nil)

	slow, err := p.a.openStream(3000)
	if err != nil {
		t.Fatal(err)
	}
	fast, err := p.a.openStream(4000)
	if err != nil {
		t.Fatal(err)
	}

	// Overflow the slow stream's receive buffer on B (nobody reads it there).
	chunk := make([]byte, 4096)
	for i := 0; i < 8; i++ { // 32 KiB > 16 KiB bound
		if _, err := slow.Write(chunk); err != nil {
			break // acceptable: B already killed the stream and told us
		}
	}

	// The fast stream must still work end-to-end.
	if _, err := fast.Write([]byte("still-alive")); err != nil {
		t.Fatalf("fast write after slow overflow: %v", err)
	}
	buf := make([]byte, len("still-alive"))
	readDone := make(chan error, 1)
	go func() {
		_, err := io.ReadFull(fast, buf)
		readDone <- err
	}()
	select {
	case err := <-readDone:
		if err != nil {
			t.Fatalf("fast echo after slow overflow: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fast stream blocked by slow stream overflow")
	}
	if string(buf) != "still-alive" {
		t.Errorf("fast echo: %q", buf)
	}

	// And the overflowed stream must be dead on the opener side too: writes
	// start failing once the peer's close frame arrives.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := slow.Write([]byte("x")); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("slow stream write never errored after overflow kill")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
