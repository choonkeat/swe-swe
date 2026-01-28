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

### Phase 3: Server Default Color (Homepage) ✅
- [x] Add settings icon/gear to homepage header
- [x] Create settings modal with color picker (10 presets + custom)
- [x] Store in localStorage: `swe-swe-primary-color`
- [x] Apply on page load
- [x] Verified: color persists after page refresh

### Phase 4: Repository Type Color ✅
- [x] Add color picker to New Session dialog (repo type section)
- [x] Store per-repo-type: `swe-swe-color-repo-{repoType}`
- [x] Override server default when set
- [x] Pass color to session via URL param `?color=hex`

### Phase 5: Session-Specific Color ✅
- [x] Read `?color=hex` URL param on session page
- [x] Apply theme via CSS variables
- [x] Store per-session: `swe-swe-color-session-{uuid}`
- [x] Settings panel header shows accent color
- [ ] (Future) Add color picker UI to session settings panel

## Progress

- [x] Research complete - identified all UI elements and colors
- [x] Phase 1: Color utilities - `color-utils.js` created
- [x] Phase 2: CSS variables - homepage updated, theming verified
- [x] Phase 3: Server default - settings gear icon + color picker modal
- [x] Phase 4: Repository type - color picker in New Session dialog
- [x] Phase 5: Session-specific - URL param + localStorage support

## Summary

All core phases complete. Users can now:
1. Set a server-wide theme color via Settings gear on homepage
2. Optionally set a per-repo-type color in New Session dialog
3. Sessions inherit colors and can receive custom colors via `?color=hex` URL param

The color cascade priority: session > repo-type > server-default > fallback (#7c3aed)

## Testing

Use dev server workflow:
```bash
make run > /tmp/server.log 2>&1 &
# Test at http://swe-swe:3000 via MCP browser
# Or App Preview at port 11977
make stop
```
