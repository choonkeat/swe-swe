# Terminal-UI JS Modules Refactoring

## Goal

Refactor the monolithic `terminal-ui.js` (3,880 lines) into well-organized ES modules while:
- Maintaining identical functionality
- Introducing no regressions
- Maximizing pure functions for unit testability
- Keeping zero build step (native ES modules)

## Current State

| File | Lines | Purpose |
|------|-------|---------|
| `terminal-ui.js` | 3,880 | Single Web Component class |
| `link-provider.js` | 425 | xterm link providers |
| **Total** | **4,305** | |

## Target Structure

```
static/
├── terminal-ui.js           # Main Web Component (reduced to ~600 lines)
├── modules/
│   ├── util.js              # Pure utilities
│   ├── util.test.js
│   ├── validation.js        # Pure validation
│   ├── validation.test.js
│   ├── uuid.js              # Pure UUID derivation
│   ├── uuid.test.js
│   ├── url-builder.js       # Pure URL construction
│   ├── url-builder.test.js
│   ├── messages.js          # Pure message encoding/decoding
│   ├── messages.test.js
│   ├── reconnect.js         # Pure reconnection state
│   ├── reconnect.test.js
│   ├── upload-queue.js      # Pure upload queue state
│   ├── upload-queue.test.js
│   ├── chunk-assembler.js   # Pure chunk assembly state
│   ├── chunk-assembler.test.js
│   ├── status-renderer.js   # Pure HTML renderers
│   ├── status-renderer.test.js
│   ├── connection.js        # WebSocket wrapper (impure)
│   ├── connection.test.js
│   ├── terminal.js          # xterm wrapper (impure)
│   ├── terminal.test.js
│   ├── chat.js              # Chat DOM (impure)
│   ├── chat.test.js
│   ├── upload.js            # File upload DOM (impure)
│   ├── upload.test.js
│   ├── mobile-keyboard.js   # Mobile keyboard DOM (impure)
│   ├── settings.js          # Settings panel DOM (impure)
│   ├── split-pane.js        # Iframe pane DOM (impure)
│   └── status-bar.js        # Status bar DOM (impure)
├── styles/
│   └── terminal-ui.css      # Extracted CSS
├── link-provider.js         # Existing (no changes)
└── index.html               # Updated imports
```

## Testability Tiers

| Tier | Type | Testing | Modules |
|------|------|---------|---------|
| **1** | Pure utilities | Unit test directly | util, validation, uuid, url-builder |
| **2** | Pure state reducers | Unit test with state snapshots | reconnect, upload-queue, chunk-assembler |
| **3** | Pure renderers | Unit test output strings | status-renderer, messages |
| **4** | DOM wrappers | Integration via test container + MCP browser | connection, terminal, chat, upload, mobile-keyboard, settings, split-pane, status-bar |

## Development Workflow

```
┌─────────────────────────────────────────────────────────────┐
│  1. CREATE new module file (e.g., modules/util.js)          │
│     └─ standalone, doesn't affect anything yet              │
├─────────────────────────────────────────────────────────────┤
│  2. WRITE unit tests for the new module                     │
│     └─ tests run independently, terminal-ui.js unchanged    │
├─────────────────────────────────────────────────────────────┤
│  3. RUN unit tests → verify pure functions work             │
│     └─ terminal-ui.js still works exactly as before         │
├─────────────────────────────────────────────────────────────┤
│  4. SWAP IN: add import to terminal-ui.js, remove old code  │
│     └─ this is the only moment of risk                      │
├─────────────────────────────────────────────────────────────┤
│  5. INTEGRATION TEST via test container + MCP browser       │
│     └─ verify no regression                                 │
└─────────────────────────────────────────────────────────────┘
```

---

## Phase 1: Extract CSS to External Stylesheet

### What Will Be Achieved
- Move ~500 lines of CSS from `render()` template literal to `static/styles/terminal-ui.css`
- CSS becomes cacheable separately from JS
- Easier to edit styles without touching JS

### Steps

1. Create `static/styles/terminal-ui.css`
2. Copy all CSS from inside `<style>...</style>` in the `render()` method
3. Update `index.html` to load CSS via `<link>` tag
4. Keep CSS custom properties (`:root { --status-bar-color: ... }`) working
5. Verify template placeholders like `{{STATUS_BAR_COLOR}}` still work

### Verification

- **Unit:** N/A
- **Integration:** Boot test container, open terminal-ui in MCP browser, verify:
  - Status bar renders with correct color
  - Terminal displays correctly
  - Mobile keyboard styles work
  - Settings panel opens/closes with correct styling

---

## Phase 2: Extract `util.js` (Pure Utilities)

### What Will Be Achieved
- Extract 5 pure functions into `static/modules/util.js`
- Zero dependencies, trivially unit-testable

