---
description: Inspect App Preview page content — use instead of browser tools for preview
---

# Debug with App Preview

Use the `swe-swe-preview` MCP tools to inspect the App Preview panel and receive console logs, errors, and network requests from the user's browser. This is more effective than the CDP browser because you see exactly what the user sees.

## Prerequisites

- User must have the App Preview panel open (right side of terminal UI)
- App must be running on `$PORT` (check with `echo $PORT`)

## MCP Tools

### Query DOM Elements

Use `browser_debug_preview` to query a specific element by CSS selector:

```
mcp__swe-swe-preview__browser_debug_preview(selector: "h1")
mcp__swe-swe-preview__browser_debug_preview(selector: ".error-message")
mcp__swe-swe-preview__browser_debug_preview(selector: "#submit-btn")
mcp__swe-swe-preview__browser_debug_preview(selector: "[data-testid='login-form']")
```

Response:
```json
{"t":"queryResult","found":true,"text":"Page Title","html":"<h1>Page Title</h1>","visible":true,"rect":{"x":0,"y":0,"width":500,"height":50}}
```

If not found:
```json
{"t":"queryResult","found":false}
```

### Listen for Console & Network Activity

Use `browser_debug_preview_listen` to capture console logs, errors, and network requests for a specified duration:

```
mcp__swe-swe-preview__browser_debug_preview_listen(duration_seconds: 5)
```

Returns JSON messages collected during the listening period:
```json
{"t":"console","m":"log","args":["Hello!",{"data":123}],"ts":...}
{"t":"console","m":"warn","args":["Warning message"],"ts":...}
{"t":"console","m":"error","args":["Error occurred"],"ts":...}
{"t":"error","msg":"Uncaught TypeError: ...","stack":"...","ts":...}
{"t":"fetch","url":"/api/users","method":"GET","status":200,"ms":45,"ts":...}
{"t":"xhr","url":"/api/data","method":"POST","status":500,"ms":120,"ts":...}
```

Navigation events:
```json
{"t":"urlchange","url":"http://localhost:3000/about","ts":...}
{"t":"navstate","canGoBack":true,"canGoForward":false}
```

## Workflow

1. **Start your app** on `$PORT` (e.g., `python3 -m http.server "$PORT"`)
2. **Ask the user** to open the Preview tab in the right panel
3. **Use `browser_debug_preview`** to query DOM elements and see what's on the page
4. **Use `browser_debug_preview_listen`** to monitor console output, errors, and network requests
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

- Prefer `browser_debug_preview` (DOM query) for quick page inspection — it returns immediately
- Use `browser_debug_preview_listen` when you need to capture activity over time (e.g., trigger an action then see what happens)
- Start with short durations (2-5 seconds) for `browser_debug_preview_listen`
- DOM queries return the FIRST matching element only
- The `visible` field in query results indicates if element is in viewport
- Network requests show timing (`ms` field) — useful for performance debugging

## See Also

- `.swe-swe/docs/app-preview.md` - App Preview documentation
- `.swe-swe/docs/browser-automation.md` - CDP browser for visual testing
