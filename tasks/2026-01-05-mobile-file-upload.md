# Mobile File Upload Feature

## Goal

Add a paperclip button to the mobile keyboard input bar that triggers a file chooser, using the same upload process that desktop drag-and-drop already uses.

## Phases

1. **Phase 1: Add UI elements (HTML + CSS)**
2. **Phase 2: Wire up event handlers**
3. **Phase 3: Test and verify**

---

## Phase 1: Add UI elements (HTML + CSS)

### What will be achieved
A paperclip button will appear in the mobile keyboard input bar (to the left of the textarea), and a hidden file input element will be added to support file selection.

### Small steps

1. **Add HTML elements** (in `render()` method, ~line 740):
   - Add `<button class="mobile-keyboard__attach">` with a paperclip SVG icon before the textarea
   - Add `<input type="file" class="mobile-keyboard__file-input" multiple hidden>` to support multi-file selection

2. **Add CSS styling** (in the `<style>` block, ~line 253 near other mobile-keyboard styles):
   - Style `.mobile-keyboard__attach` button to match the Enter button aesthetics but be more subtle (icon-only, same height)
   - Ensure the button is touch-friendly (minimum 44px tap target)

### Verification

1. **Visual check**: Load the page on mobile (or mobile viewport in dev tools), confirm:
   - Paperclip button appears to the left of the textarea
   - Button is visually consistent with the design
   - Button doesn't break the layout of existing elements

2. **Desktop regression check**: Load on desktop, confirm:
   - Mobile keyboard is still hidden on desktop (existing behavior)
   - Drag-and-drop still shows the overlay and works normally

---

## Phase 2: Wire up event handlers

### What will be achieved
Clicking the paperclip button opens the native file picker. When files are selected, they're added to the existing upload queue and processed using the same logic as drag-and-drop.

### Small steps

1. **Add click handler for paperclip button** (in `setupMobileKeyboard()` method, ~line 1640):
   - Query for `.mobile-keyboard__attach` button
   - Query for `.mobile-keyboard__file-input` input
   - On button click, trigger `fileInput.click()` to open native file picker

2. **Add change handler for file input** (same location):
   - Listen for `change` event on the file input
   - Loop through `fileInput.files` and call `this.addFileToQueue(file)` for each
   - Call `this.processUploadQueue()` if queue was empty before adding
   - Reset the file input value (`fileInput.value = ''`) so the same file can be re-selected

### Code (reuses existing upload queue)

```javascript
// In setupMobileKeyboard()
const attachBtn = this.querySelector('.mobile-keyboard__attach');
const fileInput = this.querySelector('.mobile-keyboard__file-input');

attachBtn.addEventListener('click', () => fileInput.click());

fileInput.addEventListener('change', () => {
    const wasEmpty = this.uploadQueue.length === 0;
    for (const file of fileInput.files) {
        this.addFileToQueue(file);  // <-- existing method
    }
    if (wasEmpty && this.uploadQueue.length > 0) {
        this.processUploadQueue();   // <-- existing method
    }
    fileInput.value = '';
});
```

### Verification

1. **Functional check on mobile**:
   - Tap paperclip -> file picker opens
   - Select a text file (e.g., `.txt`, `.md`) -> content is pasted into terminal
   - Select a binary file (e.g., image) -> file is uploaded, path sent to terminal
   - Select multiple files -> all are queued and processed sequentially

2. **Desktop regression check**:
   - Drag-and-drop still works
   - Paste file from clipboard still works
   - Upload overlay shows correctly during uploads

3. **Edge cases**:
   - Cancel file picker without selecting -> nothing happens (no error)
   - Select same file twice in a row -> works both times (due to input value reset)

---

## Phase 3: Test and verify

### What will be achieved
Deploy a test instance using the scripts and verify the feature using Playwright MCP.

### Small steps

1. **Build the binary**:
   ```bash
   make build
   ```

2. **Deploy test instance** (run sequentially):
   ```bash
   ./scripts/01-test-container-init.sh
   ./scripts/02-test-container-build.sh
   HOST_PORT=11977 HOST_IP=host.docker.internal ./scripts/03-test-container-run.sh
   ```

3. **Test with Playwright MCP**:
   - Navigate to `http://host.docker.internal:11977/`
   - Resize browser to mobile viewport (e.g., 375x667)
   - Take snapshot to verify paperclip button is visible
   - Click paperclip button -> verify file picker behavior
   - Test file upload flow

4. **Desktop regression** (same instance):
   - Resize to desktop viewport
   - Verify mobile keyboard is hidden
   - Test drag-and-drop still works (if possible via Playwright)

5. **Teardown**:
   ```bash
   ./scripts/04-test-container-down.sh
   ```

### Verification method

Automated testing via Playwright MCP browser tools against the test container instance.

---

## Files to modify

- `/workspace/cmd/swe-swe/templates/host/swe-swe-server/static/terminal-ui.js`
  - HTML: ~line 740 (mobile-keyboard__input div)
  - CSS: ~line 253 (after mobile-keyboard__send styles)
  - JS: ~line 1640 (in setupMobileKeyboard method)

## Existing code to reuse (no duplication)

| Method | Location | Purpose |
|--------|----------|---------|
| `addFileToQueue(file)` | ~line 2002 | Adds a File object to the queue |
| `processUploadQueue()` | ~line 2030 | Processes files sequentially |
| `isTextFile(file)` | ~line 1963 | Determines text vs binary handling |
| Upload overlay logic | ~line 2070 | Shows progress UI |