### Functions to Extract

| Function | Lines | Current Location |
|----------|-------|------------------|
| `formatDuration(ms)` | 10 | terminal-ui.js:1449 |
| `formatFileSize(bytes)` | 4 | terminal-ui.js:3386 |
| `escapeHtml(text)` | 4 | terminal-ui.js:2628 |
| `escapeFilename(name)` | 3 | terminal-ui.js:3372 |
| `parseLinks(linksStr)` | 11 | terminal-ui.js:1461 |

### Steps

1. Create `static/modules/util.js` with all 5 functions as named exports
2. Rewrite `escapeHtml` to be pure (no DOM):
   ```javascript
   export function escapeHtml(text) {
     return text
       .replace(/&/g, '&amp;')
       .replace(/</g, '&lt;')
       .replace(/>/g, '&gt;')
       .replace(/"/g, '&quot;');
   }
   ```
3. Create `static/modules/util.test.js` with unit tests
4. Update `terminal-ui.js` to import from `./modules/util.js`
5. Remove the original function definitions from `terminal-ui.js`

### Unit Tests

```javascript
// util.test.js
import { formatDuration, formatFileSize, escapeHtml, escapeFilename, parseLinks } from './util.js';

// formatDuration
assert.equal(formatDuration(5000), '5s');
assert.equal(formatDuration(65000), '1m 5s');
assert.equal(formatDuration(3665000), '1h 1m 5s');

// formatFileSize
assert.equal(formatFileSize(500), '500 B');
assert.equal(formatFileSize(1536), '1.5 KB');
assert.equal(formatFileSize(1572864), '1.5 MB');

// escapeHtml
assert.equal(escapeHtml('<script>'), '&lt;script&gt;');
assert.equal(escapeHtml('"foo"'), '&quot;foo&quot;');

// escapeFilename
assert.equal(escapeFilename("foo bar"), "foo\\ bar");
assert.equal(escapeFilename("it's"), "it\\'s");

// parseLinks
assert.deepEqual(parseLinks('[Docs](https://docs.com)'), [{ text: 'Docs', url: 'https://docs.com' }]);
assert.deepEqual(parseLinks(''), []);
```

### Verification

- **Unit:** Run `util.test.js`, all tests pass
- **Integration:** Boot test container, verify chat messages escape HTML, file sizes display correctly

---

## Phase 3: Extract `validation.js` (Pure Validation)

### What Will Be Achieved
- Extract 2 pure validation functions into `static/modules/validation.js`
- Consistent return shape: `{ valid: boolean, name?: string, error?: string }`

### Functions to Extract

| Function | Lines | Current Location |
|----------|-------|------------------|
| `validateUsername(name)` | 16 | terminal-ui.js:2092 |
| `validateSessionName(name)` | 16 | terminal-ui.js:2166 |

### Steps

1. Create `static/modules/validation.js` with both functions as named exports
2. Create `static/modules/validation.test.js` with unit tests
3. Update `terminal-ui.js` to import from `./modules/validation.js`
4. Remove the original method definitions from `terminal-ui.js`

### Unit Tests

```javascript
// validation.test.js
import { validateUsername, validateSessionName } from './validation.js';

// validateUsername - valid cases
assert.deepEqual(validateUsername('Alice'), { valid: true, name: 'Alice' });
assert.deepEqual(validateUsername('  Bob  '), { valid: true, name: 'Bob' });
assert.deepEqual(validateUsername('User 123'), { valid: true, name: 'User 123' });

// validateUsername - invalid cases
assert.deepEqual(validateUsername(''), { valid: false, error: 'Name cannot be empty' });
assert.deepEqual(validateUsername('   '), { valid: false, error: 'Name cannot be empty' });
assert.deepEqual(validateUsername('ThisNameIsTooLongForLimit'), { valid: false, error: 'Name must be 16 characters or less' });
assert.deepEqual(validateUsername('user@domain'), { valid: false, error: 'Name can only contain letters, numbers, and spaces' });

// validateSessionName - valid cases
assert.deepEqual(validateSessionName(''), { valid: true, name: '' });
assert.deepEqual(validateSessionName('my-session_01'), { valid: true, name: 'my-session_01' });

// validateSessionName - invalid cases
assert.deepEqual(validateSessionName('a'.repeat(33)), { valid: false, error: 'Name must be 32 characters or less' });
assert.deepEqual(validateSessionName('session@home'), { valid: false, error: 'Name can only contain letters, numbers, spaces, hyphens, and underscores' });
```

### Verification

- **Unit:** Run `validation.test.js`, all tests pass
- **Integration:** Boot test container, open settings panel, verify username/session name validation

---

