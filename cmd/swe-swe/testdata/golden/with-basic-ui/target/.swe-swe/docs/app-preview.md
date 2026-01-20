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
