# Fix Garish Autocomplete Menu Colors

## Problem
The autocomplete/file search dropdown menu uses bright orange highlighting (#FFA500 or similar) that appears too harsh and garish against the dark theme background. This creates poor visual contrast and user experience.

## Current State
- Autocomplete menu highlights matching text with bright orange color
- The orange is very saturated and stands out too aggressively
- Creates visual fatigue during extended usage

## Proposed Solution
Replace the current bright orange highlighting with more subtle, theme-appropriate colors:

### Option 1: Muted Orange
- Use a desaturated orange: `#CC8844` or `#B8860B` (dark goldenrod)
- Maintains the orange theme but reduces intensity

### Option 2: Theme-Consistent Blue
- Use a blue that matches common editor themes: `#569CD6` (VS Code blue)
- Better integration with typical development tool color schemes

### Option 3: Gray-Based Highlighting
- Use a light gray with subtle accent: `#4A4A4A` background with `#E0E0E0` text
- Most conservative approach, reduces visual noise

## Implementation
The color changes likely need to be made in:
- `cmd/swe-swe/static/css/styles.css` - for web interface styling
- Template files that generate the autocomplete HTML

## Priority
Small (S) - UI polish improvement that enhances user experience without affecting functionality.