## Phase 4: Extract `uuid.js` (Pure UUID Derivation)

### What Will Be Achieved
- Extract deterministic UUID generation into `static/modules/uuid.js`
- Expose `djb2Hash` helper for potential reuse

### Functions to Extract

| Function | Lines | Current Location |
|----------|-------|------------------|
| `deriveShellUUID(parentUUID)` | 22 | terminal-ui.js:1527 |
| `djb2Hash(str, seed)` | 8 | (nested inside above) |

### Steps

1. Create `static/modules/uuid.js` with both functions as named exports
2. Create `static/modules/uuid.test.js` with unit tests
3. Update `terminal-ui.js` to import from `./modules/uuid.js`
4. Remove the original method definition from `terminal-ui.js`

### Unit Tests

```javascript
// uuid.test.js
import { djb2Hash, deriveShellUUID } from './uuid.js';

// djb2Hash - deterministic
assert.equal(djb2Hash('hello'), djb2Hash('hello'));
assert.notEqual(djb2Hash('hello'), djb2Hash('world'));
assert.equal(typeof djb2Hash('test'), 'number');

// djb2Hash - seed changes output
assert.notEqual(djb2Hash('hello', 5381), djb2Hash('hello', 1234));

// deriveShellUUID - format (UUID v4 pattern)
const uuid = deriveShellUUID('550e8400-e29b-41d4-a716-446655440000');
assert.match(uuid, /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);

// deriveShellUUID - deterministic
assert.equal(deriveShellUUID('abc123'), deriveShellUUID('abc123'));

// deriveShellUUID - different parents = different UUIDs
assert.notEqual(deriveShellUUID('parent-1'), deriveShellUUID('parent-2'));
```

### Verification

- **Unit:** Run `uuid.test.js`, all tests pass
- **Integration:** Boot test container, click Shell link, verify derived UUID in URL

---

## Phase 5: Extract `url-builder.js` (Pure URL Construction)

### What Will Be Achieved
- Extract URL construction logic into `static/modules/url-builder.js`
- Make functions pure by passing config instead of reading `window.location`

### Functions to Extract

| Function | Lines | Current Location | Make Pure By |
|----------|-------|------------------|--------------|
| `getBaseUrl()` | 4 | terminal-ui.js:1511 | Pass `location` as param |
| `getVSCodeUrl()` | 6 | terminal-ui.js:1517 | Pass `baseUrl`, `workDir` |
| `buildShellUrl()` | (inline) | terminal-ui.js:1571 | Extract, pass config |
| `buildPreviewUrl()` | (inline) | terminal-ui.js:1565 | Extract, pass config |
| `getDebugQueryString()` | 3 | terminal-ui.js:1655 | Pass `debugMode` |

### Steps

1. Create `static/modules/url-builder.js` with pure functions
2. Each function takes explicit params instead of reading globals
3. Create `static/modules/url-builder.test.js` with unit tests
4. Update `terminal-ui.js` to import and call with appropriate args
5. Remove the original method definitions from `terminal-ui.js`

### Unit Tests

```javascript
// url-builder.test.js
import { getBaseUrl, buildVSCodeUrl, buildShellUrl, buildPreviewUrl, getDebugQueryString } from './url-builder.js';

// getBaseUrl
assert.equal(getBaseUrl({ protocol: 'https:', hostname: 'example.com', port: '' }), 'https://example.com');
assert.equal(getBaseUrl({ protocol: 'http:', hostname: 'localhost', port: '8080' }), 'http://localhost:8080');

// buildVSCodeUrl
assert.equal(buildVSCodeUrl('http://localhost:8080', '/workspace'), 'http://localhost:8080/vscode/?folder=%2Fworkspace');
assert.equal(buildVSCodeUrl('http://localhost:8080', ''), 'http://localhost:8080/vscode/');

// buildShellUrl
assert.equal(
  buildShellUrl({ baseUrl: 'http://localhost:8080', shellUUID: 'abc-123', parentUUID: 'parent-456', debug: false }),
  'http://localhost:8080/session/abc-123?assistant=shell&parent=parent-456'
);

// buildPreviewUrl
assert.equal(buildPreviewUrl({ protocol: 'https:', hostname: 'example.com', port: '8080' }), 'https://example.com:18080');

// getDebugQueryString
assert.equal(getDebugQueryString(true), '?debug=1');
assert.equal(getDebugQueryString(false), '');
```

### Verification

- **Unit:** Run `url-builder.test.js`, all tests pass
- **Integration:** Boot test container, verify VSCode/Shell/Preview links work correctly

---

## Phase 6: Extract `messages.js` (Pure Message Encoding/Decoding)

### What Will Be Achieved
- Extract binary protocol encoding/decoding into `static/modules/messages.js`
- Extract JSON message parsing with type discrimination
- Critical for correctness — pure functions are easy to test exhaustively

