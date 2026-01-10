# Touch Scroll Proxy Prototype

Proof-of-concept for mobile terminal scrolling using a transparent overlay div that provides native momentum scrolling.

## The Idea

Instead of fighting xterm.js's broken touch handling:

1. Overlay a transparent scrollable div on top of xterm
2. This div has a spacer element matching the buffer height
3. Native browser scroll (with momentum!) works on this overlay
4. Scroll position syncs to xterm.scrollToLine()

## Run with Docker

```bash
# Build
docker build -t touch-scroll-proto .

# Run
docker run -it --rm -p 8080:8080 touch-scroll-proto
```

Then open `http://<your-host-ip>:8080` on your mobile device.

## Run locally (requires Go)

```bash
go run main.go
```

## Test Scrolling

1. Generate lots of output:
   ```bash
   for i in $(seq 1 500); do echo "Line $i"; done
   ```

2. Swipe up/down on the terminal area
3. Should get native momentum scrolling!

## Debug Info

The debug bar at top shows:
- Number of buffer lines
- Spacer height
- Scroll sync direction and positions

## Files

- `main.go` - Minimal Go websocket + PTY server
- `index.html` - xterm.js + touch scroll proxy overlay
- `Dockerfile` - Alpine-based container

## What to Test

- [ ] Momentum scrolling works?
- [ ] Scrolling stops when finger lifts (no fighting)?
- [ ] New output auto-scrolls when at bottom?
- [ ] Scroll position syncs correctly?
- [ ] Mobile keyboard buttons work?
- [ ] Input field sends commands?
