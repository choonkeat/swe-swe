# Improve Todo Checkbox Visibility

## Problem
The current todo checkboxes using Unicode symbols `☐` (unchecked) and `☑` (checked) are too similar and hard to distinguish, especially in the chat interface's dark theme.

## Current Implementation
- **Unchecked**: `☐` (U+2610 BALLOT BOX)
- **Checked**: `☑` (U+2611 BALLOT BOX WITH CHECK)
- Location: `/workspace/elm/src/Main.elm` lines 2092-2126

## Proposed Solutions

### Option 1: More Distinct Unicode Symbols
Replace the current symbols with more visually distinct alternatives:

- **Unchecked**: `⬜` (U+2B1C WHITE LARGE SQUARE) or `□` (U+25A1 WHITE SQUARE)
- **Checked**: `✅` (U+2705 WHITE HEAVY CHECK MARK) or `✔️` (U+2714 HEAVY CHECK MARK + U+FE0F VARIATION SELECTOR)

### Option 2: Enhanced Color-Coded Symbols
Use colored emoji-style symbols:

- **Unchecked**: `⚪` (U+26AA MEDIUM WHITE CIRCLE) 
- **Checked**: `✅` (U+2705 WHITE HEAVY CHECK MARK)

### Option 3: Text-Based Indicators (Recommended)
Use clear text-based indicators with consistent width:

- **Unchecked**: `[ ]` (brackets with space)
- **Checked**: `[✓]` (brackets with check mark)

Both symbols maintain the same 3-character width for consistent alignment.

### Option 4: Status Word Prefixes
Use clear text status:

- **Unchecked**: `TODO:` 
- **Checked**: `DONE:` or `✓ DONE:`

## Implementation Details

The change needs to be made in the Elm frontend at `/workspace/elm/src/Main.elm` in the `renderTodo` function around lines 2099-2105:

```elm
statusSymbol =
    if todo.status == "completed" then
        "[✓] "  -- NEW: Bracketed check mark
    else
        "[ ] "  -- NEW: Empty brackets
```

## Benefits
- **Better visibility**: More distinct visual difference between checked/unchecked states
- **Better accessibility**: Easier to distinguish for users with visual impairments
- **Cross-platform consistency**: Emoji symbols render more consistently across different systems
- **Immediate recognition**: Users instantly understand the status without close inspection

## Testing Considerations
- Test across different themes (light/dark)
- Verify rendering on different browsers and operating systems  
- Ensure the symbols don't break the layout or text alignment
- Check that the symbols are supported across all target platforms

## Recommendation
**Option 3** is recommended as it provides excellent visibility, consistent width for alignment, universal text compatibility, and clear visual distinction between states while being accessible across all platforms and themes.