# App Preview Panel

The terminal UI has a split-pane layout with your app preview on the right side.

## How It Works

The preview panel automatically connects to the per-session preview proxy on port `20000+PORT` (e.g., if `PORT=3007`, preview connects to `23007`).

Inside the container, start your app on the assigned `PORT`:

```bash
# See your assigned port
echo $PORT

# Example: Start a Python HTTP server
python3 -m http.server "$PORT"

# Example: Start a Node.js app
npm start  # assuming it runs on $PORT
```

The preview panel will show your app automatically. If no server is running on `$PORT`, a "Waiting for App" page will display with auto-retry.

## Multiple services (Procfile)

If your app has more than one process (web + worker + database, etc.), run them
with a `Procfile` via `swe-run` instead of starting each by hand:

```
web: node server.js
db: postgres -D ./pgdata -p $PORT_DB -k /tmp
```

```bash
swe-run
```

`swe-run` gives the **primary** service (`web`, or the first line) your session
`PORT`, so it shows in this Preview panel automatically; every other service
gets its own collision-free port published as `$PORT_<NAME>` for siblings to
reach on `localhost`. Services are ordinary children of the session, so they are
torn down cleanly when the session ends -- no Docker, no leaks. See
`.swe-swe/docs/multi-service.md` for the full guide.

## Navigation

The preview has a toolbar with these controls:

- **Home (⌂)**: Navigate back to `/`
- **Back (◀)**: Go back in browser history (disabled when no history)
- **Forward (▶)**: Go forward in browser history (disabled when at latest)
- **Refresh (↻)**: Reload the current page
- **URL bar**: Shows the current page URL; type a path and press Enter or click Go to navigate
- **Go (→)**: Navigate to the URL typed in the URL bar
- **Open external (↗)**: Open the current page in a new browser tab

The URL bar updates live as the user navigates (including SPA pushState/replaceState changes). Back/forward buttons enable/disable automatically based on navigation history.

## Port Configuration

Each session gets its own `PORT` (default range 3000-3019). The preview port is computed as `20000+PORT`:
- PORT=3000 → Preview port 23000
- PORT=3007 → Preview port 23007
- PORT=3019 → Preview port 23019

## Multiple Services / vhost Apps

The preview listener can reach more than the single `$PORT` app. It demuxes the
leftmost label of the browser-facing hostname (see ADR-0045):

- `app1-5000.<reach>:<previewPort>` -> `127.0.0.1:5000`, upstream
  `Host: app1.lvh.me:5000` (so a compose traefik/nginx matches `app1.lvh.me`).
- `5000.<reach>:<previewPort>` -> `127.0.0.1:5000`, upstream
  `Host: localhost:5000` (no Host-based router needed).

You do NOT need Docker or compose for this. Run several services as plain
processes and reach each by its port label:

```bash
# each on its own loopback port
python3 -m http.server 5000 &
PORT=5001 npm start &
# or a Procfile-style runner
npx -y foreman start        # foreman / node-foreman
process-compose up          # process-compose (declarative)
```

Then type `app1.lvh.me:5000` (or `5000` for the bare-port form) in the preview
URL bar. The `Host` rewrite only matters when your own stack has a Host-based
router (traefik/nginx); plain apps ignore it. swe-swe does not start, stop, or
supervise these processes -- that is your runner's job.

When wildcard DNS for the reach is unavailable, the preview degrades to
**pinned mode** (one vhost at a time) and shows a "pinned" indicator by the URL
bar. Over the tunnel it is always pinned. See
[docs/multi-service.md](../../../docs/multi-service.md) for the full guide.

## Debug Channel

The preview proxy injects a debug script into HTML responses, allowing you to receive console logs, errors, and network requests from the user's browser in real-time.

### Listening for Debug Messages

```bash
# Listen for all debug messages (console, errors, fetch, etc.)
swe-swe-server --debug-listen
```

Output is JSON lines:
```json
{"t":"init","url":"http://...","ts":...}
{"t":"console","m":"log","args":["Hello!",{"time":123}],"ts":...}
{"t":"console","m":"warn","args":["Warning!"],"ts":...}
{"t":"error","msg":"Uncaught Error: ...","stack":"...","ts":...}
{"t":"fetch","url":"/api/test","method":"GET","status":200,"ms":45,"ts":...}
```

Press Ctrl+C to stop listening.

### Querying DOM Elements

```bash
# Query an element by CSS selector
swe-swe-server --debug-query "h1"
swe-swe-server --debug-query ".error-message"
swe-swe-server --debug-query "#submit-btn"
```

Response:
```json
{"t":"queryResult","found":true,"text":"Page Title","html":"...","visible":true,"rect":{"x":0,"y":0,"width":100,"height":50}}
```

### Message Types

| Type | Field `t` | Description |
|------|-----------|-------------|
| Page load | `init` | Sent when page loads, includes URL |
| URL change | `urlchange` | SPA navigation (pushState, replaceState, popstate, hashchange) |
| Nav state | `navstate` | Back/forward button availability (`canGoBack`, `canGoForward`) |
| Console | `console` | Console.log/warn/error/info/debug output |
| Error | `error` | Uncaught exceptions with stack trace |
| Promise rejection | `rejection` | Unhandled promise rejections |
| Fetch | `fetch` | fetch() requests with status and timing |
| XHR | `xhr` | XMLHttpRequest with status and timing |
| Query result | `queryResult` | Response to DOM query |

### Limitations

- Only works for web apps served through the App Preview (your session's `PORT`)
- The user must have the preview open in their browser for messages to flow
- DOM queries return the first matching element only
- No request/response body capture (only metadata)
