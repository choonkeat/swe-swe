# Feature: Enhanced Tool Output Formatting

## Overview
Transform raw tool output JSON into rich, interactive HTML components with intelligent truncation, syntax highlighting, and expandable sections. Fall back to raw JSON display for uncatered output formats.

## Key Features

### 1. File Read Formatting
- Syntax highlighting based on file extension
- Line numbers with clickable anchors
- Collapsible sections for long files
- Smart truncation with "Show more" option
- File metadata header (path, size, lines)

**Example Design:**
```html
<div class="tool-output file-read">
  <header>
    <span class="file-icon">📄</span>
    <span class="file-path">/src/components/Button.tsx</span>
    <span class="file-meta">2.3KB • 89 lines</span>
    <button class="expand-toggle">Collapse</button>
  </header>
  <pre class="file-content" data-language="typescript">
    <code class="line-numbers">
      1  import React from 'react';
      2  
      3  export function Button({ onClick, children }) {
      ...
      [Lines 4-85 hidden] <button>Show all</button>
      86    );
      87  }
    </code>
  </pre>
</div>
```

### 2. Bash Execution Formatting
- Command header with copy button
- Exit status indicator (success/error)
- Collapsible stdout/stderr sections
- ANSI color support
- Execution time display

**Example Design:**
```html
<div class="tool-output bash-execution">
  <header>
    <span class="tool-icon">🖥️</span>
    <span class="command">npm test</span>
    <button class="copy-command">Copy</button>
    <span class="exit-status success">✓ Exit 0</span>
    <span class="exec-time">2.3s</span>
  </header>
  <div class="output-sections">
    <details open>
      <summary>Output (23 lines)</summary>
      <pre class="stdout">
        Test Suites: 5 passed, 5 total
        Tests:       28 passed, 28 total
        ...
      </pre>
    </details>
  </div>
</div>
```

### 3. File Write/Edit Formatting
- Diff view for edits (before/after)
- Created/modified indicator
- File path with folder navigation
- Character/line count changes
- Syntax highlighted preview

**Example Design:**
```html
<div class="tool-output file-write">
  <header>
    <span class="tool-icon">✏️</span>
    <span class="action">Modified</span>
    <span class="file-path">/config/settings.json</span>
    <span class="changes">+12 -3 lines</span>
  </header>
  <div class="diff-view">
    <div class="diff-line removed">-  "debug": false,</div>
    <div class="diff-line added">+  "debug": true,</div>
    <div class="diff-line added">+  "logLevel": "verbose",</div>
  </div>
</div>
```

### 4. Search Results Formatting
- Grouped by file
- Match highlighting
- Context lines
- Result count summary
- Jump-to-file links

### 5. Generic Tool Output Handler
- Pretty-printed JSON with syntax highlighting
- Collapsible nested objects
- Copy raw JSON button
- Tool name and timestamp

## Smart Truncation Rules

### File Content
- Show first 20 and last 10 lines for long files
- Always show error regions if present
- Maintain syntax integrity (don't break mid-function)

### Command Output
- Show first 50 lines by default
- Always show error output completely
- Preserve ANSI escape sequences

### Diff Display
- Show 3 lines context around changes
- Collapse unchanged regions over 10 lines
- Smart grouping of nearby changes

## Interactive Features

### 1. Expand/Collapse
- Remember user preference per tool type
- Keyboard shortcuts (Space to toggle)
- Smooth animations

### 2. Copy Functions
- Copy entire output
- Copy specific sections (command, file path)
- Copy as markdown for sharing

### 3. Navigation
- Click file paths to open in editor
- Click line numbers to jump
- Breadcrumb navigation for paths

## Theming and Customization

### Color Schemes
```css
.tool-output {
  --header-bg: var(--surface-2);
  --success-color: var(--green-500);
  --error-color: var(--red-500);
  --line-number-color: var(--gray-600);
}

/* Dark mode adjustments */
@media (prefers-color-scheme: dark) {
  .tool-output {
    --header-bg: var(--surface-3);
    --line-number-color: var(--gray-400);
  }
}
```

### User Preferences
- Default expansion state
- Line wrap vs horizontal scroll
- Syntax highlighting theme
- Output density (compact/comfortable)

## Performance Considerations

### Lazy Rendering
- Virtual scrolling for long outputs
- Progressive syntax highlighting
- Debounced expand/collapse

### Memory Management
- Limit stored output history
- Compress large outputs
- Offload to IndexedDB if needed

## Implementation Strategy

### Tool Output Registry
```typescript
interface ToolFormatter {
  canFormat(tool: string, output: any): boolean;
  format(output: any): HTMLElement;
}

const formatters: ToolFormatter[] = [
  new FileReadFormatter(),
  new BashFormatter(),
  new FileWriteFormatter(),
  new SearchFormatter(),
  new GenericJSONFormatter(), // fallback
];
```

### Progressive Enhancement
1. Start with JSON display (current state)
2. Add formatters one by one
3. Maintain backward compatibility
4. Allow users to toggle raw view

## Benefits
- Improved readability of tool outputs
- Faster comprehension of results
- Less cognitive load parsing JSON
- Better mobile experience
- Professional appearance

## Estimation

### T-Shirt Size: L (Large)

### Breakdown
- **Formatter Infrastructure**: M
  - Registry pattern setup
  - Base formatter class
  - Output detection logic
  
- **Individual Formatters**: M
  - File read/write formatters
  - Bash output formatter
  - Search result formatter
  
- **UI Components**: M
  - Collapsible sections
  - Syntax highlighting
  - Copy functionality
  - Diff viewer
  
- **Performance Optimization**: S
  - Virtual scrolling
  - Lazy loading
  - Memory management

### Impact Analysis
- **User Experience**: Very high positive impact
- **Codebase Changes**: Moderate - new formatting layer
- **Architecture**: Low impact - presentation layer only
- **Performance Risk**: Medium - need optimization for large outputs

### Agent-Era Estimation Notes
This is "Large" because:
- Many different output types to handle
- Rich interaction patterns to implement
- Performance optimization for large outputs
- Cross-browser compatibility for formatting
- Accessibility requirements for interactive elements