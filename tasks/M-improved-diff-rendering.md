# Feature: Improved Diff Rendering

## Overview
Replace the current poor diff rendering (large red/green blocks) with a professional GitHub-style diff viewer that shows line-by-line changes with character-level highlighting and proper context.

## Current Problem
The existing `renderDiff` function in `/elm/src/Main.elm` (lines 1635-1669) simply displays:
- All old lines in red blocks
- All new lines in green blocks
- No context lines or character-level highlighting
- Hard to see what actually changed

## Solution: Web Component Approach

### Implementation Plan
1. **Add diff2html web component** via `@lrnwebcomponents/lrndesign-diff2html`
2. **Modify Elm template** to include the web component script
3. **Update diff rendering** to use the web component instead of custom HTML
4. **Configure view options** (inline vs side-by-side)

### Key Features
- **GitHub-style appearance** with proper styling
- **Character-level highlighting** within changed lines
- **Context lines** showing unchanged code around changes
- **Side-by-side vs inline views** switchable
- **Syntax highlighting** for code diffs
- **No Elm ports needed** - framework-agnostic web component

## Technical Implementation

### 1. Web Component Integration
```html
<!-- In index.html template -->
<script type="module" src="@lrnwebcomponents/lrndesign-diff2html.js"></script>
```

### 2. Elm Template Update
Replace current `renderDiff` function output with:
```html
<lrndesign-diff2html 
  diff-string="--- a/file.txt\n+++ b/file.txt\n@@ -1,3 +1,3 @@\n-old line\n+new line\n unchanged line"
  output-format="line-by-line"
  highlight-code="true">
</lrndesign-diff2html>
```

### 3. Diff Generation
Convert current old/new string pairs to unified diff format:
- Generate proper diff headers (`--- a/file` `+++ b/file`)
- Create hunks with `@@` markers
- Use jsdiff library for proper diff computation if needed

### 4. Configuration Options
```javascript
// Web component attributes
{
  "output-format": "line-by-side" | "side-by-side",
  "highlight-code": "true",
  "matching": "lines" | "words" | "none",
  "synchronised-scroll": "true", // for side-by-side
  "draw-file-list": "false" // since we're showing single diffs
}
```

## Files to Modify

### Primary Changes
1. **`/elm/src/Main.elm`**
   - Update `renderDiff` function (line 1635-1669)
   - Update `renderEditAsDiff` function (line 1676-1693) 
   - Update Edit tool rendering (line 1892-1914)
   - Update MultiEdit tool rendering (line 1916-1949)

2. **`/cmd/swe-swe/index.html.tmpl`**
   - Add web component script import
   - Add diff2html CSS if needed

3. **`/cmd/swe-swe/static/css/styles.css`**
   - Remove existing diff styles (lines 1173-1282)
   - Add minimal integration styles for web component

### Package Management
```bash
# Add to package.json or install globally
npm install @lrnwebcomponents/lrndesign-diff2html
```

## Expected Results

### Before (Current)
```
Changes to apply:
- old line 1
- old line 2  
- old line 3
+ new line 1
+ new line 2
+ new line 3
```

### After (Improved)
```
filename.ext
  1  | unchanged line
- 2  | old line with highlighting
+ 2  | new line with highlighting  
  3  | unchanged line
```

With:
- Character-level highlighting within changed lines
- Proper context lines
- Syntax highlighting
- Professional GitHub appearance
- Optional side-by-side view

## Benefits
- **Dramatically improved readability** - easy to see exact changes
- **Professional appearance** matching modern dev tools
- **Zero ports complexity** - pure web component integration  
- **Multiple view modes** - inline and side-by-side
- **Syntax highlighting** for code diffs
- **Better UX** for reviewing Edit/MultiEdit operations

## Implementation Steps
1. **Install web component package** and update build process
2. **Modify Elm template** to include web component
3. **Replace renderDiff function** to generate unified diff format
4. **Update tool rendering** for Edit/MultiEdit tools
5. **Remove old diff CSS** and add integration styles
6. **Test with various diff scenarios** (small/large, code/text)

## Estimation

### T-Shirt Size: M (Medium)

### Breakdown
- **Web component integration**: S
  - Add package and script import
  - Update build process if needed
  
- **Elm diff generation**: M  
  - Convert old/new strings to unified diff format
  - Update renderDiff, renderEditAsDiff functions
  - Handle edge cases (empty strings, no changes)
  
- **Template and styling updates**: S
  - Update HTML template
  - Remove old CSS, add integration styles
  - Ensure theme compatibility
  
- **Testing and refinement**: S
  - Test with various diff scenarios
  - Verify appearance in all themes
  - Check mobile responsiveness

### Risk Assessment
- **Low technical risk** - web components are well-supported
- **Low breaking change risk** - only affects visual rendering
- **Medium dependency risk** - adds external web component dependency
- **High value impact** - dramatically improves user experience

## Success Criteria
- [ ] No more large red/green blocks
- [ ] Character-level highlighting visible  
- [ ] Context lines show unchanged code
- [ ] Works in all theme modes
- [ ] Side-by-side view available as option
- [ ] Syntax highlighting works for code files
- [ ] Mobile-friendly responsive design
- [ ] Performance acceptable for large diffs