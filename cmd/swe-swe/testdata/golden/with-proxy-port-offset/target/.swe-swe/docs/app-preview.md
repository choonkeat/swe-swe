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
