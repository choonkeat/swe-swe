# Integrate Edit and MultiEdit Tools with Proper Diff Viewer

## Problem Description

From the provided screenshots:

### `edit-diff.png` shows:
- Edit tool displays basic "Replace:" and "With:" text blocks
- Content is truncated with "..." 
- No visual diff highlighting or structure
- Uses simple background colors instead of proper diff visualization

### `multi-edit-diff.png` shows:
- MultiEdit tool shows only basic metadata ("File: path", "Number of edits: X")
- No preview of actual changes
- No structured diff visualization 
- Users can't see what changes will be made before approving

## Root Cause Analysis

After analyzing the Elm code in `/workspace/elm/src/Main.elm`:

### Current Edit Tool Permission Display (lines 1798-1843)
```elm
"Edit" ->
    -- Shows truncated "Replace:" and "With:" blocks
    -- Limited to 200 characters with "..." truncation
    -- No diff visualization
```

### Current MultiEdit Tool Permission Display (lines 1865-1881)  
```elm
"MultiEdit" ->
    -- Only shows file path and edit count
    -- No actual diff preview
    -- Raw JSON fallback when parsing fails
```

### Available Diff Rendering Functions (lines 1680-1781)
The codebase already has excellent diff rendering capabilities:
- `renderDiff` - Creates unified diff view
- `renderSideBySideDiff` - Creates side-by-side diff view  
- `renderEditAsDiff` - Renders individual edit as diff
- `generateUnifiedDiff` - Computes line-based diffs

### Chat Tool Rendering Works Correctly (lines 1980-2146)
Edit and MultiEdit tools DO show proper diffs in chat messages, but NOT in permission dialogs.

## Solution Implementation

### Step 1: Update Edit Tool Permission Display
Replace the current Edit tool formatting in `/workspace/elm/src/Main.elm` (lines 1798-1843):

```elm
"Edit" ->
    case
        Decode.decodeString
            (Decode.map3 (\fp old new -> { filePath = fp, oldString = old, newString = new })
                (Decode.field "file_path" Decode.string)
                (Decode.field "old_string" Decode.string)
                (Decode.field "new_string" Decode.string)
            )
            inputJson
    of
        Ok edit ->
            div []
                [ p [ style "margin" "0.25rem 0", style "font-weight" "bold" ] 
                    [ text ("📝 Edit: " ++ edit.filePath) ]
                , div [ style "margin-top" "0.5rem" ]
                    [ createPermissionPreviewDiff edit.oldString edit.newString ]
                ]

        Err _ ->
            pre [ style "font-size" "0.9em", style "overflow" "auto" ] [ text inputJson ]
```

### Step 2: Update MultiEdit Tool Permission Display  
Replace the current MultiEdit tool formatting in `/workspace/elm/src/Main.elm` (lines 1865-1881):

```elm
"MultiEdit" ->
    case
        Decode.decodeString
            (Decode.map2 (\fp edits -> { filePath = fp, edits = edits })
                (Decode.field "file_path" Decode.string)
                (Decode.field "edits" (Decode.list Decode.value))
            )
            inputJson
    of
        Ok multiEdit ->
            div []
                [ p [ style "margin" "0.25rem 0", style "font-weight" "bold" ] 
                    [ text ("📝 MultiEdit: " ++ multiEdit.filePath ++ " (" ++ String.fromInt (List.length multiEdit.edits) ++ " edits)") ]
                , details [ style "margin-top" "0.5rem" ]
                    [ summary [ style "cursor" "pointer", style "font-weight" "500" ] 
                        [ text "📋 Preview all changes" ]
                    , div [ class "permission-multi-edit-preview", style "margin-top" "0.5rem" ]
                        (List.indexedMap
                            (\idx editJson ->
                                div [ class "permission-edit-item" ]
                                    [ div [ style "font-size" "0.9em", style "color" "var(--text-secondary)", style "margin-bottom" "0.25rem" ]
                                        [ text ("Edit " ++ String.fromInt (idx + 1) ++ " of " ++ String.fromInt (List.length multiEdit.edits)) ]
                                    , renderEditAsDiffPreview editJson
                                    ]
                            )
                            multiEdit.edits
                        )
                    ]
                ]

        Err _ ->
            pre [ style "font-size" "0.9em", style "overflow" "auto" ] [ text inputJson ]
```

### Step 3: Create Permission-Specific Diff Preview Functions
Add these helper functions in `/workspace/elm/src/Main.elm`:

