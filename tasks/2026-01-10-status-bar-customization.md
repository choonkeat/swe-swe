# Status Bar & Font Customization

## Goal

Allow users to customize the terminal UI via `swe-swe init` flags:
- `--status-bar-color <color>` — Status bar background with auto-contrast text
- `--terminal-font-size <pixels>` — Font size for xterm
- `--terminal-font-family <font>` — Font family for xterm
- `--status-bar-font-size <pixels>` — Font size for status bar
- `--status-bar-font-family <font>` — Font family for status bar

**Purpose:** Differentiate environments (local/dev/production) similar to terminal background colors.

**Key Features:**
- Accept any CSS color (hex or named) with auto-contrast text
- Show ANSI color samples in `--help` for easy selection
- Manual verification via test container + MCP browser

## Default Values (Current Behavior)

| Flag | Default |
|------|---------|
| `--status-bar-color` | `#007acc` |
| `--terminal-font-size` | `14` |
| `--terminal-font-family` | `Menlo, Monaco, "Courier New", monospace` |
| `--status-bar-font-size` | `12` |
| `--status-bar-font-family` | `-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif` |

---

## Phase 1: Flag Parsing Infrastructure ✅ COMPLETED

### What Will Be Achieved
Add five new flags to `swe-swe init` with full persistence support via `--previous-init-flags=reuse`. No visual changes yet — pure plumbing.

### Small Steps

1. **Add fields to `InitConfig` struct** (`main.go:370-378`)
   ```go
   StatusBarColor       string `json:"statusBarColor,omitempty"`
   TerminalFontSize     int    `json:"terminalFontSize,omitempty"`
   TerminalFontFamily   string `json:"terminalFontFamily,omitempty"`
   StatusBarFontSize    int    `json:"statusBarFontSize,omitempty"`
   StatusBarFontFamily  string `json:"statusBarFontFamily,omitempty"`
   ```

2. **Add flag definitions in `handleInit()`** (`main.go:820-832`)
   ```go
   statusBarColor := fs.String("status-bar-color", "#007acc", "Status bar background color")
   terminalFontSize := fs.Int("terminal-font-size", 14, "Terminal font size in pixels")
   terminalFontFamily := fs.String("terminal-font-family", "Menlo, Monaco, \"Courier New\", monospace", "Terminal font family")
   statusBarFontSize := fs.Int("status-bar-font-size", 12, "Status bar font size in pixels")
   statusBarFontFamily := fs.String("status-bar-font-family", "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif", "Status bar font family")
   ```

3. **Add `--previous-init-flags=reuse` handling** (`main.go:958-980`)
   - Load saved values from `init.json`
   - Apply defaults for old configs without these fields

4. **Add fields to `InitConfig` instantiation** (`main.go:1252-1263`)
   - Include new fields when saving to `init.json`

5. **Add test variants in `main_test.go`** (`main_test.go:628-754`)
   ```go
   {"with-status-bar-color", []string{"--status-bar-color", "#dc2626"}},
   {"with-terminal-font", []string{"--terminal-font-size", "16", "--terminal-font-family", "JetBrains Mono"}},
   {"with-status-bar-font", []string{"--status-bar-font-size", "14", "--status-bar-font-family", "monospace"}},
   {"with-all-ui-options", []string{"--status-bar-color", "red", "--terminal-font-size", "18", "--status-bar-font-size", "14"}},
   ```

6. **Update help text** (`main.go:305-320`)

### Verification (TDD Style)

