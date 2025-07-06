# Tool Input Format Changes

## Summary

Changed the tool rendering format from:
```
[{tool name}] {description} <code>{command}</code>
```

To:
```
[{tool name}] <code>{tool input json}</code>
```

## Special Handling for MultiEdit

For the `MultiEdit` tool, if the `edits` array is detected, it's replaced with a summary to avoid extremely long output:

Original:
```json
{
  "file_path": "/path/to/file.elm",
  "edits": [
    {"old_string": "...", "new_string": "..."},
    {"old_string": "...", "new_string": "..."},
    // ... potentially hundreds more
  ]
}
```

Displayed as:
```json
{"file_path":"/path/to/file.elm","edits":"[2 edits]"}
```

## Implementation Details

1. **ChatToolUse** rendering (lines 1109-1161):
   - Removed extraction of `description` and `command` fields
   - Added `inputDisplay` that shows the full JSON
   - Special case for MultiEdit to truncate the edits array

2. **ChatToolUseWithResult** rendering (lines 1180-1234):
   - Same changes as ChatToolUse
   - Summary now only shows tool name, JSON is in code block

3. **parseClaudeContent** (lines 799-812):
   - For regular tools, now shows the full JSON with 2-space indentation
   - TodoWrite keeps its special rendering

## Examples

### Before:
```
[Bash] Lists files in current directory <code>ls -la</code>
[Read] <code>/path/to/file.txt</code>
[MultiEdit] Edit multiple sections <code>/path/to/file.elm</code>
```

### After:
```
[Bash] <code>{"command":"ls -la","description":"Lists files in current directory"}</code>
[Read] <code>{"file_path":"/path/to/file.txt"}</code>
[MultiEdit] <code>{"file_path":"/path/to/file.elm","edits":"[3 edits]"}</code>
```