### Binary Protocol Reference

```
Resize:      [0x00, rows_hi, rows_lo, cols_hi, cols_lo]
FileUpload:  [0x01, name_len_hi, name_len_lo, ...name_bytes, ...file_data]
Chunk:       [0x02, chunk_index, total_chunks, ...payload]
```

### Functions to Extract

| Function | Purpose |
|----------|---------|
| `encodeResize(rows, cols)` | Build resize binary message |
| `encodeFileUpload(filename, data)` | Build file upload binary message |
| `decodeChunkHeader(data)` | Parse chunk header |
| `isChunkMessage(data)` | Check if binary is chunk |
| `parseServerMessage(jsonStr)` | Parse and validate JSON message |

### Steps

1. Create `static/modules/messages.js` with encoding/decoding functions
2. Define message type constants: `OPCODE_RESIZE = 0x00`, etc.
3. Create `static/modules/messages.test.js` with unit tests
4. Update `terminal-ui.js` to use these functions
5. Remove inline binary manipulation from `terminal-ui.js`

### Unit Tests

```javascript
// messages.test.js
import { OPCODE_RESIZE, encodeResize, encodeFileUpload, decodeChunkHeader, isChunkMessage, parseServerMessage } from './messages.js';

// encodeResize
const resize = encodeResize(24, 80);
assert.equal(resize.length, 5);
assert.equal(resize[0], OPCODE_RESIZE);
assert.equal((resize[1] << 8) | resize[2], 24);
assert.equal((resize[3] << 8) | resize[4], 80);

// isChunkMessage
assert.equal(isChunkMessage(new Uint8Array([0x02, 0, 3, 1, 2, 3])), true);
assert.equal(isChunkMessage(new Uint8Array([0x00, 0, 24, 0, 80])), false);

// decodeChunkHeader
const chunk = new Uint8Array([0x02, 2, 5, 10, 20, 30]);
const header = decodeChunkHeader(chunk);
assert.equal(header.chunkIndex, 2);
assert.equal(header.totalChunks, 5);
assert.deepEqual(Array.from(header.payload), [10, 20, 30]);

// parseServerMessage
assert.deepEqual(parseServerMessage('{"type":"pong","data":{"ts":123}}'), { type: 'pong', data: { ts: 123 } });
assert.equal(parseServerMessage('not json'), null);
```

### Verification

- **Unit:** Run `messages.test.js`, all tests pass
- **Integration:** Boot test container, verify terminal resizes, file upload works, large output loads

---

## Phase 7: Extract `reconnect.js` (Pure Reconnection State)

### What Will Be Achieved
- Extract reconnection logic as a pure state reducer pattern
- Easy to test all reconnection scenarios without actual WebSocket

### State Shape

```javascript
{
  attempts: number,
  maxDelay: number,   // 60000
  baseDelay: number,  // 1000
}
```

### Functions to Extract

| Function | Purpose |
|----------|---------|
| `createReconnectState()` | Initial state |
| `getDelay(state)` | Calculate delay: `min(1000 * 2^attempts, maxDelay)` |
| `nextAttempt(state)` | Increment attempts, return new state |
| `resetAttempts(state)` | Reset on success |
| `formatCountdown(delayMs)` | Format for display |

### Steps

1. Create `static/modules/reconnect.js` with pure state functions
2. Use reducer pattern — functions take state, return new state
3. Create `static/modules/reconnect.test.js` with unit tests
4. Update `terminal-ui.js` to use these functions
5. Remove `getReconnectDelay()` method from `terminal-ui.js`

### Unit Tests

```javascript
// reconnect.test.js
import { createReconnectState, getDelay, nextAttempt, resetAttempts } from './reconnect.js';

const initial = createReconnectState();
assert.equal(initial.attempts, 0);

// Exponential backoff
assert.equal(getDelay(initial), 1000);
assert.equal(getDelay({ ...initial, attempts: 1 }), 2000);
assert.equal(getDelay({ ...initial, attempts: 2 }), 4000);
assert.equal(getDelay({ ...initial, attempts: 10 }), 60000); // capped

// Immutable updates
const state1 = createReconnectState();
const state2 = nextAttempt(state1);
assert.equal(state1.attempts, 0);
assert.equal(state2.attempts, 1);

// Reset
const afterReset = resetAttempts({ ...initial, attempts: 5 });
assert.equal(afterReset.attempts, 0);
```

### Verification

- **Unit:** Run `reconnect.test.js`, all tests pass
- **Integration:** Boot test container, disconnect server, observe countdown increases: 1s → 2s → 4s...

---

## Phase 8: Extract `upload-queue.js` (Pure Queue State)

