# Debug Channel: eval Support

**Date**: 2026-02-01
**Status**: Pending
**Source**: research/2026-02-01-preview-tab-enhancements.md (Section 2)

## Goal

Add `eval` command support to the debug channel so agents can execute arbitrary JS in the preview iframe.

## Protocol

Agent sends:
```json
{ "t": "eval", "id": "unique-id", "code": "document.title" }
```

Iframe responds:
```json
{ "t": "evalResult", "id": "unique-id", "result": "My Page Title" }
```

On error:
```json
{ "t": "evalResult", "id": "unique-id", "error": "ReferenceError: x is not defined", "stack": "..." }
```

## Implementation

Add ~15 lines to `debugInjectJS` command handler in main.go:

```js
if (cmd.t === 'eval') {
  try {
    var result = (0, eval)(cmd.code);  // indirect eval = global scope
    Promise.resolve(result).then(function(val) {
      send({ t: 'evalResult', id: cmd.id, result: serialize(val) });
    }).catch(function(err) {
      send({ t: 'evalResult', id: cmd.id, error: err.message });
    });
  } catch (e) {
    send({ t: 'evalResult', id: cmd.id, error: e.message, stack: e.stack });
  }
}
```

## Security

Per ADR-024, the debug channel is behind the same `forwardAuth` middleware as the preview. An agent that can connect to `/__swe-swe-debug__/agent` already has full access to the user's app. Eval does not expand the attack surface.

## Files to change

| File | Change |
|------|--------|
| `cmd/swe-swe/templates/host/swe-swe-server/main.go` | Add eval handler to `debugInjectJS` (~line 1441) |

## Verification

1. `make run` per docs/dev/swe-swe-server-workflow.md
2. Connect to debug WebSocket at `/__swe-swe-debug__/agent`
3. Send eval command, verify result returns correctly
4. Send eval with invalid code, verify error response
5. Send eval returning a Promise, verify async result