1. **Red:** Run `make test` — new test variants fail (golden files don't exist)
2. **Green:** Run `make build golden-update` — generates new golden files
3. **Verify:**
   - `git diff --cached -- cmd/swe-swe/testdata/golden` shows only `init.json` changes with new fields
   - No changes to other generated files (Dockerfile, terminal-ui.js, etc.)
   - Run `make test` — all tests pass
4. **Manual smoke test:** Run `./swe-swe init --status-bar-color red` and verify `init.json` contains the value

---

## Phase 2: CSS Color Parsing & Luminance ✅ COMPLETED

### What Will Be Achieved
Implement a reusable color utility that:
- Parses hex colors (`#rgb`, `#rrggbb`)
- Parses named CSS colors (`red`, `darkgreen`, etc.)
- Calculates relative luminance
- Returns contrasting text color (white or black)

### Small Steps

1. **Create `color.go`** with color parsing utilities
   - `ParseCSSColor(color string) (r, g, b uint8, ok bool)` — parses hex and named colors
   - `RelativeLuminance(r, g, b uint8) float64` — calculates luminance per WCAG formula
   - `ContrastingTextColor(bgColor string) string` — returns `#fff` or `#000`

2. **Add named color lookup table** (~140 standard CSS named colors)
   - `red` → `#ff0000`
   - `darkgreen` → `#006400`
   - `navy` → `#000080`
   - etc.

3. **Implement luminance calculation**
   ```go
   // WCAG relative luminance formula
   func RelativeLuminance(r, g, b uint8) float64 {
       rs := float64(r) / 255.0
       gs := float64(g) / 255.0
       bs := float64(b) / 255.0
       // Apply gamma correction
       if rs <= 0.03928 { rs = rs / 12.92 } else { rs = math.Pow((rs + 0.055) / 1.055, 2.4) }
       // ... same for gs, bs
       return 0.2126*rs + 0.7152*gs + 0.0722*bs
   }
   ```

4. **Create `color_test.go`** with comprehensive tests
   - Test hex parsing: `#fff`, `#FFF`, `#ffffff`, `#FFFFFF`
   - Test named colors: `red`, `Blue`, `DARKGREEN` (case-insensitive)
   - Test invalid inputs: `not-a-color`, `#gggggg`, empty string
   - Test luminance: black → 0.0, white → 1.0
   - Test contrast: dark bg → white text, light bg → black text

### Verification (TDD Style)

1. **Red:** Write tests first in `color_test.go`
   ```go
   func TestContrastingTextColor(t *testing.T) {
       tests := []struct{ bg, want string }{
           {"#000000", "#fff"},  // black bg → white text
           {"#ffffff", "#000"},  // white bg → black text
           {"#007acc", "#fff"},  // our blue → white text
           {"#dc2626", "#fff"},  // red → white text
           {"yellow", "#000"},   // yellow → black text
       }
       // ...
   }
   ```
2. **Green:** Implement until all tests pass
3. **Refactor:** Clean up, ensure no duplication
4. **Run:** `make test` — all tests pass, no regressions

---

## Phase 3: Template Integration ✅ COMPLETED

### What Will Be Achieved
Inject the UI customization values into `terminal-ui.js` so they affect:
- Status bar background color (with auto-contrasted text)
- Status bar font size and family
- xterm.js terminal font size and family

### Small Steps

1. **Add template placeholders to `terminal-ui.js`**

   In the CSS section (status bar styling):
   ```css
   .terminal-ui__status-bar.connected {
       background: {{STATUS_BAR_COLOR}};
       color: {{STATUS_BAR_TEXT_COLOR}};
   }
   .terminal-ui__status-bar {
       font-size: {{STATUS_BAR_FONT_SIZE}}px;
       font-family: {{STATUS_BAR_FONT_FAMILY}};
   }
   ```

   In the xterm.js initialization:
   ```javascript
   this.term = new Terminal({
       fontSize: {{TERMINAL_FONT_SIZE}},
       fontFamily: '{{TERMINAL_FONT_FAMILY}}',
       // ... other options
   });
   ```

2. **Create `processTerminalUITemplate()` function** in `main.go`
   ```go
   func processTerminalUITemplate(content string, cfg InitConfig) string {
       textColor := ContrastingTextColor(cfg.StatusBarColor)
       content = strings.ReplaceAll(content, "{{STATUS_BAR_COLOR}}", cfg.StatusBarColor)
       content = strings.ReplaceAll(content, "{{STATUS_BAR_TEXT_COLOR}}", textColor)
       content = strings.ReplaceAll(content, "{{STATUS_BAR_FONT_SIZE}}", strconv.Itoa(cfg.StatusBarFontSize))
       content = strings.ReplaceAll(content, "{{STATUS_BAR_FONT_FAMILY}}", cfg.StatusBarFontFamily)
       content = strings.ReplaceAll(content, "{{TERMINAL_FONT_SIZE}}", strconv.Itoa(cfg.TerminalFontSize))
       content = strings.ReplaceAll(content, "{{TERMINAL_FONT_FAMILY}}", cfg.TerminalFontFamily)
       return content
   }
   ```

3. **Wire up template processing** in file copying loop (`main.go:1174-1191`)
   ```go
   if hostFile == "templates/host/swe-swe-server/static/terminal-ui.js" {
       content = []byte(processTerminalUITemplate(string(content), initConfig))
   }
   ```

4. **Update golden test variants** to capture the template changes
   - Existing variants will show default values in terminal-ui.js
   - Custom variants will show overridden values

### Verification (TDD Style)

1. **Red:** Run `make test` — golden files now differ (template has placeholders but processing not wired up)
2. **Green:**
   - Implement `processTerminalUITemplate()`
   - Wire up in file copying loop
   - Run `make build golden-update`
3. **Verify:**
   - `git diff -- cmd/swe-swe/testdata/golden` shows:
     - Default variants: terminal-ui.js has default values (`#007acc`, `14`, etc.)
     - Custom variants: terminal-ui.js has overridden values (`#dc2626`, `16`, etc.)
   - Verify auto-contrast: dark colors get `#fff`, light colors get `#000`
4. **Run:** `make test` — all tests pass

---

## Phase 4: ANSI Help Output ✅ COMPLETED

### What Will Be Achieved
Enhance `--help` output with colorful ANSI samples so users can easily visualize and pick status bar colors.

### Small Steps

1. **Create `ansi.go`** with ANSI color utilities
   ```go
   // TrueColorBg returns ANSI escape for 24-bit background color
   func TrueColorBg(r, g, b uint8) string {
       return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)
   }

   // TrueColorFg returns ANSI escape for 24-bit foreground color
   func TrueColorFg(r, g, b uint8) string {
       return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)
   }

   // Reset returns ANSI reset code
   func Reset() string {
       return "\x1b[0m"
   }

   // ColorSwatch returns a colored block with label
   func ColorSwatch(cssColor, label string) string {
       r, g, b, ok := ParseCSSColor(cssColor)
       if !ok {
           return label
       }
       return TrueColorBg(r, g, b) + "  " + Reset() + " " + label
   }
   ```

2. **Define preset colors** for easy selection
   ```go
   var presetColors = []struct{ name, color string }{
       {"blue", "#007acc"},      // default
       {"red", "#dc2626"},       // production danger
       {"green", "#16a34a"},     // safe/local
       {"orange", "#ea580c"},    // staging/warning
       {"purple", "#9333ea"},    // special env
       {"gray", "#4b5563"},      // neutral
   }
   ```

3. **Create custom help text for `--status-bar-color`**
   - Override default flag help with colorful version
   - Show swatches in a grid layout:
     ```
     --status-bar-color COLOR    Status bar background (auto-contrasts text)
         Presets:  ██ blue (default)   ██ red         ██ green
                   ██ orange           ██ purple      ██ gray
         Or any CSS color: #ff5500, darkgreen, rgb(255,0,0)
     ```

4. **Add `--status-bar-color=list` option** (optional convenience)
   - If value is `list`, print color swatches and exit
   - Allows quick reference without full `--help`

5. **Create `ansi_test.go`**
   - Test escape code generation
   - Test swatch output format

### Verification (TDD Style)

1. **Red:** Write tests for ANSI utilities first
2. **Green:** Implement until tests pass
3. **Manual verification:**
   - Run `./swe-swe init --help` in terminal
   - Verify color swatches display correctly
   - Test in different terminals (if available)
4. **Run:** `make test` — all tests pass

---

## Phase 5: Manual Browser Verification ✅ COMPLETED

### What Will Be Achieved
Boot a test container and use MCP browser to visually verify:
- Status bar colors render correctly
- Auto-contrast text is legible on various backgrounds
- Font size/family changes apply to both terminal and status bar
- No regressions in default appearance

### Small Steps

1. **Boot test container** per `docs/dev/test-container-workflow.md`
   - Initialize with default settings first
   - Start the container and get the URL

2. **Verify default appearance**
   - Navigate to terminal UI via MCP browser
   - Take screenshot of status bar showing `#007acc` blue
   - Verify white text is legible
   - Verify default fonts render correctly

3. **Test custom status bar color (dark)**
   - Re-init with `--status-bar-color "#dc2626"` (red)
   - Rebuild/restart container
   - Navigate and verify:
     - Status bar is red
     - Text is white (auto-contrast)
     - Screenshot for reference

4. **Test custom status bar color (light)**
   - Re-init with `--status-bar-color "yellow"` or `#fbbf24`
   - Verify:
     - Status bar is yellow
     - Text is black (auto-contrast)
     - Screenshot for reference

5. **Test font customizations**
   - Re-init with `--terminal-font-size 18 --terminal-font-family "Courier New"`
   - Re-init with `--status-bar-font-size 16 --status-bar-font-family monospace`
   - Verify fonts changed visually
   - Screenshot for reference

6. **Test combined customizations**
   - Re-init with all options: color + fonts
   - Verify everything works together
   - Screenshot for reference

7. **Shutdown test container**
   - Clean up per workflow docs

### Verification Checklist

| Test Case | Expected Result |
|-----------|-----------------|
| Default blue (`#007acc`) | White text, legible |
| Dark red (`#dc2626`) | White text, legible |
| Light yellow (`yellow`) | Black text, legible |
| Named color (`darkgreen`) | Parses correctly, white text |
| Terminal font size 18 | Larger terminal text |
| Status bar font monospace | Monospace in status bar |
| All options combined | Everything applies correctly |