### What Will Be Achieved
- Extract upload queue management as a pure state reducer
- Immutable queue operations

### State Shape

```javascript
{
  files: File[],
  isUploading: boolean,
}
```

### Functions to Extract

| Function | Purpose |
|----------|---------|
| `createQueue()` | Initial empty queue |
| `enqueue(state, file)` | Add file to queue |
| `dequeue(state)` | Remove first file |
| `peek(state)` | Get first file without removing |
| `isEmpty(state)` | Check if queue empty |
| `getQueueInfo(state)` | Get display info |
| `startUploading(state)` | Mark upload in progress |
| `stopUploading(state)` | Mark upload complete |

### Steps

1. Create `static/modules/upload-queue.js` with pure state functions
2. Use immutable updates
3. Create `static/modules/upload-queue.test.js` with unit tests
4. Update `terminal-ui.js` to use these functions
5. Remove queue methods from `terminal-ui.js`

### Unit Tests

```javascript
// upload-queue.test.js
import { createQueue, enqueue, dequeue, peek, isEmpty, getQueueInfo } from './upload-queue.js';

const file1 = { name: 'test1.txt' };
const file2 = { name: 'test2.txt' };

const initial = createQueue();
assert.equal(isEmpty(initial), true);

// Enqueue - immutable
const state1 = enqueue(initial, file1);
assert.equal(initial.files.length, 0);
assert.equal(state1.files.length, 1);

// Peek
assert.equal(peek(initial), null);
assert.equal(peek(state1), file1);

// Dequeue - immutable
const state2 = enqueue(state1, file2);
const state3 = dequeue(state2);
assert.equal(state2.files.length, 2);
assert.equal(state3.files.length, 1);
assert.equal(state3.files[0], file2);

// Queue info
assert.deepEqual(getQueueInfo(state2), { current: file1, remaining: 1 });
```

### Verification

- **Unit:** Run `upload-queue.test.js`, all tests pass
- **Integration:** Boot test container, drag multiple files, observe queue count

---

## Phase 9: Extract `chunk-assembler.js` (Pure Chunk Assembly)

### What Will Be Achieved
- Extract snapshot chunk reassembly as a pure state reducer
- Critical for large terminal output handling

### State Shape

```javascript
{
  chunks: (Uint8Array | undefined)[],  // Sparse array
  expectedCount: number,
}
```

### Functions to Extract

| Function | Purpose |
|----------|---------|
| `createAssembler()` | Initial state |
| `addChunk(state, index, total, payload)` | Store chunk |
| `isComplete(state)` | All chunks received? |
| `getReceivedCount(state)` | Count received chunks |
| `assemble(state)` | Combine all chunks |
| `reset(state)` | Clear for next sequence |
| `getProgress(state)` | Get progress info |

### Steps

1. Create `static/modules/chunk-assembler.js` with pure state functions
2. Handle out-of-order chunk arrival
3. Create `static/modules/chunk-assembler.test.js` with unit tests
4. Update `terminal-ui.js` to use these functions
5. Remove chunk handling from `terminal-ui.js`

### Unit Tests

```javascript
// chunk-assembler.test.js
import { createAssembler, addChunk, isComplete, assemble, getProgress } from './chunk-assembler.js';

const initial = createAssembler();

// Add chunks out of order
const chunk0 = new Uint8Array([1, 2, 3]);
const chunk2 = new Uint8Array([7, 8, 9]);
const chunk1 = new Uint8Array([4, 5, 6]);

const state1 = addChunk(initial, 0, 3, chunk0);
const state2 = addChunk(state1, 2, 3, chunk2);
assert.equal(isComplete(state2), false);

const state3 = addChunk(state2, 1, 3, chunk1);
assert.equal(isComplete(state3), true);

// Assemble
const assembled = assemble(state3);
assert.deepEqual(Array.from(assembled), [1, 2, 3, 4, 5, 6, 7, 8, 9]);

// Progress
assert.deepEqual(getProgress(state2), { received: 2, total: 3 });
```

### Verification

- **Unit:** Run `chunk-assembler.test.js`, all tests pass
- **Integration:** Boot test container, generate large output, observe "Receiving snapshot: X/Y"

---

## Phase 10: Extract `status-renderer.js` (Pure HTML Renderers)

### What Will Be Achieved
- Extract status bar HTML generation as pure functions
- Given state, return HTML string — no DOM manipulation

### Functions to Extract

| Function | Purpose | Returns |
|----------|---------|---------|
| `renderStatusText(state)` | Main status message | HTML string |
| `renderStatusInfo(state)` | "Connected as X with Y" | HTML string |
| `renderServiceLinks(config)` | Shell/VSCode/Preview/Browser links | HTML string |
| `renderCustomLinks(linksStr)` | User-defined markdown links | HTML string |
| `getStatusBarClasses(state)` | CSS classes for status bar | string[] |

