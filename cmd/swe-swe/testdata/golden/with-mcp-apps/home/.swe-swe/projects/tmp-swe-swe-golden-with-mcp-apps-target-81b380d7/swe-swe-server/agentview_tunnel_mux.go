package main

// agentview_tunnel_mux.go -- stream multiplexer for the Agent View reverse
// tunnel. One WebSocket per session is dialed BY the swe-swe box to the
// browser backend (same trust direction as swe-swe-tunnel); every TCP
// connection the backend accepts on its own loopback becomes one stream over
// that WebSocket, and the swe-swe side replays it against 127.0.0.1:<port>.
//
// Wire protocol (fixed by tasks/2026-07-18-agent-view-reverse-tunnel.md):
//
//   - Control frames: WS text messages, JSON:
//     client -> backend  {"op":"sync","ports":[1977,3000]}      full desired set
//     backend -> client  {"op":"sync-result","bound":[...],"refused":[{"port":p,"reason":r}]}
//     backend -> client  {"op":"open","stream":7,"port":3000}   loopback accept
//     either direction   {"op":"close","stream":7}
//   - Data frames: WS binary messages, 4-byte big-endian stream id + payload.
//
// The mux is written against the tiny tunnelMessageConn interface (satisfied
// by *websocket.Conn) so unit tests drive it over net.Pipe-backed fakes.

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
)

// Message type values match gorilla/websocket's TextMessage/BinaryMessage so a
// *websocket.Conn satisfies tunnelMessageConn without adaptation.
const (
	tunnelTextMessage   = 1
	tunnelBinaryMessage = 2
)

// tunnelMessageConn is the minimal message-oriented conn the mux runs over.
type tunnelMessageConn interface {
	ReadMessage() (messageType int, p []byte, err error)
	WriteMessage(messageType int, data []byte) error
	Close() error
}

// tunnelStreamBufMax bounds each stream's receive buffer. The WebSocket gives
// no per-stream flow control, so a consumer that stalls while its peer keeps
// sending would otherwise either grow memory without bound or head-of-line
// block every other stream behind the shared read loop. Policy: when a
// stream's buffered-but-unread bytes would exceed this bound, the mux kills
// THAT stream (close frame + local error) and keeps the others flowing. 4 MiB
// comfortably covers HTTP responses in flight between two loopback hops. Var,
// not const, so tests can shrink it.
var tunnelStreamBufMax = 4 << 20

type tunnelRefusal struct {
	Port   int    `json:"port"`
	Reason string `json:"reason"`
}

type tunnelControl struct {
	Op      string          `json:"op"`
	Ports   []int           `json:"ports,omitempty"`
	Bound   []int           `json:"bound,omitempty"`
	Refused []tunnelRefusal `json:"refused,omitempty"`
	Stream  uint32          `json:"stream,omitempty"`
	Port    int             `json:"port,omitempty"`
}

// encodeTunnelFrame prefixes payload with the 4-byte big-endian stream id.
func encodeTunnelFrame(stream uint32, payload []byte) []byte {
	b := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(b, stream)
	copy(b[4:], payload)
	return b
}

// decodeTunnelFrame splits a data frame into stream id and payload.
func decodeTunnelFrame(b []byte) (uint32, []byte, error) {
	if len(b) < 4 {
		return 0, nil, fmt.Errorf("tunnel data frame too short: %d bytes", len(b))
	}
	return binary.BigEndian.Uint32(b), b[4:], nil
}

var (
	errTunnelStreamClosed = errors.New("tunnel: stream closed")
	errTunnelMuxClosed    = errors.New("tunnel: mux closed")
	errTunnelOverflow     = errors.New("tunnel: stream receive buffer overflow")
)

// tunnelMux multiplexes streams over one message conn. Symmetric, but in this
// protocol only the backend opens streams (on loopback accept) and only the
// client sends sync.
type tunnelMux struct {
	conn tunnelMessageConn

	writeMu sync.Mutex // serializes WriteMessage across streams + control

	mu      sync.Mutex
	streams map[uint32]*tunnelStream
	nextID  uint32
	closed  bool

	// onOpen runs in its own goroutine when the peer opens a stream (the
	// handler dials a local port and must not stall the read loop).
	onOpen func(s *tunnelStream, port int)
	// onControl runs synchronously in the read loop for sync / sync-result
	// ops so consecutive syncs are observed in order. Keep it quick.
	onControl func(tunnelControl)
}

