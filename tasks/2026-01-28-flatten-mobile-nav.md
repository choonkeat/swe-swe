# Flatten Mobile Navigation to Single Dropdown

**Date**: 2026-01-28
**Status**: Completed

**Commits**:
- `d4bfaf935` refactor(ui): flatten mobile navigation to single dropdown
- `45a63a479` refactor(ui): integrate YOLO toggle into assistant badge

## Problem

Current mobile navigation has two levels:
1. Top tabs: `Terminal | Workspace`
2. Secondary dropdown (when Workspace selected): `Preview, Code, Terminal, Agent View`

This requires extra taps and creates a confusing mental model.

## Solution

Replace with single dropdown containing all 5 options:
- Agent Terminal
- App Preview
- Code
- Terminal
- Agent View

## Files to Modify

- `cmd/swe-swe/templates/host/swe-swe-server/static/terminal-ui.js`
  - Lines 310-324: Replace view tabs with single dropdown
  - Lines 352-359: Remove separate panel dropdown
  - Lines 1437-1444: Update event handlers
  - Lines 2820-2852: Add new `switchMobileNav()` method

- `cmd/swe-swe/templates/host/swe-swe-server/static/styles/terminal-ui.css`
  - Lines 1598-1626: Remove/update view-tabs styles
  - Lines 1691-1792: Update mobile dropdown styles

## Implementation

### New HTML (in terminal bar)
```html
<div class="terminal-ui__terminal-bar mobile-only">
    <select class="terminal-ui__mobile-nav-select">
        <option value="agent-terminal">Agent Terminal</option>
        <option value="preview">App Preview</option>
        <option value="vscode">Code</option>
        <option value="shell">Terminal</option>
        <option value="browser">Agent View</option>
    </select>
    <span class="terminal-ui__assistant-badge">CLAUDE</span>
</div>
```

### New JS Method
```javascript
switchMobileNav(value) {
    const terminalUi = this.querySelector('.terminal-ui');

    if (value === 'agent-terminal') {
        terminalUi.classList.remove('mobile-view-workspace');
        terminalUi.classList.add('mobile-view-terminal');
        this.mobileActiveView = 'terminal';
        setTimeout(() => this.fitAndPreserveScroll(), 50);
    } else {
        terminalUi.classList.remove('mobile-view-terminal');
        terminalUi.classList.add('mobile-view-workspace');
        this.mobileActiveView = 'workspace';
        this.switchPanelTab(value);
    }
}
```

## Verification

1. `make build`
2. Boot test container
3. Test on mobile viewport (< 640px)
4. Verify all 5 options work
5. Verify desktop still works
