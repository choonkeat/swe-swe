package main

import (
	"bufio"
	"errors"
	"net"
	"net/http"
)

// sameOriginFrameWriter stamps a response as "may be shown in a frame on this
// same site" at the moment its headers go out, not before the handler runs.
//
// Everything under /proxy/{uuid}/ is shown in a pane iframe on this same
// origin. A front proxy that adds "X-Frame-Options: deny" to any response
// lacking one (a Cloudflare Access gateway does) would otherwise blank every
// pane in single-port mode. Our own SAMEORIGIN stops it adding deny;
// frame-ancestors 'self' wins even if it adds deny anyway, since browsers
// ignore X-Frame-Options when frame-ancestors is present.
//
// Why at header time: the preview proxy rewrites the Content-Security-Policy
// of every page it injects its script into -- it adds "script-src 'self'" and
// removes frame-ancestors. A policy set before it ran was fed through that
// rewrite, so a page that sent no policy came out with its inline scripts
// blocked and without our frame-ancestors. Adding ours last, as a separate
// policy, leaves the app's own policy (or its absence) as the proxy made it.
type sameOriginFrameWriter struct {
	http.ResponseWriter
	stamped bool
}

func (w *sameOriginFrameWriter) stamp() {
	if w.stamped {
		return
	}
	w.stamped = true
	stampSameOriginFraming(w.ResponseWriter.Header())
}

// stampSameOriginFraming marks a response as "may be shown in a frame on this
// same site, and nowhere else". Used for /proxy/ panes (via
// sameOriginFrameWriter) and for the session page, which the Terminal pane
// shows inside a frame (/session/{shell uuid}?assistant=shell) -- a page with
// no X-Frame-Options of its own gets "deny" added by a gateway such as
// Cloudflare Access, and the Terminal tab stays blank.
func stampSameOriginFraming(h http.Header) {
	h.Set("X-Frame-Options", "SAMEORIGIN")
	h.Add("Content-Security-Policy", "frame-ancestors 'self'")
}

func (w *sameOriginFrameWriter) WriteHeader(code int) {
	w.stamp()
	w.ResponseWriter.WriteHeader(code)
}

func (w *sameOriginFrameWriter) Write(b []byte) (int, error) {
	w.stamp()
	return w.ResponseWriter.Write(b)
}

func (w *sameOriginFrameWriter) Flush() {
	w.stamp()
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack keeps WebSocket upgrades (Agent View, agent-chat, the preview debug
// channel) working through the path route.
func (w *sameOriginFrameWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, errors.New("sameOriginFrameWriter: underlying ResponseWriter cannot hijack")
}

func (w *sameOriginFrameWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