func newTunnelMux(conn tunnelMessageConn, onOpen func(*tunnelStream, int), onControl func(tunnelControl)) *tunnelMux {
	return &tunnelMux{
		conn:      conn,
		streams:   make(map[uint32]*tunnelStream),
		onOpen:    onOpen,
		onControl: onControl,
	}
}

// run is the read loop. It blocks until the conn dies or close() is called,
// then fails every remaining stream. Callers run it in a goroutine.
func (m *tunnelMux) run() error {
	defer m.failAll(errTunnelMuxClosed)
	for {
		mt, data, err := m.conn.ReadMessage()
		if err != nil {
			return err
		}
		switch mt {
		case tunnelTextMessage:
			var ctl tunnelControl
			if err := json.Unmarshal(data, &ctl); err != nil {
				log.Printf("tunnel: dropping unparseable control frame: %v", err)
				continue
			}
			m.handleControl(ctl)
		case tunnelBinaryMessage:
			id, payload, err := decodeTunnelFrame(data)
			if err != nil {
				log.Printf("tunnel: dropping bad data frame: %v", err)
				continue
			}
			m.deliver(id, payload)
		}
	}
}

func (m *tunnelMux) handleControl(ctl tunnelControl) {
	switch ctl.Op {
	case "open":
		s := newTunnelStream(ctl.Stream, ctl.Port, m)
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			return
		}
		m.streams[ctl.Stream] = s
		m.mu.Unlock()
		if m.onOpen != nil {
			go m.onOpen(s, ctl.Port)
		}
	case "close":
		m.mu.Lock()
		s := m.streams[ctl.Stream]
		delete(m.streams, ctl.Stream)
		m.mu.Unlock()
		if s != nil {
			s.peerClosed()
		}
	default:
		if m.onControl != nil {
			m.onControl(ctl)
		}
	}
}

// deliver appends a data frame to its stream's receive buffer. Data for an
// unknown (never-opened or already-closed) stream is dropped silently -- close
// races frames in flight, that is normal. Overflowing the bounded buffer
// kills the stream, never the mux.
func (m *tunnelMux) deliver(id uint32, payload []byte) {
	m.mu.Lock()
	s := m.streams[id]
	m.mu.Unlock()
	if s == nil {
		return
	}
	if !s.enqueue(payload) {
		log.Printf("tunnel: stream %d (port %d) exceeded %d byte buffer -- killing stream", id, s.port, tunnelStreamBufMax)
		s.fail(errTunnelOverflow)
		m.closeStream(s, true)
	}
}

// openStream allocates a stream id, registers the stream, and tells the peer.
func (m *tunnelMux) openStream(port int) (*tunnelStream, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, errTunnelMuxClosed
	}
	m.nextID++
	id := m.nextID
	s := newTunnelStream(id, port, m)
	m.streams[id] = s
	m.mu.Unlock()
	if err := m.writeControl(tunnelControl{Op: "open", Stream: id, Port: port}); err != nil {
		m.mu.Lock()
		delete(m.streams, id)
		m.mu.Unlock()
		return nil, err
	}
	return s, nil
}

func (m *tunnelMux) sendSync(ports []int) error {
	return m.writeControl(tunnelControl{Op: "sync", Ports: ports})
}

func (m *tunnelMux) sendSyncResult(bound []int, refused []tunnelRefusal) error {
	return m.writeControl(tunnelControl{Op: "sync-result", Bound: bound, Refused: refused})
}

func (m *tunnelMux) writeControl(ctl tunnelControl) error {
	b, err := json.Marshal(ctl)
	if err != nil {
		return err
	}
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	return m.conn.WriteMessage(tunnelTextMessage, b)
}

func (m *tunnelMux) writeData(id uint32, p []byte) error {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	return m.conn.WriteMessage(tunnelBinaryMessage, encodeTunnelFrame(id, p))
}