### Steps

1. Create `static/modules/status-renderer.js` with pure render functions
2. Functions return HTML strings, caller sets `innerHTML`
3. Use `escapeHtml` from `util.js` for user content
4. Create `static/modules/status-renderer.test.js` with unit tests
5. Update `terminal-ui.js` to call renderers and apply to DOM
6. Remove inline HTML generation from `terminal-ui.js`

### Unit Tests

```javascript
// status-renderer.test.js
import { renderStatusInfo, getStatusBarClasses } from './status-renderer.js';

// Connected state
const info = renderStatusInfo({
  connected: true,
  userName: 'Alice',
  assistantName: 'Claude',
  sessionName: 'my-session',
  viewers: 1,
  yoloMode: false,
});
assert.match(info, /Connected as/);
assert.match(info, /Alice/);
assert.match(info, /Claude/);

// Multiple viewers
const info2 = renderStatusInfo({ ...state, viewers: 3 });
assert.match(info2, /2 others/);

// YOLO mode
const info3 = renderStatusInfo({ ...state, yoloMode: true, yoloSupported: true });
assert.match(info3, /YOLO as/);

// Classes
assert.deepEqual(getStatusBarClasses({ connected: true, viewers: 1 }), ['connected']);
assert.deepEqual(getStatusBarClasses({ connected: true, viewers: 2 }), ['connected', 'multiuser']);
```

### Verification

- **Unit:** Run `status-renderer.test.js`, all tests pass
- **Integration:** Boot test container, verify status bar renders correctly in all states

---

## Phase 11: Extract `connection.js` (WebSocket Wrapper)

### What Will Be Achieved
- Extract WebSocket management into `static/modules/connection.js`
- Thin wrapper using pure functions from `messages.js` and `reconnect.js`

### Functions to Extract

| Function | Purpose |
|----------|---------|
| `createConnection(config)` | Create WebSocket connection |
| `disconnect(conn)` | Close connection cleanly |
| `send(conn, data)` | Send binary or JSON |
| `sendResize(conn, rows, cols)` | Send resize message |
| `sendJSON(conn, obj)` | Send JSON message |
| `startHeartbeat(conn, interval)` | Start ping interval |
| `stopHeartbeat(conn)` | Stop ping interval |

### Steps

1. Create `static/modules/connection.js`
2. Import `encodeResize` from `messages.js`
3. Handle binary vs JSON message routing
4. Create `static/modules/connection.test.js` with mock WebSocket
5. Update `terminal-ui.js` to use connection module
6. Remove connection methods from `terminal-ui.js`

### Unit Tests

```javascript
// connection.test.js (with mock WebSocket)
import { createConnection } from './connection.js';

class MockWebSocket {
  constructor(url) { this.url = url; this.readyState = 1; this.sent = []; }
  send(data) { this.sent.push(data); }
  close() { this.readyState = 3; }
}
globalThis.WebSocket = MockWebSocket;

const conn = createConnection({ url: 'ws://localhost/test', onOpen: () => {}, onBinary: () => {}, onJSON: () => {}, onClose: () => {}, onError: () => {} });
assert.equal(typeof conn.sendResize, 'function');

conn.sendResize(24, 80);
assert.equal(conn.ws.sent[0][0], 0x00);
```

### Verification

- **Unit:** Run `connection.test.js`, all tests pass
- **Integration:** Boot test container, verify connect/disconnect/heartbeat work

---

## Phase 12: Extract `terminal.js` (xterm.js Wrapper)

### What Will Be Achieved
- Extract xterm.js initialization and management into `static/modules/terminal.js`
- Centralize terminal setup, fitting, scroll proxy, link providers

### Functions to Extract

| Function | Purpose |
|----------|---------|
| `initTerminal(container, config)` | Create xterm + FitAddon |
| `fitTerminal(term, fitAddon)` | Resize terminal to container |
| `writeToTerminal(term, data)` | Write data with batching |
| `setupTouchScrollProxy(elements, term)` | iOS momentum scrolling |
| `registerLinkProviders(term, callbacks)` | File/color/URL providers |
| `disposeTerminal(term)` | Cleanup resources |

### Steps

1. Create `static/modules/terminal.js`
2. Move terminal init, touch scroll proxy, link provider registration
3. Implement write batching
4. Create `static/modules/terminal.test.js`
5. Update `terminal-ui.js` to use terminal module
6. Remove terminal setup code from `terminal-ui.js`

### Verification

- **Unit:** Test `combineArrays` helper
- **Integration:** Boot test container, verify terminal renders, typing works, links work, resize works

---

