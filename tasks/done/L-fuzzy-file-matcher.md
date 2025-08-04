# Feature: Fuzzy File Matcher with @ Trigger

## Overview
Add the ability to use the `@` character during prompt input to trigger a fuzzy filename matcher, similar to VS Code's quick file picker. This will enable users to quickly reference files and directories without typing exact paths.

## Key Features

### 1. Trigger Mechanism
- Typing `@` in the prompt should immediately open the fuzzy matcher
- Should work inline while composing prompts (not just at the beginning)
- Example: "Please update the @ to include..." → triggers matcher mid-sentence

### 2. Fuzzy Matching Algorithm
- Support partial string matching across path components
- Example: `statstylcss` matches `static/style.css`
- Example: `compfoobar` matches `components/foo/bar.js`
- Case-insensitive matching
- Match any substring sequence, not just prefix matching

### 3. Search Scope
- Include both files and directories in results
- Start search from current working directory
- Support relative and absolute path display options

### 4. Filtering and Exclusions
- Respect `.gitignore` patterns
- Exclude common non-source directories:
  - `node_modules/`
  - `.git/`
  - `dist/`, `build/`, `out/`
  - Binary files and large assets
- Allow custom exclusion patterns (possibly from config)

### 5. Result Ordering
- Prioritize matches based on:
  1. Exact filename matches
  2. Start-of-filename matches
  3. Path segment boundary matches
  4. Recently accessed/modified files
  5. Shorter path depth
  6. Alphabetical as final tiebreaker

### 6. UI/UX Considerations
- Display results in a scrollable list
- Show file paths with directory context
- Highlight matched characters in results
- Preview panel for selected file (optional)
- Keyboard navigation:
  - Arrow keys or Ctrl+N/P for navigation
  - Enter to select
  - Escape to cancel
  - Continue typing to refine search

### 7. Integration Points
- Selected path should be inserted at cursor position
- Maintain prompt context before and after insertion
- Support multiple file selections in one prompt (using multiple @ triggers)

## Technical Implementation Notes

### Search Performance
- Build and maintain file index for large codebases
- Incremental updates when files change
- Lazy loading for very large result sets

### Configuration Options
```yaml
fuzzyMatcher:
  enabled: true
  triggerChar: "@"
  maxResults: 50
  excludePatterns:
    - "*.log"
    - "*.tmp"
  includeHidden: false
  searchDepth: 10
```

## Example Usage Scenarios

1. **Quick file reference**: 
   "Update the @sty (selects static/css/styles.css) to use the new color scheme"

2. **Multiple file selection**:
   "Compare @useauth (selects hooks/useAuth.js) with @authcont (selects contexts/AuthContext.js)"

3. **Directory selection**:
   "Move all files from @comp/old (selects components/old/) to @arch (selects archived/)"

## Benefits
- Reduces typing effort and errors
- Speeds up file referencing in prompts
- Makes the tool more accessible to users unfamiliar with exact project structure
- Improves workflow efficiency for frequent file operations

## Future Enhancements
- Symbol search within files (functions, classes)
- Content preview in matcher
- Smart suggestions based on prompt context
- Integration with project-wide search

## Estimation

### T-Shirt Size: L (Large)

### Breakdown
- **UI Component Development**: M
  - Fuzzy matcher interface
  - Keyboard navigation
  - Result highlighting
  
- **Search Algorithm**: S
  - Basic fuzzy matching logic
  - Path component matching
  
- **Integration Complexity**: L
  - Inline trigger detection
  - Cursor position management
  - File system traversal with exclusions
  - Index building and caching
  
- **Testing & Edge Cases**: M
  - Cross-platform path handling
  - Large codebase performance
  - Special character handling

### Impact Analysis
- **User Experience**: High positive impact
- **Codebase Changes**: Moderate - new subsystem with minimal changes to existing code
- **Architecture**: Low impact - can be implemented as a plugin/module
- **Performance Risk**: Medium - requires optimization for large codebases

### Agent-Era Estimation Notes
In the age of coding agents, T-shirt sizing shifts from "effort hours" to:
- **Complexity of Requirements**: How well-defined and edge-case-free is the feature?
- **Integration Surface**: How many existing systems need to be touched?
- **Testing Complexity**: How many scenarios need validation?
- **Human Review Needs**: How much human oversight for UX/architecture decisions?

This feature remains "Large" because:
- High integration complexity (real-time UI interaction)
- Significant UX decisions requiring human input
- Performance optimization needs human judgment
- Cross-platform compatibility testing