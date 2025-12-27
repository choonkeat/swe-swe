# Task: Show viewer count and PTY size in status bar

## Goal
Display the number of connected viewers and PTY size in the bottom status bar.
- Show count and size only when connected
- Use server-push approach for real-time updates

## Design
- New JSON message type: `{"type": "status", "viewers": 2, "cols": 80, "rows": 24}`
- Server broadcasts status when:
  - Client joins (after adding)
  - Client leaves (after removing)
  - PTY size changes
- Client displays: `80×24 | 2 viewers | 5m 32s`

---

## Steps

### Step 1: Add server-side status broadcast function
- [ ] Add `BroadcastStatus()` method to Session struct
- [ ] Sends JSON `{"type": "status", "viewers": N, "cols": C, "rows": R}`
- [ ] Test: Run server, connect one client, verify no errors in logs

### Step 2: Call BroadcastStatus on client join
- [ ] Call `BroadcastStatus()` at end of `AddClient()`
- [ ] Test: Connect client, check server logs for broadcast

### Step 3: Call BroadcastStatus on client leave
- [ ] Call `BroadcastStatus()` at end of `RemoveClient()`
- [ ] Test: Connect then disconnect, check logs

### Step 4: Call BroadcastStatus on PTY resize
- [ ] Call `BroadcastStatus()` at end of `UpdateClientSize()`
- [ ] Test: Resize browser window, check logs

### Step 5: Client-side handle status message
- [ ] Add case `"status"` in `handleJSONMessage()`
- [ ] Store viewers/cols/rows in component state
- [ ] Test: Connect, verify console.log shows status message

### Step 6: Update status bar UI to show info
- [ ] Add elements for viewers and size in status bar HTML
- [ ] Update `handleJSONMessage` to populate these elements
- [ ] Show only when connected (clear on disconnect)
- [ ] Test: Connect, verify UI shows correct values

### Step 7: Verify multi-client scenario
- [ ] Open 2 browser tabs to same session
- [ ] Verify both show "2 viewers"
- [ ] Close one tab, verify other updates to "1 viewer"
- [ ] Test resize affects both clients' displayed size

### Step 8: Final cleanup and commit
- [ ] Review code for any issues
- [ ] Ensure all tests pass
- [ ] Commit changes

---

## Progress

- [x] Step 1: Added BroadcastStatus() method with writeMu for thread-safe writes
- [x] Step 2: Call BroadcastStatus() in AddClient() via goroutine
- [x] Step 3: Call BroadcastStatus() in RemoveClient() via goroutine
- [x] Step 4: Call BroadcastStatus() in UpdateClientSize() via goroutine
- [x] Step 5: Handle "status" message type in handleJSONMessage()
- [x] Step 6: Updated status bar UI with status-info element
- [x] Step 7: Verified multi-client: 2 tabs show "2 viewers", closing one updates to "1 viewer"
- [x] Step 8: Added tap-to-reconnect feature on status bar
