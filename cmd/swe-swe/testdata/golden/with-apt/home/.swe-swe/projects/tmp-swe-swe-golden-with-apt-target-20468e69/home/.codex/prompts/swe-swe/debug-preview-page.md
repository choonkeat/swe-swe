---
description: Inspect App Preview page content — use instead of browser tools for preview
---

# Debug with App Preview

Use swe-swe's debug channel to receive console logs, errors, and network requests from the user's browser in real-time. This is more effective than the CDP browser because you see exactly what the user sees.

## Prerequisites

- User must have the App Preview panel open (right side of terminal UI)
- App must be running on `$PORT` (check with `echo $PORT`)

## Commands

### Listen for Debug Messages

Run this to receive real-time debug output:

```bash
swe-swe-server --debug-listen
```

Output is JSON lines:
```json
{"t":"init","url":"http://...","ts":...}
{"t":"console","m":"log","args":["Hello!",{"data":123}],"ts":...}
{"t":"console","m":"warn","args":["Warning message"],"ts":...}
{"t":"console","m":"error","args":["Error occurred"],"ts":...}
{"t":"error","msg":"Uncaught TypeError: ...","stack":"...","ts":...}
{"t":"fetch","url":"/api/users","method":"GET","status":200,"ms":45,"ts":...}
{"t":"xhr","url":"/api/data","method":"POST","status":500,"ms":120,"ts":...}
```

You will also see navigation events:
```json
{"t":"urlchange","url":"http://localhost:3000/about","ts":...}
{"t":"navstate","canGoBack":true,"canGoForward":false}
```

Press Ctrl+C to stop listening.

### Query DOM Elements

Query a specific element by CSS selector:

```bash
swe-swe-server --debug-query "h1"
swe-swe-server --debug-query ".error-message"
swe-swe-server --debug-query "#submit-btn"
swe-swe-server --debug-query "[data-testid='login-form']"
```

Response:
```json
{"t":"queryResult","found":true,"text":"Page Title","html":"<h1>Page Title</h1>","visible":true,"rect":{"x":0,"y":0,"width":500,"height":50}}
```

If not found:
```json
{"t":"queryResult","found":false}
```

## Workflow

1. **Start your app** on `$PORT` (e.g., `python3 -m http.server "$PORT"`)
2. **Ask the user** to open the Preview tab in the right panel
3. **Run `--debug-listen`** to monitor console output, errors, and network requests
4. **Use `--debug-query`** to inspect specific DOM elements
5. **Fix issues** based on what you observe, then ask the user to reload

## Message Types

| Type | Field `t` | Description |
|------|-----------|-------------|
| Page load | `init` | Sent when page loads, includes URL |
| URL change | `urlchange` | SPA navigation (pushState, replaceState, popstate, hashchange) |
| Nav state | `navstate` | Back/forward availability (`canGoBack`, `canGoForward`) |
| Console | `console` | Console.log/warn/error/info/debug output |
| Error | `error` | Uncaught exceptions with stack trace |
| Promise rejection | `rejection` | Unhandled promise rejections |
| Fetch | `fetch` | fetch() requests with status and timing |
| XHR | `xhr` | XMLHttpRequest with status and timing |
| Query result | `queryResult` | Response to DOM query |

## Tips

- The debug channel captures ALL console output, including from third-party libraries
- Network requests show timing (`ms` field) - useful for performance debugging
- DOM queries return the FIRST matching element only
- The `visible` field in query results indicates if element is in viewport
- `urlchange` events fire on SPA navigations, so you can track which page the user is on
- Stack traces in errors may be minified - check source maps if needed

## Limitations

- Only works for the App Preview (your session's `$PORT`)
- User must have Preview panel open for messages to flow
- No request/response body capture (only metadata)
- Source maps not automatically resolved

## Example Session

```bash
# Terminal 1: Start your app
python3 -m http.server "$PORT"

# Terminal 2: Listen for debug messages
swe-swe-server --debug-listen

# User opens Preview panel, you see:
# {"t":"init","url":"http://localhost:3000/","ts":1706012345678}
# {"t":"navstate","canGoBack":false,"canGoForward":false}

# User clicks a link, you see:
# {"t":"urlchange","url":"http://localhost:3000/about","ts":1706012345800}
# {"t":"navstate","canGoBack":true,"canGoForward":false}

# Query for an error message element
swe-swe-server --debug-query ".error-toast"
# {"t":"queryResult","found":true,"text":"Invalid email address","visible":true,...}
```

## See Also

- `.swe-swe/docs/app-preview.md` - App Preview documentation
- `.swe-swe/docs/browser-automation.md` - CDP browser for visual testing
