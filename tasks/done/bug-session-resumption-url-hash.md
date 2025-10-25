# Session Resumption Bug: Claude session ID in URL hash doesn't properly resume session

## Problem Analysis

When visiting a URL with a Claude session ID in the hash fragment (e.g., `http://localhost:8080/#claude=session_12345`), the session is not properly resumed. Instead, a new Claude session is started.

## Root Cause

The bug occurs due to incorrect initialization of the `isFirstUserMessage` flag in the Elm application:

1. **URL Parsing Works**: The JavaScript code correctly parses the Claude session ID from the URL hash fragment and passes it to Elm via flags (index.html.tmpl lines 31-47).

2. **Elm Initialization Issue**: In `elm/src/Main.elm`, the `init` function always sets `isFirstUserMessage = True` (line 247), regardless of whether a `claudeSessionID` was provided in the flags.

3. **Message Sending**: When the user sends their first message, the Elm app includes `"firstMessage": true` in the JSON payload (lines 364, 579).

4. **Server-Side Logic**: The Go backend checks the `FirstMessage` flag to determine whether to:
   - Use the first message command (`agentCLI1st`) if `isFirstMessage == true` (websocket.go lines 290-293)
   - Add `--resume` flag with the session ID if `!isFirstMessage && claudeSessionID != ""` (websocket.go lines 322-325)

5. **Result**: Since `isFirstMessage` is always `true` for the first message after loading the page (even with a session ID in the URL), the server never adds the `--resume` flag, causing a new session to be created instead of resuming the existing one.

## Fix

The fix requires modifying the Elm initialization logic to properly set `isFirstUserMessage` based on whether a Claude session ID is present:

### In `elm/src/Main.elm`:

Change the `init` function (around lines 222-253) to set `isFirstUserMessage` based on the presence of `claudeSessionID`:

```elm
init : Flags -> ( Model, Cmd Msg )
init flags =
    let
        initialTheme =
            if flags.systemTheme == "dark" then
                DarkTerminal
            else
                LightModern

        initialFuzzyMatcher =
            { isOpen = False
            , query = ""
            , results = []
            , selectedIndex = 0
            , cursorPosition = 0
            }
        
        -- If we have a Claude session ID from the URL, this is NOT the first message
        -- because we're resuming an existing session
        isFirstUserMessage =
            case flags.claudeSessionID of
                Just _ ->
                    False
                Nothing ->
                    True
    in
    ( { input = ""
      , messages = []
      , currentSender = Nothing
      , theme = stringToTheme flags.savedUserTheme
      , isConnected = False
      , systemTheme = initialTheme
      , isTyping = False
      , isFirstUserMessage = isFirstUserMessage  -- Use the calculated value
      , browserSessionID = Just flags.browserSessionID
      , claudeSessionID = flags.claudeSessionID
      , pendingToolUses = Dict.empty
      , allowedTools = []
      , skipPermissions = False
      , permissionDialog = Nothing
      , pendingPermissionRequest = Nothing
      , fuzzyMatcher = initialFuzzyMatcher
      }
    , Cmd.none
    )
```

## Testing

After implementing the fix:

1. Start a new conversation and get a Claude session ID in the URL
2. Copy the URL with the session ID
3. Open a new tab/window and paste the URL
4. Send a message - it should resume the existing session, not start a new one
5. Verify by checking if the conversation history is maintained

## Alternative Considerations

Another approach would be to modify the server-side logic to check for the presence of `claudeSessionID` independently of the `FirstMessage` flag. However, the Elm-side fix is cleaner because:
- It correctly represents the semantic meaning of `isFirstUserMessage`
- It maintains consistency between client and server state
- It doesn't require complex server-side logic changes