// closeStream unregisters s and (best effort) tells the peer. Idempotent.
func (m *tunnelMux) closeStream(s *tunnelStream, notifyPeer bool) {
	m.mu.Lock()
	_, registered := m.streams[s.id]
	delete(m.streams, s.id)
	m.mu.Unlock()
	if registered && notifyPeer {
		if err := m.writeControl(tunnelControl{Op: "close", Stream: s.id}); err != nil {
			log.Printf("tunnel: close frame for stream %d failed: %v", s.id, err)
		}
	}
}

// close tears down the conn; run() then unblocks and fails all streams.
func (m *tunnelMux) close() error {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	return m.conn.Close()
}

// failAll marks every stream dead (used when the conn drops).
func (m *tunnelMux) failAll(err error) {
	m.mu.Lock()
	m.closed = true
	streams := make([]*tunnelStream, 0, len(m.streams))
	for _, s := range m.streams {
		streams = append(streams, s)
	}
	m.streams = make(map[uint32]*tunnelStream)
	m.mu.Unlock()
	for _, s := range streams {
		s.fail(err)
	}
}

// tunnelStream is one multiplexed byte stream. Read/Write/Close follow
// net.Conn-ish semantics: Read drains buffered data before reporting EOF,
// Write fails once either side closed, Close is idempotent.
type tunnelStream struct {
	id   uint32
	port int
	mux  *tunnelMux

	mu       sync.Mutex
	cond     *sync.Cond
	buf      []byte
	readErr  error // sticky: peer closed (io.EOF), overflow, mux dead
	writeErr error // sticky: local close or peer close
}

func newTunnelStream(id uint32, port int, m *tunnelMux) *tunnelStream {
	s := &tunnelStream{id: id, port: port, mux: m}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// enqueue appends received data; false means the bounded buffer would
// overflow and the caller must kill the stream.
func (s *tunnelStream) enqueue(p []byte) bool {
	if len(p) == 0 {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readErr != nil {
		return true // stream already dead; drop quietly
	}
	if len(s.buf)+len(p) > tunnelStreamBufMax {
		return false
	}
	s.buf = append(s.buf, p...)
	s.cond.Broadcast()
	return true
}

func (s *tunnelStream) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.buf) == 0 && s.readErr == nil {
		s.cond.Wait()
	}
	if len(s.buf) > 0 {
		n := copy(p, s.buf)
		s.buf = s.buf[n:]
		return n, nil
	}
	return 0, s.readErr
}

func (s *tunnelStream) Write(p []byte) (int, error) {
	s.mu.Lock()
	err := s.writeErr
	s.mu.Unlock()
	if err != nil {
		return 0, err
	}
	if err := s.mux.writeData(s.id, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Close ends the stream locally and notifies the peer. Idempotent.
func (s *tunnelStream) Close() error {
	s.mu.Lock()
	alreadyDead := s.writeErr != nil && s.readErr != nil
	if s.writeErr == nil {
		s.writeErr = errTunnelStreamClosed
	}
	if s.readErr == nil {
		s.readErr = errTunnelStreamClosed
	}
	s.cond.Broadcast()
	s.mu.Unlock()
	if !alreadyDead {
		s.mux.closeStream(s, true)
	}
	return nil
}

// peerClosed handles the peer's close frame: buffered data stays readable,
// then Read reports io.EOF; writes fail immediately.
func (s *tunnelStream) peerClosed() {
	s.mu.Lock()
	if s.readErr == nil {
		s.readErr = io.EOF
	}
	if s.writeErr == nil {
		s.writeErr = errTunnelStreamClosed
	}
	s.cond.Broadcast()
	s.mu.Unlock()
}

// fail marks the stream dead with err (overflow, mux teardown). Pending and
// future reads see err once the buffer would block; writes fail.
func (s *tunnelStream) fail(err error) {
	s.mu.Lock()
	if s.readErr == nil {
		s.readErr = err
	}
	if s.writeErr == nil {
		s.writeErr = err
	}
	s.cond.Broadcast()
	s.mu.Unlock()
}
