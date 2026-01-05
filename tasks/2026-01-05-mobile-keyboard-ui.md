# Mobile Keyboard UI

## Goal

Implement a mobile-friendly keyboard UI for the terminal that provides:
- Quick-access keys (Esc, Tab, ⇧Tab) always visible
- Expandable Ctrl modifier row for common control sequences
- Expandable Nav row for arrow keys
- Smart input bar with context-aware Enter/Send button

## Design

```
┌─────────────────────────────────────────────────────────┐
│                       Terminal                          │
│  $ _                                                    │
├─────────────────────────────────────────────────────────┤
│                                                         │
│  NORMAL MODE:                                           │
│  ┌────────┐┌────────┐┌────────┐┌──────────┐┌──────────┐ │
│  │  Esc   ││  Tab   ││  ⇧Tab  ││  Ctrl... ││  Nav...  │ │
│  └────────┘└────────┘└────────┘└──────────┘└──────────┘ │
│                                                         │
├─────────────────────────────────────────────────────────┤
│                                                         │
│  CTRL TOGGLED:                                          │
│  ┌────────┐┌────────┐┌────────┐┌──────────┐┌──────────┐ │
│  │  Esc   ││  Tab   ││  ⇧Tab  ││  ■ Ctrl  ││  Nav...  │ │
│  └────────┘└────────┘└────────┘└──────────┘└──────────┘ │
│  ┌───────┐┌───────┐┌───────┐┌───────┐┌───────┐┌───────┐ │
│  │   A   ││   C   ││   D   ││   E   ││   K   ││   W   │ │
│  └───────┘└───────┘└───────┘└───────┘└───────┘└───────┘ │
│                                                         │
├─────────────────────────────────────────────────────────┤
│                                                         │
│  NAV TOGGLED:                                           │
│  ┌────────┐┌────────┐┌────────┐┌──────────┐┌──────────┐ │
│  │  Esc   ││  Tab   ││  ⇧Tab  ││  Ctrl... ││  ■ Nav   │ │
│  └────────┘└────────┘└────────┘└──────────┘└──────────┘ │
│  ┌──────────┐┌──────────┐┌──────────┐┌──────────┐       │
│  │    ←     ││    →     ││    ↑     ││    ↓     │       │
│  └──────────┘└──────────┘└──────────┘└──────────┘       │
│                                                         │
├─────────────────────────────────────────────────────────┤
│                                                         │
│  BOTH TOGGLED:                                          │
│  ┌────────┐┌────────┐┌────────┐┌──────────┐┌──────────┐ │
│  │  Esc   ││  Tab   ││  ⇧Tab  ││  ■ Ctrl  ││  ■ Nav   │ │
│  └────────┘└────────┘└────────┘└──────────┘└──────────┘ │
│  ┌───────┐┌───────┐┌───────┐┌───────┐┌───────┐┌───────┐ │
│  │   A   ││   C   ││   D   ││   E   ││   K   ││   W   │ │
│  └───────┘└───────┘└───────┘└───────┘└───────┘└───────┘ │
│  ┌──────────┐┌──────────┐┌──────────┐┌──────────┐       │
│  │    ←     ││    →     ││    ↑     ││    ↓     │       │
│  └──────────┘└──────────┘└──────────┘└──────────┘       │
│                                                         │
├─────────────────────────────────────────────────────────┤
│  ┌─────────────────────────────────────────┐ ┌────────┐ │
│  │ Type command...                         │ │ Enter  │ │  ← empty
│  └─────────────────────────────────────────┘ └────────┘ │
│                                                         │
│  ┌─────────────────────────────────────────┐ ┌────────┐ │
│  │ git status                              │ │  Send  │ │  ← has text
│  └─────────────────────────────────────────┘ └────────┘ │
└─────────────────────────────────────────────────────────┘
```

## Key Mappings

### Main Row
| Button | Sends | Description |
|--------|-------|-------------|
| Esc | `\x1b` | Escape |
| Tab | `\t` | Tab |
| ⇧Tab | `\x1b[Z` | Shift-Tab (reverse tab) |
| Ctrl... | (toggle) | Shows/hides Ctrl row |
| Nav... | (toggle) | Shows/hides Nav row |

### Ctrl Row (when expanded)
| Button | Sends | Description |
|--------|-------|-------------|
| A | `\x01` | Beginning of line |
| C | `\x03` | Interrupt/cancel |
| D | `\x04` | EOF / exit |
| E | `\x05` | End of line |
| K | `\x0B` | Kill to end of line |
| W | `\x17` | Delete word backward |

