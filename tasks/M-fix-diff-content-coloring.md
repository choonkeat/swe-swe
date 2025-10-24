# Fix Diff Content Coloring Issue

## Problem Description

From `diff-content-not-colored.png`, the diff viewer shows:
- ✅ Green left margin (working correctly) 
- ❌ Diff content text is not colored (missing proper styling)
- The actual text content in diff lines appears in default text color instead of diff-specific colors

## Root Cause Analysis

After analyzing the CSS in `/workspace/cmd/swe-swe/static/css/styles.css` and the Elm rendering in `/workspace/elm/src/Main.elm`:

### Current CSS Structure (lines 1204-1216)
```css
.diff-line.diff-old {
    background-color: rgba(255, 0, 0, 0.1);
    color: #ff6b6b;
}

.diff-line.diff-new {
    background-color: rgba(0, 255, 0, 0.1);
    color: #51cf66;
}
```

### Current Elm HTML Structure (lines 1688-1703)
```elm
div [ class "diff-line diff-added" ]
    [ span [ class "diff-marker" ] [ text "+ " ]
    , span [ class "diff-content" ] [ text content ]
    ]
```

### The Problem
The CSS sets colors on `.diff-line` but the actual text content is inside nested `.diff-content` spans. The color inheritance isn't working properly, especially across different themes.

## Solution Implementation

### Step 1: Enhance CSS for Diff Content
Add explicit styling for `.diff-content` spans in `/workspace/cmd/swe-swe/static/css/styles.css`:

**Add after line 1216:**
```css
/* Explicit diff content text coloring */
.diff-line.diff-old .diff-content {
    color: #d73a49; /* Red for removed content */
}

.diff-line.diff-new .diff-content {
    color: #28a745; /* Green for added content */
}

.diff-line.diff-added .diff-content {
    color: #24292f; /* Dark text on light green background */
}

.diff-line.diff-removed .diff-content {
    color: #24292f; /* Dark text on light red background */
}

.diff-line.diff-context .diff-content {
    color: var(--text-tertiary);
}

/* Ensure diff markers also get proper coloring */
.diff-line.diff-old .diff-marker {
    color: #d73a49;
    font-weight: bold;
}

.diff-line.diff-new .diff-marker {
    color: #28a745;
    font-weight: bold;
}

.diff-line.diff-added .diff-marker {
    color: #28a745;
    font-weight: bold;
}

.diff-line.diff-removed .diff-marker {
    color: #d73a49;
    font-weight: bold;
}
```

### Step 2: Update Dark Theme Support
Enhance the existing dark theme diff styling (after line 1604):

