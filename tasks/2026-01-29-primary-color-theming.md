# Primary Color Theming

## Goal

Allow users to pick a primary color at 3 levels to distinguish servers/sessions:
1. **Server default** - Homepage setting for all sessions on this server
2. **Repository Type** - Per-repo-type color in the New Session dialog
3. **Session-specific** - Per-session override on the session page

## Current State

- ~~Primary accent: `#7c3aed` (purple) hardcoded throughout~~ Now uses CSS variables
- ~~No contrast calculation code exists~~ `color-utils.js` created
- No color picker UI exists yet

## UI Elements to Theme

### Homepage (selection.html)
- [x] Logo background
- [x] Primary buttons (New Session, Start)
- [x] Section count badges
- [x] Card selected/focus borders (dialog agents)
- [x] Input focus borders
- [x] Play button backgrounds
- [x] Spinner accent

### Session Page (terminal-ui.css + index.html)
- [x] `--accent-primary` CSS variable
- [x] `--accent-hover` CSS variable
- [x] Additional variables: `--accent-dark`, `--accent-light`, `--accent-10/20/30`, `--accent-text`, `--accent-gradient`
- [ ] Header back button background (uses `--bg-elevated`, not accent)
- [ ] Status indicators using accent

## Implementation Plan

### Phase 1: Color Utility Functions ✅
- [x] Create `color-utils.js` with:
  - `hexToRgb()` - parse hex colors
  - `rgbToHex()` - convert back to hex
  - `getLuminance()` - WCAG luminance calculation
  - `getContrastingTextColor()` - white or black based on bg
  - `adjustColor()` - lighten/darken by percentage
  - `generatePalette()` - derive all variants from one color

### Phase 2: CSS Variable Infrastructure ✅
- [x] Add CSS variables for derived colors in terminal-ui.css
- [x] Update selection.html to use CSS variables instead of hardcoded colors
- [x] Create `applyTheme(primaryColor)` function
- [x] Verified: purple, blue, yellow themes all work with correct text contrast

### Phase 3: Server Default Color (Homepage)
- [ ] Add settings icon/gear to homepage header
- [ ] Create settings modal with color picker
- [ ] Store in localStorage: `swe-swe-primary-color`
- [ ] Apply on page load

### Phase 4: Repository Type Color
- [ ] Add color picker to New Session dialog (repo type section)
- [ ] Store per-repo-type: `swe-swe-color-{repoType}`
- [ ] Override server default when set

### Phase 5: Session-Specific Color
- [ ] Add color picker to session page (settings panel or header)
- [ ] Store per-session: `swe-swe-color-session-{uuid}`
- [ ] Pass via URL param for sharing: `?color=hex`

## Progress

- [x] Research complete - identified all UI elements and colors
- [x] Phase 1: Color utilities - `color-utils.js` created
- [x] Phase 2: CSS variables - homepage updated, theming verified
- [ ] Phase 3: Server default - need settings UI
- [ ] Phase 4: Repository type
- [ ] Phase 5: Session-specific

## Testing

Use dev server workflow:
```bash
make run > /tmp/server.log 2>&1 &
# Test at http://swe-swe:3000 via MCP browser
# Or App Preview at port 11977
make stop
```