### Nav Row (when expanded)
| Button | Sends | Description |
|--------|-------|-------------|
| ← | `\x1b[D` | Cursor left |
| → | `\x1b[C` | Cursor right |
| ↑ | `\x1b[A` | Cursor up / history prev |
| ↓ | `\x1b[B` | Cursor down / history next |

### Input Bar
| State | Button | Behavior |
|-------|--------|----------|
| Empty | Enter | Sends `\r` (Enter keystroke) |
| Has text | Send | Sends `text + \r` (command + Enter) |

## Toggle Behavior

- **Sticky toggles**: Ctrl and Nav remain active until explicitly toggled off
- **Both can be active**: Ctrl and Nav rows can both be visible simultaneously
- **Visual indicator**: Active toggle shows `■` prefix, inactive shows `...` suffix

---

## Phase 1: HTML Structure ✅

### What will be achieved
Add the mobile keyboard UI HTML elements to the terminal template, replacing the old `.terminal-ui__extra-keys`.

### Steps

1. **Remove old extra-keys HTML** - Delete the `.terminal-ui__extra-keys` div (lines 640-649)
2. **Add `.mobile-keyboard` container** - Wrapper for all keyboard elements
3. **Add `.mobile-keyboard__main` row** - Esc, Tab, ⇧Tab, Ctrl..., Nav...
4. **Add `.mobile-keyboard__ctrl` row** - A, C, D, E, K, W (hidden by default)
5. **Add `.mobile-keyboard__nav` row** - ←, →, ↑, ↓ (hidden by default)
6. **Add `.mobile-keyboard__input` bar** - Text input + Enter/Send button
7. **Add data attributes** - `data-key` for direct keys, `data-toggle` for Ctrl/Nav, `data-ctrl` for Ctrl letters

### HTML Structure

```html
<div class="mobile-keyboard">
    <div class="mobile-keyboard__main">
        <button data-key="Escape">Esc</button>
        <button data-key="Tab">Tab</button>
        <button data-key="ShiftTab">⇧Tab</button>
        <button data-toggle="ctrl" class="mobile-keyboard__toggle">Ctrl</button>
        <button data-toggle="nav" class="mobile-keyboard__toggle">Nav</button>
    </div>
    <div class="mobile-keyboard__ctrl">
        <button data-ctrl="a">A</button>
        <button data-ctrl="c">C</button>
        <button data-ctrl="d">D</button>
        <button data-ctrl="e">E</button>
        <button data-ctrl="k">K</button>
        <button data-ctrl="w">W</button>
    </div>
    <div class="mobile-keyboard__nav">
        <button data-key="ArrowLeft">←</button>
        <button data-key="ArrowRight">→</button>
        <button data-key="ArrowUp">↑</button>
        <button data-key="ArrowDown">↓</button>
    </div>
    <div class="mobile-keyboard__input">
        <input type="text" placeholder="Type command..." class="mobile-keyboard__text">
        <button class="mobile-keyboard__send">Enter</button>
    </div>
</div>
```

### Verification

1. **Visual check** - Load page, inspect DOM to confirm elements exist
2. **No regression** - Page loads without JS errors

---

## Phase 2: CSS Styling

### What will be achieved
Style the mobile keyboard UI with toggle states and input bar.

### Steps

1. **Remove old `.terminal-ui__extra-keys` CSS** - Delete existing styles
2. **Add `.mobile-keyboard` container styles** - Flexbox column, dark background, border-top
3. **Add `.mobile-keyboard__main` row styles** - Flex row, gap, padding
4. **Add button base styles** - Min-width, padding 12px, touch-friendly, monospace font
5. **Add toggle button styles** - Shared CSS for `...` suffix and `■` prefix states
6. **Add `.mobile-keyboard__ctrl` row styles** - Hidden by default, flex when `.visible`
7. **Add `.mobile-keyboard__nav` row styles** - Hidden by default, flex when `.visible`
8. **Add `.mobile-keyboard__input` styles** - Flex row, input field stretches, button fixed

### CSS for Toggle States (shared)

```css
.mobile-keyboard__toggle::after {
    content: '...';
}
.mobile-keyboard__toggle.active::before {
    content: '■ ';
}
.mobile-keyboard__toggle.active::after {
    content: '';
}
```

### Verification