```elm
-- Create a truncated diff suitable for permission dialogs
createPermissionPreviewDiff : String -> String -> Html Msg
createPermissionPreviewDiff oldString newString =
    let
        oldLines = String.lines oldString
        newLines = String.lines newString
        
        -- Limit preview size for permission dialogs
        maxPreviewLines = 15
        totalLines = List.length oldLines + List.length newLines
        
        shouldTruncate = totalLines > maxPreviewLines
        
        (previewOld, previewNew) = 
            if shouldTruncate then
                ( oldLines |> List.take 8 |> String.join "\n"
                , newLines |> List.take 8 |> String.join "\n"
                )
            else
                (oldString, newString)
    in
    div [ class "permission-diff-preview" ]
        [ renderDiff previewOld previewNew
        , if shouldTruncate then
            div [ class "diff-truncation-notice" ]
                [ text ("... (showing preview, " ++ 
                    String.fromInt (List.length oldLines) ++ " → " ++ 
                    String.fromInt (List.length newLines) ++ " total lines)")
                ]
          else
            text ""
        ]

-- Render individual edit for MultiEdit preview
renderEditAsDiffPreview : Decode.Value -> Html Msg
renderEditAsDiffPreview editJson =
    case
        Decode.decodeValue
            (Decode.map3 (\old new replaceAll -> { oldString = old, newString = new, replaceAll = replaceAll })
                (Decode.field "old_string" Decode.string)
                (Decode.field "new_string" Decode.string)
                (Decode.oneOf [ Decode.field "replace_all" Decode.bool, Decode.succeed False ])
            )
            editJson
    of
        Ok edit ->
            createPermissionPreviewDiff edit.oldString edit.newString

        Err _ ->
            div [ style "background-color" "var(--bg-surface-alt)", style "padding" "0.5rem", style "border-radius" "4px" ]
                [ text "Unable to preview this edit"
                , details [ style "margin-top" "0.25rem" ]
                    [ summary [ style "font-size" "0.8em", style "cursor" "pointer" ] [ text "Show raw data" ]
                    , pre [ style "font-size" "0.8em", style "margin-top" "0.25rem" ] [ text (Encode.encode 2 editJson) ]
                    ]
                ]
```

### Step 4: Add Permission Dialog Diff Styling
Add these styles to `/workspace/cmd/swe-swe/static/css/styles.css`:

```css
/* Permission dialog diff preview styling */
.permission-diff-preview {
    max-height: 400px;
    overflow-y: auto;
    border: 1px solid var(--border-light);
    border-radius: 4px;
    margin: 0.5rem 0;
}

.permission-diff-preview .diff-container {
    margin: 0;
    border: none;
    border-radius: 0;
}

.permission-multi-edit-preview {
    max-height: 500px;
    overflow-y: auto;
}

.permission-edit-item {
    margin-bottom: 1rem;
    padding-bottom: 0.75rem;
    border-bottom: 1px solid var(--border-light);
}

.permission-edit-item:last-child {
    border-bottom: none;
    margin-bottom: 0;
}

.diff-truncation-notice {
    padding: 0.5rem;
    background-color: var(--bg-surface-alt);
    border-top: 1px solid var(--border-light);
    text-align: center;
    font-style: italic;
    color: var(--text-secondary);
    font-size: 0.9em;
}

/* Compact permission diff styling */
.permission-diff-preview .diff-line {
    padding: 1px 8px;
    line-height: 1.2;
    font-size: 0.85em;
}

.permission-diff-preview .diff-header {
    padding: 4px 8px;
    font-size: 0.85em;
}

/* Dark theme support for permission diffs */
.messages[style*="background-color: rgb(13, 17, 23)"] .permission-diff-preview,
.messages[style*="background-color: rgb(30, 30, 30)"] .permission-diff-preview,
.messages[style*="background-color: rgb(0, 0, 0)"] .permission-diff-preview,
.messages[style*="background-color: rgb(26, 27, 38)"] .permission-diff-preview,
.messages[style*="background-color: rgb(0, 43, 54)"] .permission-diff-preview {
    border-color: var(--color-gray-700);
}

.messages[style*="background-color: rgb(13, 17, 23)"] .diff-truncation-notice,
.messages[style*="background-color: rgb(30, 30, 30)"] .diff-truncation-notice,
.messages[style*="background-color: rgb(0, 0, 0)"] .diff-truncation-notice,
.messages[style*="background-color: rgb(26, 27, 38)"] .diff-truncation-notice,
.messages[style*="background-color: rgb(0, 43, 54)"] .diff-truncation-notice {
    background-color: var(--color-gray-800);
    border-color: var(--color-gray-700);
    color: var(--color-gray-400);
}
```

## Files to Modify

1. `/workspace/elm/src/Main.elm` - Update `formatToolInput` function for Edit and MultiEdit tools
2. `/workspace/cmd/swe-swe/static/css/styles.css` - Add permission dialog diff styling

## Testing Plan

1. **Edit Tool Permission Testing**
   - Test Edit tool shows structured diff instead of "Replace/With" blocks
   - Test large file edits show truncation notice correctly
   - Test small edits show full diff without truncation

2. **MultiEdit Tool Permission Testing**  
   - Test MultiEdit shows expandable preview of all edits
   - Test each individual edit renders as a proper diff
   - Test large MultiEdit operations don't overwhelm the dialog

3. **Cross-Theme Compatibility**
   - Test permission diffs render correctly in all themes
   - Test diff coloring works with permission dialog styling
   - Test expandable details work in all themes

4. **Integration Testing**
   - Verify permission diffs use the same styling as chat diffs
   - Test that approving permissions still works correctly
   - Test error handling for malformed tool inputs

## Expected Results

After implementation:

### Edit Tool Permissions
- ✅ Show structured diff visualization instead of text blocks
- ✅ Proper syntax highlighting and diff coloring 
- ✅ Intelligent truncation for large files with preview notice
- ✅ Consistent visual experience with chat diffs

### MultiEdit Tool Permissions  
- ✅ Show file path and edit count summary
- ✅ Expandable preview showing all individual edits as diffs
- ✅ Each edit rendered with proper diff visualization
- ✅ Compact layout suitable for permission dialog constraints

### Overall UX Improvements
- ✅ Users can see exactly what changes will be made before approving
- ✅ Consistent diff rendering between permission dialogs and chat
- ✅ Better informed consent for file modifications
- ✅ Enhanced trust and transparency in tool operations

## Implementation Priority

**Priority: HIGH** - This significantly improves user experience and trust when reviewing tool permissions for file modifications.