## Phase 13: Extract `chat.js` (Chat System)

### What Will Be Achieved
- Extract chat UI management into `static/modules/chat.js`

### Functions to Extract

| Function | Purpose |
|----------|---------|
| `addMessage(overlay, message, options)` | Display chat message |
| `showNotification(overlay, text, duration)` | System notification |
| `openInput(elements)` | Show chat input |
| `closeInput(elements)` | Hide chat input |
| `setupInputHandlers(elements, onSend, onClose)` | Keyboard handlers |

### Steps

1. Create `static/modules/chat.js`
2. Import `escapeHtml` from `util.js`
3. Create `static/modules/chat.test.js`
4. Update `terminal-ui.js` to use chat module
5. Remove chat methods from `terminal-ui.js`

### Verification

- **Unit:** Test message creation with mock DOM
- **Integration:** Boot test container, verify chat input opens, messages appear and fade

---

## Phase 14: Extract `upload.js` (File Upload Handling)

### What Will Be Achieved
- Extract file upload DOM handling into `static/modules/upload.js`
- Uses pure `upload-queue.js` for state management

### Functions to Extract

| Function | Purpose |
|----------|---------|
| `setupFileDrop(container, overlay, onFiles)` | Drag-drop handlers |
| `setupClipboardPaste(element, onFile)` | Paste file handling |
| `readFileAsText(file)` | Read file as string |
| `readFileAsBinary(file)` | Read file as Uint8Array |
| `updateOverlay(overlay, queueState)` | Update upload progress UI |
| `showOverlay(overlay)` / `hideOverlay(overlay)` | Toggle overlay |

