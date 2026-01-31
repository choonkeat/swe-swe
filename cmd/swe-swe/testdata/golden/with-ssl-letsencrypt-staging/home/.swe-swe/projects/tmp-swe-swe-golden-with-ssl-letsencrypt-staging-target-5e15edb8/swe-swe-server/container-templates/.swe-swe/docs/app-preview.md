# App Preview Panel

The terminal UI has a split-pane layout with your app preview on the right side.

## How It Works

The preview panel automatically connects to port `1${SWE_PORT}` (e.g., if SWE_PORT=1977, preview connects to port 11977).

Inside the container, start your app on port 3000:

```bash
# Example: Start a Python HTTP server
python3 -m http.server 3000

# Example: Start a Node.js app
npm start  # assuming it runs on port 3000
```

The preview panel will show your app automatically. If no server is running on port 3000, a "Waiting for App" page will display with auto-retry.

## Navigation

- **Home button (⌂)**: Navigate to the root path
- **Refresh button (↻)**: Reload the current page

## Port Configuration

The preview port is computed as "1" + SWE_PORT:
- SWE_PORT=1977 → Preview port 11977
- SWE_PORT=8080 → Preview port 18080

**Note**: SWE_PORT must be ≤ 9999 for valid preview port (max 19999 < 65535).

Inside the container, the preview proxy forwards requests to `localhost:3000` by default. Set `SWE_PREVIEW_TARGET_PORT` to use a different port.

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

### Limitations

- Only works for web apps served through the App Preview (port 3000 by default)
- The user must have the preview open in their browser for messages to flow
- DOM queries return the first matching element only