1. **Visual check** - Buttons render with correct sizing and colors
2. **Toggle visual check** - Manually add `.active` class in DevTools, verify indicator changes
3. **Touch target check** - Buttons are at least 44px tall

---

## Phase 3: JavaScript Logic

### What will be achieved
Implement the interactive behavior for toggles, key sending, and input bar.

### Steps

1. **Remove old extra-keys JS** - Delete `keyMap`, old button handlers, old `ctrlPressed` logic
2. **Add state properties** - `ctrlActive`, `navActive` in constructor
3. **Add `toggleCtrl()` method** - Toggle state, update button class, show/hide Ctrl row
4. **Add `toggleNav()` method** - Toggle state, update button class, show/hide Nav row
5. **Add `sendKey(code)` method** - Encode and send via WebSocket
6. **Add main row handlers** - Esc, Tab, ⇧Tab buttons
7. **Add Ctrl row handlers** - A, C, D, E, K, W buttons with Ctrl codes
8. **Add Nav row handlers** - Arrow buttons
9. **Add input bar logic** - Update button text on input change
10. **Add send handlers** - Enter sends `\r`, Send sends `text + \r`

### Key Code Constants

```javascript
const CTRL_CODES = {
    'a': '\x01', // Beginning of line
    'c': '\x03', // Interrupt
    'd': '\x04', // EOF
    'e': '\x05', // End of line
    'k': '\x0B', // Kill to end
    'w': '\x17'  // Delete word
};

const KEY_CODES = {
    'Escape': '\x1b',
    'Tab': '\t',
    'ShiftTab': '\x1b[Z',
    'ArrowLeft': '\x1b[D',
    'ArrowRight': '\x1b[C',
    'ArrowUp': '\x1b[A',
    'ArrowDown': '\x1b[B'
};
```

### Verification

1. **Console log check** - Each button logs correct code before sending
2. **Toggle state check** - Rows appear/disappear, button classes update
3. **Input bar check** - Button text changes between Enter/Send
4. **No regression** - Existing terminal input still works

---

## Phase 4: Integration & Testing

### What will be achieved
End-to-end verification with running swe-swe server.

### Steps

1. **Build the binary** - `make build`
2. **Start test container** - Run scripts to spin up server
3. **Open in MCP browser** - Navigate to `http://<HOST_IP>:<PORT>/`
4. **Test main row keys** - Esc, Tab, ⇧Tab
5. **Test Ctrl toggle** - Verify row appears, stays sticky
6. **Test Ctrl keys** - Each letter sends correct code
7. **Test Nav toggle** - Verify row appears, stays sticky
8. **Test Nav keys** - Arrows move cursor
9. **Test input bar empty** - "Enter" button sends `\r`
10. **Test input bar with text** - "Send" button sends text + `\r`
11. **Test untoggle** - Rows hide when toggled off
12. **Run golden tests** - `make build golden-update`

### Verification

1. **Functional test** - Keys produce expected terminal behavior
2. **Visual test** - Toggle states display correctly
3. **Regression test** - Chat, file upload, resize still work
4. **Golden test** - Diff shows only keyboard changes

---

## Phase 5: Mobile-Only Display (Future)

### What will be achieved
Restrict keyboard visibility to mobile-width screens.

### Steps

1. **Add media query** - Hide keyboard at ≥768px
2. **Test on desktop** - Verify hidden
3. **Test on mobile** - Verify visible
4. **Update golden tests**

```css
@media (min-width: 768px) {
    .mobile-keyboard {
        display: none;
    }
}
```

### Verification

1. **Desktop check** - Keyboard not visible at ≥768px
2. **Mobile check** - Keyboard visible at <768px
3. **Breakpoint check** - Toggle at exactly 768px

---

## Replaces

This implementation replaces the existing `.terminal-ui__extra-keys`:

```html
<!-- OLD - TO BE REMOVED -->
<div class="terminal-ui__extra-keys">
    <button data-key="Escape">ESC</button>
    <button data-key="Tab">TAB</button>
    <button data-modifier="ctrl" class="modifier">Ctrl</button>
    <button data-key="ArrowUp">↑</button>
    <button data-key="ArrowDown">↓</button>
    <button data-key="ArrowLeft">←</button>
    <button data-key="ArrowRight">→</button>
    <button data-action="paste">Paste</button>
</div>
```

And related JavaScript:
- `ctrlPressed` state property
- `keyMap` object
- Extra-keys button event handlers (lines 1492-1552)