```css
/* Dark theme diff content coloring */
.messages[style*="background-color: rgb(13, 17, 23)"] .diff-line.diff-old .diff-content,
.messages[style*="background-color: rgb(30, 30, 30)"] .diff-line.diff-old .diff-content,
.messages[style*="background-color: rgb(0, 0, 0)"] .diff-line.diff-old .diff-content,
.messages[style*="background-color: rgb(26, 27, 38)"] .diff-line.diff-old .diff-content,
.messages[style*="background-color: rgb(0, 43, 54)"] .diff-line.diff-old .diff-content {
    color: #ff8787; /* Lighter red for dark themes */
}

.messages[style*="background-color: rgb(13, 17, 23)"] .diff-line.diff-new .diff-content,
.messages[style*="background-color: rgb(30, 30, 30)"] .diff-line.diff-new .diff-content,
.messages[style*="background-color: rgb(0, 0, 0)"] .diff-line.diff-new .diff-content,
.messages[style*="background-color: rgb(26, 27, 38)"] .diff-line.diff-new .diff-content,
.messages[style*="background-color: rgb(0, 43, 54)"] .diff-line.diff-new .diff-content {
    color: #6bcf7f; /* Lighter green for dark themes */
}

.messages[style*="background-color: rgb(13, 17, 23)"] .diff-line.diff-added .diff-content,
.messages[style*="background-color: rgb(30, 30, 30)"] .diff-line.diff-added .diff-content,
.messages[style*="background-color: rgb(0, 0, 0)"] .diff-line.diff-added .diff-content,
.messages[style*="background-color: rgb(26, 27, 38)"] .diff-line.diff-added .diff-content,
.messages[style*="background-color: rgb(0, 43, 54)"] .diff-line.diff-added .diff-content {
    color: #adbac7; /* Light text on dark green background */
}

.messages[style*="background-color: rgb(13, 17, 23)"] .diff-line.diff-removed .diff-content,
.messages[style*="background-color: rgb(30, 30, 30)"] .diff-line.diff-removed .diff-content,
.messages[style*="background-color: rgb(0, 0, 0)"] .diff-line.diff-removed .diff-content,
.messages[style*="background-color: rgb(26, 27, 38)"] .diff-line.diff-removed .diff-content,
.messages[style*="background-color: rgb(0, 43, 54)"] .diff-line.diff-removed .diff-content {
    color: #adbac7; /* Light text on dark red background */
}

/* Dark theme diff markers */
.messages[style*="background-color: rgb(13, 17, 23)"] .diff-line.diff-old .diff-marker,
.messages[style*="background-color: rgb(30, 30, 30)"] .diff-line.diff-old .diff-marker,
.messages[style*="background-color: rgb(0, 0, 0)"] .diff-line.diff-old .diff-marker,
.messages[style*="background-color: rgb(26, 27, 38)"] .diff-line.diff-old .diff-marker,
.messages[style*="background-color: rgb(0, 43, 54)"] .diff-line.diff-old .diff-marker {
    color: #ff8787;
}

.messages[style*="background-color: rgb(13, 17, 23)"] .diff-line.diff-new .diff-marker,
.messages[style*="background-color: rgb(30, 30, 30)"] .diff-line.diff-new .diff-marker,
.messages[style*="background-color: rgb(0, 0, 0)"] .diff-line.diff-new .diff-marker,
.messages[style*="background-color: rgb(26, 27, 38)"] .diff-line.diff-new .diff-marker,
.messages[style*="background-color: rgb(0, 43, 54)"] .diff-line.diff-new .diff-marker {
    color: #6bcf7f;
}
```

### Step 3: Verify Side-by-Side Diff Support
Ensure the side-by-side diff styles also get proper content coloring (around line 1550):

```css
/* Side-by-side diff content coloring */
.diff-side.diff-old .diff-content {
    color: #d73a49;
}

.diff-side.diff-new .diff-content {
    color: #28a745;
}

/* Dark theme side-by-side */
.messages[style*="background-color: rgb(13, 17, 23)"] .diff-side.diff-old .diff-content,
.messages[style*="background-color: rgb(30, 30, 30)"] .diff-side.diff-old .diff-content,
.messages[style*="background-color: rgb(0, 0, 0)"] .diff-side.diff-old .diff-content,
.messages[style*="background-color: rgb(26, 27, 38)"] .diff-side.diff-old .diff-content,
.messages[style*="background-color: rgb(0, 43, 54)"] .diff-side.diff-old .diff-content {
    color: #ff8787;
}

.messages[style*="background-color: rgb(13, 17, 23)"] .diff-side.diff-new .diff-content,
.messages[style*="background-color: rgb(30, 30, 30)"] .diff-side.diff-new .diff-content,
.messages[style*="background-color: rgb(0, 0, 0)"] .diff-side.diff-new .diff-content,
.messages[style*="background-color: rgb(26, 27, 38)"] .diff-side.diff-new .diff-content,
.messages[style*="background-color: rgb(0, 43, 54)"] .diff-side.diff-new .diff-content {
    color: #6bcf7f;
}
```

## Files to Modify

- `/workspace/cmd/swe-swe/static/css/styles.css`

## Testing Plan

1. **Light Theme Testing**
   - Test unified diff view shows green text for additions, red for removals
   - Test side-by-side diff view shows proper coloring
   - Test context lines show muted coloring

2. **Dark Theme Testing**
   - Test all dark theme variants (DarkTerminal, ClassicTerminal, SoftDark, Solarized)
   - Verify contrast ratios are adequate for readability
   - Test both unified and side-by-side views

3. **Cross-Browser Testing**
   - Test color inheritance works in Chrome, Firefox, Safari
   - Verify CSS specificity doesn't cause conflicts

## Expected Results

After implementation:
- ✅ Diff content text will show proper colors (red for removals, green for additions)
- ✅ Diff markers (+/-) will have matching colors and bold weight
- ✅ Context lines will show in muted colors
- ✅ All themes will have appropriate contrast and readability
- ✅ Both unified and side-by-side diff views will work consistently

## Implementation Priority

**Priority: HIGH** - This is a fundamental UX issue affecting code review and understanding of changes.