Note: Move `isTextFile()` to `util.js` (it's pure)

### Steps

1. Move `isTextFile()` to `util.js`
2. Create `static/modules/upload.js`
3. Import from `upload-queue.js`, `util.js`, `messages.js`
4. Create `static/modules/upload.test.js`
5. Update `terminal-ui.js` to use upload module
6. Remove file handling methods from `terminal-ui.js`

### Verification

- **Unit:** Test `isTextFile` in util.test.js
- **Integration:** Boot test container, verify drag-drop, paste, upload queue, overlay

---

## Phase 15: Extract `mobile-keyboard.js` (Mobile Keyboard)

### What Will Be Achieved
- Extract mobile keyboard UI into `static/modules/mobile-keyboard.js`

### Functions to Extract

| Function | Purpose |
|----------|---------|
| `setupMobileKeyboard(container, callbacks)` | Initialize keyboard |
| `setupKeyboardVisibility(keyboard)` | Show/hide based on device |
| `setupViewportListeners(onKeyboardChange)` | iOS keyboard detection |
| `toggleCtrl(state)` / `toggleNav(state)` | Toggle key rows |

### Steps

1. Create `static/modules/mobile-keyboard.js`
2. Define `KEY_CODES` and `CTRL_CODES` constants
3. Update `terminal-ui.js` to use mobile keyboard module
4. Remove mobile keyboard methods from `terminal-ui.js`

### Verification

- **Integration:** Boot test container (or test on mobile), verify keyboard appears, keys send correct codes

---

## Phase 16: Extract `settings.js` (Settings Panel)

### What Will Be Achieved
- Extract settings panel UI into `static/modules/settings.js`

### Functions to Extract

| Function | Purpose |
|----------|---------|
| `setupSettingsPanel(panel, callbacks)` | Initialize panel |
| `openSettings(panel)` | Show panel |
| `closeSettings(panel)` | Hide panel |
| `populateSettings(panel, state)` | Fill form values |
| `setStatusBarColor(color)` | Update CSS variable |
| `restoreStatusBarColor()` | Load from localStorage |

### Steps

1. Create `static/modules/settings.js`
2. Import validation functions from `validation.js`
3. Update `terminal-ui.js` to use settings module
4. Remove settings methods from `terminal-ui.js`

### Verification

- **Integration:** Boot test container, verify settings panel opens, inputs work, color picker works

---

## Phase 17: Extract `split-pane.js` (Iframe Pane)

### What Will Be Achieved
- Extract split-pane UI into `static/modules/split-pane.js`

### Functions to Extract

| Function | Purpose |
|----------|---------|
| `initSplitPane(container, config)` | Initialize pane |
| `openIframePane(state, tab, url)` | Show iframe with content |
| `closeIframePane(state)` | Hide iframe pane |
| `setupResizer(elements, onResize)` | Drag to resize |
| `canShowSplitPane()` | Check viewport width |
| `handleTabClick(e, tab, url, state)` | Tab toggle behavior |

### Steps

1. Create `static/modules/split-pane.js`
2. Update `terminal-ui.js` to use split-pane module
3. Remove split-pane methods from `terminal-ui.js`

### Verification

- **Integration:** Boot test container, verify iframe pane opens, tabs work, resizer works

---

## Phase 18: Extract `status-bar.js` (Status Bar DOM)

### What Will Be Achieved
- Extract status bar DOM management into `static/modules/status-bar.js`
- Uses pure renderers from `status-renderer.js`

### Functions to Extract

| Function | Purpose |
|----------|---------|
| `updateStatus(elements, state, message)` | Update status bar |
| `updateStatusInfo(elements, session)` | Update connected info |
| `setupStatusBarListeners(statusBar, callbacks)` | Click handlers |
| `startUptimeTimer(timerEl)` | Start uptime counter |
| `stopUptimeTimer()` | Stop uptime counter |

### Steps

1. Create `static/modules/status-bar.js`
2. Import renderers from `status-renderer.js`
3. Update `terminal-ui.js` to use status-bar module
4. Remove status bar methods from `terminal-ui.js`

### Verification

- **Integration:** Boot test container, verify status bar updates, click handlers work

---

## Phase 19: Final Cleanup and Verification

### What Will Be Achieved
- Clean up `terminal-ui.js` to only contain Web Component glue code
- Verify all modules are properly imported
- Comprehensive integration testing

### Steps

1. Review `terminal-ui.js` — should be ~600 lines of glue code
2. Remove any dead code or unused imports
3. Verify module dependency graph is clean (no cycles)
4. Run all unit tests
5. Full integration test of all features

### Final `terminal-ui.js` Structure

```javascript
import { formatDuration, formatFileSize, escapeHtml, ... } from './modules/util.js';
import { validateUsername, validateSessionName } from './modules/validation.js';
import { deriveShellUUID } from './modules/uuid.js';
import { getBaseUrl, buildVSCodeUrl, ... } from './modules/url-builder.js';
import { encodeResize, parseServerMessage, ... } from './modules/messages.js';
import { createReconnectState, getDelay, ... } from './modules/reconnect.js';
import { createQueue, enqueue, ... } from './modules/upload-queue.js';
import { createAssembler, addChunk, ... } from './modules/chunk-assembler.js';
import { renderStatusInfo, getStatusBarClasses, ... } from './modules/status-renderer.js';
import { createConnection, ... } from './modules/connection.js';
import { initTerminal, ... } from './modules/terminal.js';
import { addMessage, showNotification, ... } from './modules/chat.js';
import { setupFileDrop, ... } from './modules/upload.js';
import { setupMobileKeyboard, ... } from './modules/mobile-keyboard.js';
import { setupSettingsPanel, ... } from './modules/settings.js';
import { initSplitPane, ... } from './modules/split-pane.js';
import { updateStatus, ... } from './modules/status-bar.js';

class TerminalUI extends HTMLElement {
  // ~600 lines: constructor, lifecycle, state wiring
}

customElements.define('terminal-ui', TerminalUI);
```

### Verification Checklist

- [ ] All unit tests pass
- [ ] Terminal connects and shows "Connected"
- [ ] Typing works
- [ ] Resize works
- [ ] File upload (drag-drop, paste) works
- [ ] Chat works
- [ ] Settings panel works (username, session name, color)
- [ ] Service links work (Shell, VSCode, Preview, Browser)
- [ ] Split-pane works (open, close, resize)
- [ ] Mobile keyboard works
- [ ] Reconnection works with exponential backoff
- [ ] Large output chunk reassembly works
- [ ] Link providers work (file paths, colors, URLs)
- [ ] YOLO mode toggle works (if supported)

---

## Summary

| Phase | Module | Tier | Lines Extracted |
|-------|--------|------|-----------------|
| 1 | CSS | — | ~500 |
| 2 | util.js | 1 | ~35 |
| 3 | validation.js | 1 | ~30 |
| 4 | uuid.js | 1 | ~25 |
| 5 | url-builder.js | 1 | ~40 |
| 6 | messages.js | 1 | ~60 |
| 7 | reconnect.js | 2 | ~20 |
| 8 | upload-queue.js | 2 | ~40 |
| 9 | chunk-assembler.js | 2 | ~50 |
| 10 | status-renderer.js | 3 | ~80 |
| 11 | connection.js | 4 | ~150 |
| 12 | terminal.js | 4 | ~200 |
| 13 | chat.js | 4 | ~100 |
| 14 | upload.js | 4 | ~150 |
| 15 | mobile-keyboard.js | 4 | ~150 |
| 16 | settings.js | 4 | ~200 |
| 17 | split-pane.js | 4 | ~200 |
| 18 | status-bar.js | 4 | ~100 |
| 19 | Final cleanup | — | — |

**Total lines with unit tests: Tier 1-3 = ~380 lines of pure, testable code**
