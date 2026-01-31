# App Preview Panel

The terminal UI has a split-pane layout with your app preview on the right side.

## How It Works

The preview panel automatically connects to the per-session preview proxy on port `5${PORT}` (e.g., if `PORT=3007`, preview connects to `53007`).

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

- **Home button (⌂)**: Navigate to the root path
- **Refresh button (↻)**: Reload the current page

## Port Configuration

Each session gets its own `PORT` (default range 3000-3019). The preview port is computed as `5${PORT}`:
- PORT=3000 → Preview port 53000
- PORT=3007 → Preview port 53007
- PORT=3019 → Preview port 53019

## Debug Channel

The preview proxy injects a debug script into HTML responses, allowing you to receive console logs, errors, and network requests from the user's browser in real-time.

### Listening for Debug Messages

```bash
# Listen for all debug messages (console, errors, fetch, etc.)
export SWE_PREVIEW_PORT="$PORT"
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

### Querying DOM Elements

```bash
# Query an element by CSS selector
export SWE_PREVIEW_PORT="$PORT"
swe-swe-server --debug-query "h1"
swe-swe-server --debug-query ".error-message"
swe-swe-server --debug-query "#submit-btn"
```

Response:
```json
{"t":"queryResult","found":true,"text":"Page Title","html":"...","visible":true,"rect":{"x":0,"y":0,"width":100,"height":50}}
```

### Limitations

- Only works for web apps served through the App Preview (your session's `PORT`)
- The user must have the preview open in their browser for messages to flow
- DOM queries return the first matching element only
