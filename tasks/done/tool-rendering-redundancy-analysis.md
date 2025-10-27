# Tool Rendering Redundancy Analysis & Solution

## Problem Analysis

You're absolutely correct! There's redundant rendering happening between `.tool-use` and `.tool-result` elements. Based on my analysis of the Elm source code:

### Current Redundant Structure

**Scenario 1: Separate tool-use and tool-result**
```
ChatToolUse -> renders as:
  <div class="tool-use">🔧 Tool: Edit</div>

ChatToolResult -> renders as:
  <details class="tool-result">
    <summary>Tool Result</summary>
    <div class="tool-result-content">...</div>
  </details>
```

**Scenario 2: Combined rendering**
```
ChatToolUseWithResult -> renders as:
  <details class="tool-result">
    <summary>[Edit] filename.txt</summary>
    <div class="tool-result-content">
      <!-- diff content, etc -->
    </div>
  </details>
```

## The Redundancy Issue

1. **Two separate elements** for the same logical operation when tool-use and tool-result appear separately
2. **Visual duplication** - both show tool information 
3. **Inconsistent UX** - sometimes expandable (tool-result), sometimes not (tool-use)
4. **Screen space waste** - especially problematic with the current tall tool-use elements

## Proposed Solution

### Eliminate Standalone .tool-use Elements

**Goal**: Always render tool calls with their results in a single, consistent `.tool-result` structure.

### Implementation Changes Required

#### 1. Elm Source Changes (`/workspace/elm/src/Main.elm`)

**Current behavior** (around lines 1992-2070):
- `ChatToolUse` renders as `<div class="tool-use">` 
- `ChatToolResult` renders as `<details class="tool-result">`

**Proposed behavior**:
- `ChatToolUse` should render as `<details class="tool-result">` with summary but no content initially
- `ChatToolResult` should find and update the corresponding `tool-result` element

#### 2. Data Structure Changes

**Current**: Tool use and result are separate chat items
**Proposed**: Link tool results to their corresponding tool use, or render as placeholder until result arrives

#### 3. CSS Simplification (`/workspace/cmd/swe-swe/static/css/styles.css`)

**Remove redundant styles**:
```css
/* Remove these redundant .tool-use styles */
.tool-use {
    margin: 5px 0;
    padding: 8px 12px;
    color: var(--text-secondary);
    font-weight: 500;
}
```

**Keep and enhance .tool-result styles** with the vertical cropping:
```css
.tool-result {
    margin: 10px 0;
    border: 1px solid var(--border-light);
    border-radius: 8px;
    padding: 0;
    overflow: hidden;
}

.tool-result summary {
    background-color: var(--bg-surface-alt);
    padding: 10px 15px;
    cursor: pointer;
    font-weight: bold;
    color: var(--text-secondary);
    user-select: none;
    transition: background-color 0.2s;
    
    /* Apply vertical cropping to summary instead of .tool-use */
    max-height: 3.5em;
    overflow: hidden;
    position: relative;
}

/* Fade effect for tool-result summary when content overflows */
.tool-result summary::after {
    content: '';
    position: absolute;
    bottom: 0;
    left: 0;
    right: 0;
    height: 1em;
    background: linear-gradient(transparent, var(--bg-surface-alt));
    pointer-events: none;
    opacity: 0;
    transition: opacity 0.2s ease;
}

.tool-result summary:not(.expanded)::after {
    opacity: 1;
}

.tool-result summary:hover {
    max-height: none;
}

.tool-result summary:hover::after {
    opacity: 0;
}
```

## Benefits of This Approach

1. **Eliminates redundancy** - One element per tool operation
2. **Consistent UX** - All tool operations are expandable/collapsible
3. **Better space utilization** - Summary shows key info, details expand on demand
4. **Cleaner code** - Fewer CSS classes and simpler rendering logic
5. **Vertical cropping applied once** - Only to the summary, not duplicated

## Implementation Strategy

### Phase 1: CSS Updates
1. Apply vertical cropping to `.tool-result summary` instead of `.tool-use`
2. Add fade effects for summary overflow
3. Remove redundant `.tool-use` styles

### Phase 2: Elm Logic Updates  
1. Modify `ChatToolUse` rendering to use `tool-result` structure
2. Update `ChatToolResult` to populate existing `tool-result` content
3. Ensure proper linking between tool invocation and result

### Phase 3: Testing
1. Verify all tool types render consistently
2. Test expand/collapse behavior
3. Confirm vertical cropping works properly
4. Check dark theme compatibility

This solution transforms the current redundant two-element system into a clean, single-element approach that's more user-friendly and space-efficient.