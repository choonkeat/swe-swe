port module Main exposing
    ( ChatItem(..)
    , ClaudeContent
    , ClaudeMessage
    , ClaudeMessageContent
    , Model
    , ParseResult
    , Theme(..)
    , Todo
    , claudeMessageDecoder
    , main
    , parseClaudeMessage
    )

import Ansi exposing (ansiToElmHtml, ansiToHtml)
import Browser
import Dict exposing (Dict)
import Html exposing (Html, button, details, div, h1, h3, input, label, option, p, pre, select, span, summary, text, textarea)
import Html.Attributes exposing (autofocus, checked, class, disabled, placeholder, selected, style, type_, value)
import Html.Events exposing (keyCode, on, onClick, onInput, targetValue)
import Json.Decode as Decode
import Json.Encode as Encode
import Set



-- PORTS


port sendMessage : String -> Cmd msg


port messageReceiver : (String -> msg) -> Sub msg


port scrollToBottom : () -> Cmd msg


port connectionStatusReceiver : (Bool -> msg) -> Sub msg


port systemThemeChanged : (String -> msg) -> Sub msg


port persistUserTheme : String -> Cmd msg


port focusMessageInput : () -> Cmd msg


port sendFuzzySearch : String -> Cmd msg


port updateURLFragment : String -> Cmd msg



-- MAIN


type alias Flags =
    { systemTheme : String
    , savedUserTheme : String
    , browserSessionID : String
    , claudeSessionID : Maybe String
    }


main : Program Flags Model Msg
main =
    Browser.element
        { init = init
        , update = update
        , subscriptions = subscriptions
        , view = view
        }



-- MODEL


type Theme
    = DarkTerminal
    | ClassicTerminal
    | SoftDark
    | LightModern
    | Solarized
    | System


type alias Model =
    { input : String
    , messages : List ChatItem
    , currentSender : Maybe String
    , theme : Theme
    , isConnected : Bool
    , systemTheme : Theme
    , isTyping : Bool
    , isFirstUserMessage : Bool
    , browserSessionID : Maybe String
    , claudeSessionID : Maybe String
    , pendingToolUses : Dict String ClaudeContent
    , allowedTools : List String
    , skipPermissions : Bool
    , permissionDialog : Maybe PermissionDialogState
    , pendingPermissionRequest : Maybe PermissionRequest
    , fuzzyMatcher : FuzzyMatcherState
    }


type alias PermissionDialogState =
    { toolName : String
    , errorMessage : String
    , toolInput : Maybe String
    }


type alias PermissionRequest =
    { toolName : String
    , errorMessage : String
    , toolInput : Maybe String
    }


type alias FuzzyMatcherState =
    { isOpen : Bool
    , query : String
    , results : List FileMatch
    , selectedIndex : Int
    , cursorPosition : Int -- Position in input where @ was typed
    }


type alias FileMatch =
    { file : FileInfo
    , score : Int
    , matches : List Int
    }


type alias FileInfo =
    { path : String
    , name : String
    , isDir : Bool
    , relPath : String
    }



-- A chat item can either be a sender or a message


type ChatItem
    = ChatUser String -- Represents the user sender
    | ChatBot String -- Represents the bot sender
    | ChatContent String -- Represents the content of a message
    | ChatClaudeJSON String -- Raw Claude JSON to be parsed
    | ChatToolResult String -- Tool result content to be rendered with details/summary
    | ChatTodoWrite (List Todo) -- TodoWrite tool output
    | ChatExecStart -- Exec command started
    | ChatExecEnd -- Exec command ended
    | ChatToolUse ClaudeContent -- Tool use with id
    | ChatToolUseWithResult ClaudeContent String -- Tool use with its result
    | ChatPermissionRequest String String (Maybe String) -- Permission request (tool name, error message, tool input)
    | ChatPermissionResponse String String String -- Permission response (tool name, action, username)
    | ChatFuzzySearchResults String -- Fuzzy search results (JSON)



-- Todo type for TodoWrite tool


type alias Todo =
    { id : String
    , content : String
    , status : String
    , priority : String
    }



-- Parse result for Claude messages


type alias ParseResult =
    { messages : List ChatItem
    , toolUses : List ( String, ClaudeContent )
    }



-- Claude JSON message types


type alias ClaudeMessage =
    { type_ : String
    , subtype : Maybe String
    , durationMs : Maybe Int
    , result : Maybe String
    , message : Maybe ClaudeMessageContent
    }


type alias ClaudeMessageContent =
    { role : Maybe String
    , content : List ClaudeContent
    }


type alias ClaudeContent =
    { type_ : String
    , text : Maybe String
    , name : Maybe String
    , input : Maybe Decode.Value
    , content : Maybe String -- For tool_result
    , id : Maybe String -- For tool_use
    , toolUseId : Maybe String -- For tool_result
    }


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
    in
    ( { input = ""
      , messages = []
      , currentSender = Nothing
      , theme = stringToTheme flags.savedUserTheme
      , isConnected = False
      , systemTheme = initialTheme
      , isTyping = False
      , isFirstUserMessage = True
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



-- UPDATE


type Msg
    = Input String
    | Send
    | Receive String
    | ThemeChanged String
    | KeyDown Int Bool Bool
    | ConnectionStatus Bool
    | SystemThemeChanged String
    | StopExecution
    | AllowPermission
    | AllowPermissionPermanent
    | DenyPermission
    | SkipAllPermissions
    | FuzzySearchInput String Int -- query and cursor position
    | FuzzySearchResults String -- JSON results from backend
    | FuzzyMatcherNavigate Int -- -1 for up, 1 for down
    | FuzzyMatcherSelect
    | FuzzyMatcherClose


update : Msg -> Model -> ( Model, Cmd Msg )
update msg model =
    case msg of
        Input text ->
            -- Check for @ trigger
            let
                lastChar = String.right 1 text
                beforeAt = String.dropRight 1 text
                
                ( newFuzzyMatcher, cmd ) =
                    if lastChar == "@" && not model.fuzzyMatcher.isOpen then
                        -- Open fuzzy matcher
                        ( { isOpen = True
                          , query = ""
                          , results = []
                          , selectedIndex = 0
                          , cursorPosition = String.length text
                          }
                        , Cmd.none
                        )
                    else if model.fuzzyMatcher.isOpen then
                        if String.length text < model.fuzzyMatcher.cursorPosition then
                            -- Text was deleted before @ position, close matcher
                            ( { isOpen = False
                              , query = ""
                              , results = []
                              , selectedIndex = 0
                              , cursorPosition = 0
                              }
                            , Cmd.none
                            )
                        else
                            let
                                query = String.dropLeft model.fuzzyMatcher.cursorPosition text
                            in
                            if String.contains " " query || String.contains "\n" query then
                                -- Space or newline closes the matcher
                                ( { isOpen = False
                                  , query = ""
                                  , results = []
                                  , selectedIndex = 0
                                  , cursorPosition = 0
                                  }
                                , Cmd.none
                                )
                            else
                                -- Update query and search
                                ( { isOpen = True
                                  , query = query
                                  , results = model.fuzzyMatcher.results
                                  , selectedIndex = 0
                                  , cursorPosition = model.fuzzyMatcher.cursorPosition
                                  }
                                , if String.length query > 0 then
                                    sendFuzzySearch (Encode.encode 0 (Encode.object
                                        [ ("type", Encode.string "fuzzy_search")
                                        , ("query", Encode.string query)
                                        , ("maxResults", Encode.int 50)
                                        ]))
                                  else
                                    Cmd.none
                                )
                    else
                        ( model.fuzzyMatcher, Cmd.none )
            in
            ( { model | input = text, fuzzyMatcher = newFuzzyMatcher }, cmd )

        Send ->
            if String.trim model.input == "" then
                ( model, Cmd.none )

            else
                let
                    -- Send single message with sender and content
                    messageJson =
                        Encode.encode 0
                            (Encode.object
                                [ ( "sender", Encode.string "USER" )
                                , ( "content", Encode.string model.input )
                                , ( "firstMessage", Encode.bool model.isFirstUserMessage )
                                , ( "sessionID", Encode.string (Maybe.withDefault "" model.browserSessionID) )
                                , ( "claudeSessionID", Encode.string (Maybe.withDefault "" model.claudeSessionID) )
                                ]
                            )
                in
                ( { model | input = "", isFirstUserMessage = False }
                , Cmd.batch [ sendMessage messageJson, scrollToBottom () ]
                )

        Receive json ->
            -- First check if this is a Claude session ID message
            case Decode.decodeString (Decode.field "type" Decode.string) json of
                Ok "claude_session_id" ->
                    case Decode.decodeString (Decode.field "content" Decode.string) json of
                        Ok claudeSessionID ->
                            ( { model | claudeSessionID = Just claudeSessionID }
                            , updateURLFragment claudeSessionID
                            )
                        Err _ ->
                            ( model, Cmd.none )
                _ ->
                    case Decode.decodeString chatItemDecoder json of
                        Ok chatItem ->
                            case chatItem of
                                ChatUser sender ->
                                    ( { model
                                        | messages = model.messages ++ [ chatItem ]
                                        , currentSender = Just sender
                                        , pendingPermissionRequest = Nothing
                                      }
                                    , scrollToBottom ()
                                    )

                                ChatBot sender ->
                                    ( { model
                                        | messages = model.messages ++ [ chatItem ]
                                        , currentSender = Just sender
                                      }
                                    , scrollToBottom ()
                                    )

                                ChatContent content ->
                                    ( { model
                                        | messages = model.messages ++ [ ChatContent content ]
                                      }
                                    , scrollToBottom ()
                                    )

                                ChatClaudeJSON jsonStr ->
                                    -- Parse the Claude JSON and convert to appropriate ChatItems
                                    case Decode.decodeString claudeMessageDecoder jsonStr of
                                        Ok claudeMsg ->
                                            let
                                                parseResult =
                                                    parseClaudeMessage model claudeMsg

                                                newPendingToolUses =
                                                    List.foldl
                                                        (\( id, content ) dict -> Dict.insert id content dict)
                                                        model.pendingToolUses
                                                        parseResult.toolUses
                                            in
                                            ( { model
                                                | messages = model.messages ++ parseResult.messages
                                                , pendingToolUses = newPendingToolUses
                                              }
                                            , scrollToBottom ()
                                            )

                                        Err _ ->
                                            -- If parsing fails, display raw JSON as content
                                            ( { model
                                                | messages = model.messages ++ [ ChatContent jsonStr ]
                                              }
                                            , scrollToBottom ()
                                            )

                                ChatToolResult _ ->
                                    ( { model
                                        | messages = model.messages ++ [ chatItem ]
                                      }
                                    , scrollToBottom ()
                                    )

                                ChatTodoWrite _ ->
                                    ( { model
                                        | messages = model.messages ++ [ chatItem ]
                                      }
                                    , scrollToBottom ()
                                    )

                                ChatExecStart ->
                                    ( { model
                                        | isTyping = True -- Show typing indicator when exec starts
                                      }
                                    , Cmd.none
                                    )

                                ChatExecEnd ->
                                    let
                                        focusCmd =
                                            if model.isTyping then
                                                focusMessageInput ()

                                            else
                                                Cmd.none
                                    in
                                    ( { model
                                        | isTyping = False -- Hide typing indicator when exec ends
                                      }
                                    , focusCmd
                                    )

                                ChatToolUse _ ->
                                    ( { model
                                        | messages = model.messages ++ [ chatItem ]
                                      }
                                    , scrollToBottom ()
                                    )

                                ChatToolUseWithResult _ _ ->
                                    ( { model
                                        | messages = model.messages ++ [ chatItem ]
                                      }
                                    , scrollToBottom ()
                                    )

                                ChatPermissionRequest toolName errorMessage toolInput ->
                                    ( { model
                                        | pendingPermissionRequest = Just { toolName = toolName, errorMessage = errorMessage, toolInput = toolInput }
                                        , messages = model.messages ++ [ chatItem ]
                                      }
                                    , Cmd.batch [ scrollToBottom (), focusMessageInput () ]
                                    )

                                ChatPermissionResponse _ _ _ ->
                                    ( { model
                                        | messages = model.messages ++ [ chatItem ]
                                        , pendingPermissionRequest = Nothing
                                      }
                                    , scrollToBottom ()
                                    )

                                ChatFuzzySearchResults jsonResults ->
                                    -- Update fuzzy matcher with search results
                                    update (FuzzySearchResults jsonResults) model

                        Err _ ->
                            ( model, Cmd.none )

        ThemeChanged themeString ->
            let
                newTheme =
                    stringToTheme themeString
            in
            ( { model | theme = newTheme }, persistUserTheme (themeToString newTheme) )

        KeyDown key shiftKey metaKey ->
            if model.fuzzyMatcher.isOpen then
                -- Handle fuzzy matcher navigation
                case key of
                    38 -> -- Up arrow
                        update (FuzzyMatcherNavigate -1) model
                    
                    40 -> -- Down arrow  
                        update (FuzzyMatcherNavigate 1) model
                    
                    13 -> -- Enter
                        update FuzzyMatcherSelect model
                    
                    27 -> -- Escape
                        update FuzzyMatcherClose model
                    
                    _ ->
                        ( model, Cmd.none )
                        
            else
                case model.pendingPermissionRequest of
                    Just _ ->
                        -- Handle keyboard shortcuts for permission responses
                        case key of
                            89 ->
                                -- 'Y' key
                                update AllowPermission model

                            78 ->
                                -- 'N' key
                                update DenyPermission model

                            _ ->
                                ( model, Cmd.none )

                    Nothing ->
                        -- Normal input handling
                        if key == 13 then
                            -- Enter key
                            if shiftKey || metaKey then
                                -- Shift+Enter or Cmd/Alt+Enter: insert newline (handled by browser)
                                ( model, Cmd.none )

                            else
                            -- Plain Enter: send message
                            if
                                String.trim model.input == "" || not model.isConnected
                            then
                                ( model, Cmd.none )

                            else
                                let
                                    messageJson =
                                        Encode.encode 0
                                            (Encode.object
                                                [ ( "sender", Encode.string "USER" )
                                                , ( "content", Encode.string model.input )
                                                , ( "firstMessage", Encode.bool model.isFirstUserMessage )
                                                , ( "sessionID", Encode.string (Maybe.withDefault "" model.browserSessionID) )
                                                ]
                                            )
                                in
                                ( { model | input = "", isFirstUserMessage = False }
                                , Cmd.batch [ sendMessage messageJson, scrollToBottom () ]
                                )

                        else
                            ( model, Cmd.none )

        ConnectionStatus isConnected ->
            ( { model
                | isConnected = isConnected
                , isTyping =
                    if isConnected then
                        model.isTyping

                    else
                        False

                -- Clear typing if disconnected
              }
            , Cmd.none
            )

        SystemThemeChanged themeString ->
            let
                newSystemTheme =
                    if themeString == "dark" then
                        DarkTerminal

                    else
                        LightModern
            in
            ( { model | systemTheme = newSystemTheme }, Cmd.none )

        StopExecution ->
            let
                stopMessage =
                    Encode.encode 0
                        (Encode.object
                            [ ( "type", Encode.string "stop" )
                            ]
                        )
            in
            ( model, sendMessage stopMessage )

        AllowPermission ->
            case model.pendingPermissionRequest of
                Just permReq ->
                    let
                        newAllowedTools =
                            model.allowedTools ++ [ permReq.toolName ]

                        responseMessage =
                            Encode.encode 0
                                (Encode.object
                                    [ ( "type", Encode.string "permission_response" )
                                    , ( "allowedTools", Encode.list Encode.string newAllowedTools )
                                    , ( "skipPermissions", Encode.bool False )
                                    ]
                                )

                        -- Add permission response to chat history
                        responseItem =
                            ChatPermissionResponse permReq.toolName "allowed" "USER"
                    in
                    ( { model
                        | pendingPermissionRequest = Nothing
                        , messages = model.messages ++ [ responseItem ]
                      }
                    , Cmd.batch [ sendMessage responseMessage, scrollToBottom () ]
                    )

                Nothing ->
                    ( model, Cmd.none )

        AllowPermissionPermanent ->
            case model.pendingPermissionRequest of
                Just permReq ->
                    let
                        newAllowedTools =
                            model.allowedTools ++ [ permReq.toolName ]

                        responseMessage =
                            Encode.encode 0
                                (Encode.object
                                    [ ( "type", Encode.string "permission_response" )
                                    , ( "allowedTools", Encode.list Encode.string newAllowedTools )
                                    , ( "skipPermissions", Encode.bool False )
                                    ]
                                )

                        -- Add permission response to chat history
                        responseItem =
                            ChatPermissionResponse permReq.toolName "allowed_permanent" "USER"
                    in
                    ( { model
                        | pendingPermissionRequest = Nothing
                        , allowedTools = newAllowedTools
                        , messages = model.messages ++ [ responseItem ]
                      }
                    , Cmd.batch [ sendMessage responseMessage, scrollToBottom () ]
                    )

                Nothing ->
                    ( model, Cmd.none )

        DenyPermission ->
            case model.pendingPermissionRequest of
                Just permReq ->
                    let
                        responseMessage =
                            Encode.encode 0
                                (Encode.object
                                    [ ( "type", Encode.string "permission_response" )
                                    , ( "allowedTools", Encode.list Encode.string model.allowedTools )
                                    , ( "skipPermissions", Encode.bool False )
                                    ]
                                )

                        -- Add permission response to chat history
                        responseItem =
                            ChatPermissionResponse permReq.toolName "denied" "USER"
                    in
                    ( { model
                        | pendingPermissionRequest = Nothing
                        , messages = model.messages ++ [ responseItem ]
                      }
                    , Cmd.batch [ sendMessage responseMessage, scrollToBottom () ]
                    )

                Nothing ->
                    ( model, Cmd.none )

        SkipAllPermissions ->
            let
                responseMessage =
                    Encode.encode 0
                        (Encode.object
                            [ ( "type", Encode.string "permission_response" )
                            , ( "allowedTools", Encode.list Encode.string [] )
                            , ( "skipPermissions", Encode.bool True )
                            ]
                        )
            in
            ( { model | permissionDialog = Nothing, skipPermissions = True }
            , sendMessage responseMessage
            )

        FuzzySearchInput query cursorPos ->
            let
                fuzzyMatcher = model.fuzzyMatcher
                newFuzzyMatcher = { fuzzyMatcher | query = query, cursorPosition = cursorPos }
                cmd = if String.length query > 0 then
                    sendFuzzySearch (Encode.encode 0 (Encode.object
                        [ ("type", Encode.string "fuzzy_search")
                        , ("query", Encode.string query)
                        , ("maxResults", Encode.int 50)
                        ]))
                  else
                    Cmd.none
            in
            ( { model | fuzzyMatcher = newFuzzyMatcher }, cmd )

        FuzzySearchResults jsonResults ->
            let
                fuzzyMatcher = model.fuzzyMatcher
                results = case Decode.decodeString fileMatchListDecoder jsonResults of
                    Ok matches -> matches
                    Err _ -> []
                newFuzzyMatcher = { fuzzyMatcher | results = results, selectedIndex = 0 }
            in
            ( { model | fuzzyMatcher = newFuzzyMatcher }, Cmd.none )

        FuzzyMatcherNavigate direction ->
            if model.fuzzyMatcher.isOpen then
                let
                    fuzzyMatcher = model.fuzzyMatcher
                    newIndex = max 0 (min (List.length fuzzyMatcher.results - 1) (fuzzyMatcher.selectedIndex + direction))
                    newFuzzyMatcher = { fuzzyMatcher | selectedIndex = newIndex }
                in
                ( { model | fuzzyMatcher = newFuzzyMatcher }, Cmd.none )
            else
                ( model, Cmd.none )

        FuzzyMatcherSelect ->
            if model.fuzzyMatcher.isOpen then
                case List.head (List.drop model.fuzzyMatcher.selectedIndex model.fuzzyMatcher.results) of
                    Just selectedMatch ->
                        let
                            -- Insert the selected file path at cursor position
                            beforeCursor = String.left (model.fuzzyMatcher.cursorPosition - 1) model.input -- -1 to remove @
                            afterCursor = String.dropLeft (model.fuzzyMatcher.cursorPosition + String.length model.fuzzyMatcher.query) model.input
                            newInput = beforeCursor ++ selectedMatch.file.relPath ++ " " ++ afterCursor
                            
                            -- Close fuzzy matcher
                            newFuzzyMatcher = { isOpen = False, query = "", results = [], selectedIndex = 0, cursorPosition = 0 }
                        in
                        ( { model | input = newInput, fuzzyMatcher = newFuzzyMatcher }, Cmd.none )
                    
                    Nothing ->
                        ( model, Cmd.none )
            else
                ( model, Cmd.none )

        FuzzyMatcherClose ->
            let
                newFuzzyMatcher = { isOpen = False, query = "", results = [], selectedIndex = 0, cursorPosition = 0 }
            in
            ( { model | fuzzyMatcher = newFuzzyMatcher }, Cmd.none )



-- EVENT HELPERS


onKeyDown : (Int -> Bool -> Bool -> msg) -> Html.Attribute msg
onKeyDown tagger =
    on "keydown" <|
        Decode.map3 tagger
            (Decode.field "keyCode" Decode.int)
            (Decode.field "shiftKey" Decode.bool)
            (Decode.oneOf
                [ Decode.field "metaKey" Decode.bool
                , Decode.field "altKey" Decode.bool
                , Decode.succeed False
                ]
            )



-- THEME HELPERS


stringToTheme : String -> Theme
stringToTheme str =
    case str of
        "dark" ->
            DarkTerminal

        "classic" ->
            ClassicTerminal

        "soft" ->
            SoftDark

        "light" ->
            LightModern

        "solarized" ->
            Solarized

        "system" ->
            System

        _ ->
            System


themeToString : Theme -> String
themeToString theme =
    case theme of
        DarkTerminal ->
            "dark"

        ClassicTerminal ->
            "classic"

        SoftDark ->
            "soft"

        LightModern ->
            "light"

        Solarized ->
            "solarized"

        System ->
            "system"


themeToDisplayName : Theme -> String
themeToDisplayName theme =
    case theme of
        DarkTerminal ->
            "Dark Terminal"

        ClassicTerminal ->
            "Classic Terminal"

        SoftDark ->
            "Soft Dark"

        LightModern ->
            "Light Modern"

        Solarized ->
            "Solarized"

        System ->
            "System Default"


getEffectiveTheme : Model -> Theme
getEffectiveTheme model =
    if model.theme == System then
        model.systemTheme

    else
        model.theme


themeToStyles : Theme -> List ( String, String )
themeToStyles theme =
    case theme of
        DarkTerminal ->
            [ ( "background-color", "#0d1117" )
            , ( "color", "#c9d1d9" )
            ]

        ClassicTerminal ->
            [ ( "background-color", "#000000" )
            , ( "color", "#00ff00" )
            ]

        SoftDark ->
            [ ( "background-color", "#1a1b26" )
            , ( "color", "#a9b1d6" )
            ]

        LightModern ->
            [ ( "background-color", "#fafbfc" )
            , ( "color", "#24292f" )
            ]

        Solarized ->
            [ ( "background-color", "#002b36" )
            , ( "color", "#839496" )
            ]

        System ->
            -- This should never be used directly, but provide a fallback
            [ ( "background-color", "#ffffff" )
            , ( "color", "#333333" )
            ]



-- ENCODERS/DECODERS


encodeTodo : Todo -> Encode.Value
encodeTodo todo =
    Encode.object
        [ ( "id", Encode.string todo.id )
        , ( "content", Encode.string todo.content )
        , ( "status", Encode.string todo.status )
        , ( "priority", Encode.string todo.priority )
        ]


encodeChatItem : ChatItem -> Encode.Value
encodeChatItem chatItem =
    case chatItem of
        ChatUser sender ->
            Encode.object
                [ ( "type", Encode.string "user" )
                , ( "sender", Encode.string sender )
                ]

        ChatBot sender ->
            Encode.object
                [ ( "type", Encode.string "bot" )
                , ( "sender", Encode.string sender )
                ]

        ChatContent content ->
            Encode.object
                [ ( "type", Encode.string "content" )
                , ( "content", Encode.string content )
                ]

        ChatClaudeJSON json ->
            Encode.object
                [ ( "type", Encode.string "claudejson" )
                , ( "content", Encode.string json )
                ]

        ChatToolResult content ->
            Encode.object
                [ ( "type", Encode.string "toolresult" )
                , ( "content", Encode.string content )
                ]

        ChatTodoWrite todos ->
            Encode.object
                [ ( "type", Encode.string "todowrite" )
                , ( "todos", Encode.list encodeTodo todos )
                ]

        ChatExecStart ->
            Encode.object
                [ ( "type", Encode.string "exec_start" )
                ]

        ChatExecEnd ->
            Encode.object
                [ ( "type", Encode.string "exec_end" )
                ]

        ChatToolUse _ ->
            Encode.object
                [ ( "type", Encode.string "tool_use" )
                ]

        ChatToolUseWithResult _ _ ->
            Encode.object
                [ ( "type", Encode.string "tool_use_with_result" )
                ]

        ChatPermissionRequest toolName errorMessage toolInput ->
            Encode.object
                [ ( "type", Encode.string "permission_request" )
                , ( "sender", Encode.string toolName )
                , ( "content", Encode.string errorMessage )
                , ( "toolInput"
                  , case toolInput of
                        Just input ->
                            Encode.string input

                        Nothing ->
                            Encode.null
                  )
                ]

        ChatPermissionResponse toolName action username ->
            Encode.object
                [ ( "type", Encode.string "permission_response" )
                , ( "toolName", Encode.string toolName )
                , ( "action", Encode.string action )
                , ( "username", Encode.string username )
                ]

        ChatFuzzySearchResults jsonResults ->
            Encode.object
                [ ( "type", Encode.string "fuzzy_search_results" )
                , ( "content", Encode.string jsonResults )
                ]


chatItemDecoder : Decode.Decoder ChatItem
chatItemDecoder =
    Decode.field "type" Decode.string
        |> Decode.andThen chatItemTypeDecoder


chatItemTypeDecoder : String -> Decode.Decoder ChatItem
chatItemTypeDecoder itemType =
    case itemType of
        "user" ->
            Decode.map ChatUser
                (Decode.field "sender" Decode.string)

        "bot" ->
            Decode.map ChatBot
                (Decode.field "sender" Decode.string)

        "content" ->
            Decode.map ChatContent
                (Decode.field "content" Decode.string)

        "claudejson" ->
            Decode.map ChatClaudeJSON
                (Decode.field "content" Decode.string)

        "toolresult" ->
            Decode.map ChatToolResult
                (Decode.field "content" Decode.string)

        "exec_start" ->
            Decode.succeed ChatExecStart

        "exec_end" ->
            Decode.succeed ChatExecEnd

        "tool_use" ->
            Decode.fail "tool_use items should not be decoded from JSON"

        "tool_use_with_result" ->
            Decode.fail "tool_use_with_result items should not be decoded from JSON"

        "permission_request" ->
            Decode.map3 ChatPermissionRequest
                (Decode.field "sender" Decode.string)
                (Decode.field "content" Decode.string)
                (Decode.maybe (Decode.field "toolInput" Decode.string))

        "permission_response" ->
            Decode.map3 ChatPermissionResponse
                (Decode.field "toolName" Decode.string)
                (Decode.field "action" Decode.string)
                (Decode.field "username" Decode.string)

        "fuzzy_search_results" ->
            Decode.map ChatFuzzySearchResults
                (Decode.field "content" Decode.string)

        _ ->
            Decode.fail <| "Unknown chat item type: " ++ itemType



-- Parse Claude JSON messages into ChatItems


parseClaudeMessage : Model -> ClaudeMessage -> ParseResult
parseClaudeMessage model msg =
    case msg.type_ of
        "result" ->
            -- Handle result messages
            let
                separator =
                    "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

                subtype =
                    Maybe.withDefault "" msg.subtype

                duration =
                    msg.durationMs
                        |> Maybe.map (\ms -> "  Duration: " ++ String.fromInt ms ++ "ms")
                        |> Maybe.withDefault ""

                content =
                    "\n"
                        ++ separator
                        ++ "\n✓ Task completed ("
                        ++ subtype
                        ++ ")\n"
                        ++ duration
                        ++ "\n"
                        ++ separator
                        ++ "\n"
            in
            { messages = [ ChatContent content ]
            , toolUses = []
            }

        "assistant" ->
            -- Handle assistant messages
            case msg.message of
                Just messageContent ->
                    parseClaudeContentList messageContent.content

                Nothing ->
                    { messages = [], toolUses = [] }

        _ ->
            -- Handle user messages with tool results
            case msg.message of
                Just messageContent ->
                    if messageContent.role == Just "user" then
                        let
                            toolResultMessages =
                                messageContent.content
                                    |> List.filterMap
                                        (\content ->
                                            if content.type_ == "tool_result" then
                                                Maybe.map2
                                                    (\toolUseId resultContent ->
                                                        let
                                                            formattedResult =
                                                                if String.endsWith "\n" resultContent then
                                                                    resultContent

                                                                else
                                                                    resultContent ++ "\n"
                                                        in
                                                        case Dict.get toolUseId model.pendingToolUses of
                                                            Just toolUse ->
                                                                ChatToolUseWithResult toolUse formattedResult

                                                            Nothing ->
                                                                ChatToolResult formattedResult
                                                    )
                                                    content.toolUseId
                                                    content.content

                                            else
                                                Nothing
                                        )
                        in
                        { messages = toolResultMessages
                        , toolUses = []
                        }

                    else
                        { messages = [], toolUses = [] }

                Nothing ->
                    { messages = [], toolUses = [] }



-- Parse a list of ClaudeContent into messages and tool uses


parseClaudeContentList : List ClaudeContent -> ParseResult
parseClaudeContentList contents =
    let
        processContent : ClaudeContent -> ( List ChatItem, List ( String, ClaudeContent ) ) -> ( List ChatItem, List ( String, ClaudeContent ) )
        processContent content ( messages, toolUses ) =
            case content.type_ of
                "tool_use" ->
                    -- Check if this is TodoWrite first
                    case content.name of
                        Just "TodoWrite" ->
                            -- Parse TodoWrite specially
                            ( messages ++ parseClaudeContent content, toolUses )

                        _ ->
                            -- Other tool uses
                            case content.id of
                                Just id ->
                                    ( messages ++ [ ChatToolUse content ], ( id, content ) :: toolUses )

                                Nothing ->
                                    -- Fallback to old rendering if no ID
                                    ( messages ++ parseClaudeContent content, toolUses )

                _ ->
                    ( messages ++ parseClaudeContent content, toolUses )

        ( allMessages, allToolUses ) =
            List.foldl processContent ( [], [] ) contents
    in
    { messages = allMessages
    , toolUses = allToolUses
    }


parseClaudeContent : ClaudeContent -> List ChatItem
parseClaudeContent content =
    case content.type_ of
        "text" ->
            content.text
                |> Maybe.map (\t -> [ ChatContent t ])
                |> Maybe.withDefault []

        "tool_use" ->
            case content.name of
                Just "TodoWrite" ->
                    -- Special handling for TodoWrite
                    case content.input of
                        Just input ->
                            case Decode.decodeValue todosDecoder input of
                                Ok todos ->
                                    [ ChatTodoWrite todos ]

                                Err _ ->
                                    -- Fallback to regular tool display
                                    [ ChatContent "\n[Using tool: TodoWrite]\nError parsing todos\n" ]

                        Nothing ->
                            [ ChatContent "\n[Using tool: TodoWrite]\nNo input provided\n" ]

                Just toolName ->
                    let
                        header =
                            "\n[Using tool: " ++ toolName ++ "]\n"

                        details =
                            case content.input of
                                Just input ->
                                    Encode.encode 2 input

                                Nothing ->
                                    ""
                    in
                    [ ChatContent (header ++ details) ]

                Nothing ->
                    []

        _ ->
            []



-- Claude JSON decoders
-- Applicative helpers


andMap : Decode.Decoder a -> Decode.Decoder (a -> b) -> Decode.Decoder b
andMap =
    Decode.map2 (|>)


required : String -> Decode.Decoder a -> Decode.Decoder (a -> b) -> Decode.Decoder b
required fieldName decoder =
    andMap (Decode.field fieldName decoder)


optional : String -> Decode.Decoder a -> a -> Decode.Decoder (a -> b) -> Decode.Decoder b
optional fieldName decoder defaultValue =
    andMap (Decode.field fieldName decoder |> Decode.maybe |> Decode.map (Maybe.withDefault defaultValue))



-- Decoders using our custom helpers


claudeMessageDecoder : Decode.Decoder ClaudeMessage
claudeMessageDecoder =
    Decode.succeed ClaudeMessage
        |> required "type" Decode.string
        |> optional "subtype" (Decode.maybe Decode.string) Nothing
        |> optional "duration_ms" (Decode.maybe Decode.int) Nothing
        |> optional "result" (Decode.maybe Decode.string) Nothing
        |> optional "message" (Decode.maybe claudeMessageContentDecoder) Nothing


claudeMessageContentDecoder : Decode.Decoder ClaudeMessageContent
claudeMessageContentDecoder =
    Decode.succeed ClaudeMessageContent
        |> optional "role" (Decode.maybe Decode.string) Nothing
        |> required "content" (Decode.list claudeContentDecoder)


claudeContentDecoder : Decode.Decoder ClaudeContent
claudeContentDecoder =
    Decode.succeed ClaudeContent
        |> required "type" Decode.string
        |> optional "text" (Decode.maybe Decode.string) Nothing
        |> optional "name" (Decode.maybe Decode.string) Nothing
        |> optional "input" (Decode.maybe Decode.value) Nothing
        |> optional "content" (Decode.maybe Decode.string) Nothing
        |> optional "id" (Decode.maybe Decode.string) Nothing
        |> optional "tool_use_id" (Decode.maybe Decode.string) Nothing


todoDecoder : Decode.Decoder Todo
todoDecoder =
    Decode.succeed Todo
        |> required "id" Decode.string
        |> required "content" Decode.string
        |> required "status" Decode.string
        |> required "priority" Decode.string


todosDecoder : Decode.Decoder (List Todo)
todosDecoder =
    Decode.field "todos" (Decode.list todoDecoder)


fileInfoDecoder : Decode.Decoder FileInfo
fileInfoDecoder =
    Decode.succeed FileInfo
        |> required "path" Decode.string
        |> required "name" Decode.string
        |> required "isDir" Decode.bool
        |> required "relPath" Decode.string


fileMatchDecoder : Decode.Decoder FileMatch
fileMatchDecoder =
    Decode.succeed FileMatch
        |> required "file" fileInfoDecoder
        |> required "score" Decode.int
        |> required "matches" (Decode.list Decode.int)


fileMatchListDecoder : Decode.Decoder (List FileMatch)
fileMatchListDecoder =
    Decode.list fileMatchDecoder



-- SUBSCRIPTIONS


subscriptions : Model -> Sub Msg
subscriptions _ =
    Sub.batch
        [ messageReceiver Receive
        , connectionStatusReceiver ConnectionStatus
        , systemThemeChanged SystemThemeChanged
        ]



-- VIEW


view : Model -> Html Msg
view model =
    div []
        [ div [ class "chat-container" ]
            [ div [ class "header" ]
                [ div [ class "title-container" ]
                    [ h1 [] [ text "swe-swe" ]
                    , div
                        [ class "connection-status"
                        , class
                            (if model.isConnected then
                                "connected"

                             else
                                "disconnected"
                            )
                        ]
                        []
                    , if model.isConnected then
                        text "connected"

                      else
                        text "disconnected"
                    ]
                , div [ class "theme-selector" ]
                    [ text "Theme: "
                    , select
                        [ class "theme-dropdown"
                        , on "change" (Decode.map ThemeChanged targetValue)
                        ]
                        [ option
                            [ value "system"
                            , selected (model.theme == System)
                            ]
                            [ text "System Default" ]
                        , option
                            [ value "dark"
                            , selected (model.theme == DarkTerminal)
                            ]
                            [ text "Dark Terminal" ]
                        , option
                            [ value "classic"
                            , selected (model.theme == ClassicTerminal)
                            ]
                            [ text "Classic Terminal" ]
                        , option
                            [ value "soft"
                            , selected (model.theme == SoftDark)
                            ]
                            [ text "Soft Dark" ]
                        , option
                            [ value "light"
                            , selected (model.theme == LightModern)
                            ]
                            [ text "Light Modern" ]
                        , option
                            [ value "solarized"
                            , selected (model.theme == Solarized)
                            ]
                            [ text "Solarized" ]
                        ]
                    ]
                ]
            , div
                ([ class "messages" ]
                    ++ List.map (\( k, v ) -> style k v) (themeToStyles (getEffectiveTheme model))
                )
                (renderMessages model model.messages
                    ++ (if model.isTyping then
                            [ div [ class "typing-indicator" ]
                                [ div [ class "typing-dots" ]
                                    [ div [ class "typing-dot" ] []
                                    , div [ class "typing-dot" ] []
                                    , div [ class "typing-dot" ] []
                                    ]
                                , span
                                    [ class "stop-button"
                                    , onClick StopExecution
                                    ]
                                    [ text "Stop" ]
                                ]
                            ]

                        else
                            []
                       )
                )
            , div [ class "input-container" ]
                (case model.pendingPermissionRequest of
                    Just permissionReq ->
                        [ div [ class "permission-inline" ]
                            [ span [ class "permission-prompt-inline" ]
                                [ text ("Allow " ++ permissionReq.toolName ++ " access? ") ]
                            , button
                                [ class "permission-button-inline allow"
                                , onClick AllowPermission
                                ]
                                [ text "Y" ]
                            , button
                                [ class "permission-button-inline deny"
                                , onClick DenyPermission
                                ]
                                [ text "N" ]
                            , button
                                [ class "permission-button-inline yolo"
                                , onClick AllowPermissionPermanent
                                ]
                                [ text "YOLO" ]
                            ]
                        ]

                    Nothing ->
                        [ div [ class "input-wrapper" ]
                            [ textarea
                                [ class "message-input"
                                , placeholder "Type a message... (Enter to send, Shift+Enter for new line, @ for file picker)"
                                , value model.input
                                , onInput Input
                                , onKeyDown KeyDown
                                , autofocus True
                                ]
                                []
                            , if model.fuzzyMatcher.isOpen then
                                fuzzyMatcherView model.fuzzyMatcher
                              else
                                text ""
                            ]
                        , button
                            [ class "send-button"
                            , onClick Send
                            , disabled (String.trim model.input == "" || not model.isConnected)
                            ]
                            [ text
                                (if model.isConnected then
                                    "Send"

                                 else
                                    "Offline"
                                )
                            ]
                        ]
                )
            ]
        , permissionDialogView model.permissionDialog
        ]



-- Permission Dialog View


permissionDialogView : Maybe PermissionDialogState -> Html Msg
permissionDialogView maybeDialog =
    case maybeDialog of
        Just dialog ->
            div [ class "permission-overlay" ]
                [ div [ class "permission-dialog" ]
                    [ div [ class "permission-header" ]
                        [ h3 [] [ text "Claude requests permission" ]
                        ]
                    , div [ class "permission-content" ]
                        [ p [] [ text ("Claude wants to use: " ++ dialog.toolName) ]
                        , case dialog.toolInput of
                            Just input ->
                                div [ class "permission-tool-details" ]
                                    [ p [ style "font-weight" "bold", style "margin-bottom" "0.5rem" ] [ text "Tool parameters:" ]
                                    , div [ class "permission-tool-input" ]
                                        [ formatToolInput dialog.toolName input
                                        ]
                                    ]

                            Nothing ->
                                text ""
                        , div [ class "permission-error" ]
                            [ text dialog.errorMessage ]
                        ]
                    , div [ class "permission-actions" ]
                        [ button
                            [ class "permission-button permission-allow"
                            , onClick AllowPermission
                            ]
                            [ text "Yes (Y)" ]
                        , button
                            [ class "permission-button permission-deny"
                            , onClick DenyPermission
                            ]
                            [ text "No (N)" ]
                        , button
                            [ class "permission-button permission-allow-permanent"
                            , onClick AllowPermissionPermanent
                            ]
                            [ text "Always allow this tool" ]
                        , button
                            [ class "permission-button permission-skip-all"
                            , onClick SkipAllPermissions
                            ]
                            [ text "--dangerously-skip-permissions" ]
                        ]
                    ]
                ]

        Nothing ->
            text ""


-- Fuzzy Matcher View


fuzzyMatcherView : FuzzyMatcherState -> Html Msg
fuzzyMatcherView matcher =
    if not matcher.isOpen then
        text ""
    else
        div [ class "fuzzy-matcher-dropdown" ]
            [ div [ class "fuzzy-matcher-header" ]
                [ text ("Files matching \"" ++ matcher.query ++ "\"") ]
            , div [ class "fuzzy-matcher-results" ]
                (List.indexedMap (fuzzyMatcherResultView matcher.selectedIndex) matcher.results)
            ]


fuzzyMatcherResultView : Int -> Int -> FileMatch -> Html Msg
fuzzyMatcherResultView selectedIndex index fileMatch =
    let
        isSelected = index == selectedIndex
        fileIcon = if fileMatch.file.isDir then "📁" else "📄"
        highlightedName = highlightMatches fileMatch.file.name fileMatch.matches
    in
    div 
        [ class "fuzzy-matcher-result"
        , class (if isSelected then "selected" else "")
        , onClick FuzzyMatcherSelect
        ]
        [ div [ class "fuzzy-matcher-result-icon" ] [ text fileIcon ]
        , div [ class "fuzzy-matcher-result-content" ]
            [ div [ class "fuzzy-matcher-result-name" ] highlightedName
            , div [ class "fuzzy-matcher-result-path" ] [ text fileMatch.file.relPath ]
            ]
        ]


highlightMatches : String -> List Int -> List (Html Msg)
highlightMatches str matches =
    let
        chars = String.toList str
        
        renderChar : Int -> Char -> Html Msg
        renderChar index char =
            if List.member index matches then
                span [ class "fuzzy-match-highlight" ] [ text (String.fromChar char) ]
            else
                text (String.fromChar char)
    in
    List.indexedMap renderChar chars



-- Format tool input for display in permission dialog
-- Render diff for Edit or MultiEdit tool


-- Diff line type for better diff rendering
type DiffLineType
    = Added String
    | Removed String 
    | Unchanged String

-- Generate a unified diff from old and new strings
generateUnifiedDiff : String -> String -> List DiffLineType
generateUnifiedDiff oldString newString =
    let
        oldLines = String.lines oldString
        newLines = String.lines newString
        
        -- Simple line-based diff algorithm
        -- This is a simplified version - more complex algorithms like Myers could be used
        diffLines = computeLineDiff oldLines newLines
    in
    diffLines

-- Simple diff computation that finds added/removed/unchanged lines
computeLineDiff : List String -> List String -> List DiffLineType
computeLineDiff oldLines newLines =
    let
        oldSet = Set.fromList oldLines
        newSet = Set.fromList newLines
        
        removedLines = Set.diff oldSet newSet |> Set.toList
        addedLines = Set.diff newSet oldSet |> Set.toList
        unchangedLines = Set.intersect oldSet newSet |> Set.toList
        
        -- Create a simple ordering: removed, then added, then some unchanged for context
        diffResult = 
            (List.map Removed removedLines) ++ 
            (List.map Added addedLines) ++
            (List.take 3 unchangedLines |> List.map Unchanged) -- Show some context
    in
    diffResult

renderDiff : String -> String -> Html Msg
renderDiff oldString newString =
    let
        diffLines = generateUnifiedDiff oldString newString
        
        renderDiffLine diffLine =
            case diffLine of
                Added content ->
                    div [ class "diff-line diff-added" ]
                        [ span [ class "diff-marker" ] [ text "+ " ]
                        , span [ class "diff-content" ] [ text content ]
                        ]
                
                Removed content ->
                    div [ class "diff-line diff-removed" ]
                        [ span [ class "diff-marker" ] [ text "- " ]
                        , span [ class "diff-content" ] [ text content ]
                        ]
                
                Unchanged content ->
                    div [ class "diff-line diff-context" ]
                        [ span [ class "diff-marker" ] [ text "  " ]
                        , span [ class "diff-content" ] [ text content ]
                        ]
                        
        -- If diff is too complex, fall back to side-by-side view
        shouldShowSideBySide = 
            (String.lines oldString |> List.length) > 10 || 
            (String.lines newString |> List.length) > 10
    in
    if shouldShowSideBySide then
        renderSideBySideDiff oldString newString
    else
        div [ class "diff-container unified" ]
            [ div [ class "diff-header" ]
                [ text "Changes:" ]
            , div [ class "diff-content" ]
                (List.map renderDiffLine diffLines)
            ]

-- Render side-by-side diff for larger changes
renderSideBySideDiff : String -> String -> Html Msg
renderSideBySideDiff oldString newString =
    let
        oldLines = String.lines oldString
        newLines = String.lines newString
        maxLines = max (List.length oldLines) (List.length newLines)
        
        renderSideBySideLine index =
            let
                oldLine = List.drop index oldLines |> List.head |> Maybe.withDefault ""
                newLine = List.drop index newLines |> List.head |> Maybe.withDefault ""
                hasOldLine = index < List.length oldLines
                hasNewLine = index < List.length newLines
            in
            div [ class "diff-line-pair" ]
                [ div [ class "diff-side diff-old" ]
                    [ span [ class "diff-line-number" ] [ text (String.fromInt (index + 1)) ]
                    , span [ class "diff-marker" ] 
                        [ text (if hasOldLine then "- " else "  ") ]
                    , span [ class "diff-content" ] 
                        [ text (if hasOldLine then oldLine else "") ]
                    ]
                , div [ class "diff-side diff-new" ]
                    [ span [ class "diff-line-number" ] [ text (String.fromInt (index + 1)) ]
                    , span [ class "diff-marker" ] 
                        [ text (if hasNewLine then "+ " else "  ") ]
                    , span [ class "diff-content" ] 
                        [ text (if hasNewLine then newLine else "") ]
                    ]
                ]
    in
    div [ class "diff-container side-by-side" ]
        [ div [ class "diff-header" ]
            [ text "Changes (side-by-side):" ]
        , div [ class "diff-content" ]
            (List.range 0 (maxLines - 1) |> List.map renderSideBySideLine)
        ]



-- Render a single edit as a diff


renderEditAsDiff : Decode.Value -> Html Msg
renderEditAsDiff editJson =
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
            renderDiff edit.oldString edit.newString

        Err _ ->
            pre [ style "font-size" "0.9em", style "overflow" "auto" ]
                [ text (Encode.encode 2 editJson) ]


formatToolInput : String -> String -> Html Msg
formatToolInput toolName inputJson =
    case Decode.decodeString Decode.value inputJson of
        Ok jsonValue ->
            case toolName of
                "Bash" ->
                    case Decode.decodeString (Decode.field "command" Decode.string) inputJson of
                        Ok command ->
                            div []
                                [ p [ style "margin" "0.25rem 0" ] [ text ("Command: " ++ command) ]
                                ]

                        Err _ ->
                            pre [ style "font-size" "0.9em", style "overflow" "auto" ] [ text inputJson ]

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
                                [ p [ style "margin" "0.25rem 0" ] [ text ("File: " ++ edit.filePath) ]
                                , details [ style "margin-top" "0.5rem" ]
                                    [ summary [] [ text "View changes" ]
                                    , div [ style "margin-top" "0.5rem" ]
                                        [ p [ style "font-weight" "bold" ] [ text "Replace:" ]
                                        , pre [ style "background-color" "#ffeeee", style "padding" "0.5rem", style "overflow" "auto" ]
                                            [ text
                                                (String.left 200 edit.oldString
                                                    ++ (if String.length edit.oldString > 200 then
                                                            "..."

                                                        else
                                                            ""
                                                       )
                                                )
                                            ]
                                        , p [ style "font-weight" "bold", style "margin-top" "0.5rem" ] [ text "With:" ]
                                        , pre [ style "background-color" "#eeffee", style "padding" "0.5rem", style "overflow" "auto" ]
                                            [ text
                                                (String.left 200 edit.newString
                                                    ++ (if String.length edit.newString > 200 then
                                                            "..."

                                                        else
                                                            ""
                                                       )
                                                )
                                            ]
                                        ]
                                    ]
                                ]

                        Err _ ->
                            pre [ style "font-size" "0.9em", style "overflow" "auto" ] [ text inputJson ]

                "Read" ->
                    case Decode.decodeString (Decode.field "file_path" Decode.string) inputJson of
                        Ok filePath ->
                            div []
                                [ p [ style "margin" "0.25rem 0" ] [ text ("File: " ++ filePath) ]
                                ]

                        Err _ ->
                            pre [ style "font-size" "0.9em", style "overflow" "auto" ] [ text inputJson ]

                "Write" ->
                    case Decode.decodeString (Decode.field "file_path" Decode.string) inputJson of
                        Ok filePath ->
                            div []
                                [ p [ style "margin" "0.25rem 0" ] [ text ("File: " ++ filePath) ]
                                ]

                        Err _ ->
                            pre [ style "font-size" "0.9em", style "overflow" "auto" ] [ text inputJson ]

                "MultiEdit" ->
                    case
                        Decode.decodeString
                            (Decode.map2 (\fp edits -> { filePath = fp, editsCount = edits })
                                (Decode.field "file_path" Decode.string)
                                (Decode.field "edits" (Decode.list Decode.value) |> Decode.map List.length)
                            )
                            inputJson
                    of
                        Ok info ->
                            div []
                                [ p [ style "margin" "0.25rem 0" ] [ text ("File: " ++ info.filePath) ]
                                , p [ style "margin" "0.25rem 0" ] [ text ("Number of edits: " ++ String.fromInt info.editsCount) ]
                                ]

                        Err _ ->
                            pre [ style "font-size" "0.9em", style "overflow" "auto" ] [ text inputJson ]

                _ ->
                    -- For other tools, show formatted JSON
                    pre [ style "font-size" "0.9em", style "overflow" "auto" ]
                        [ text (Encode.encode 2 jsonValue) ]

        Err _ ->
            -- If JSON parsing fails, show raw text
            pre [ style "font-size" "0.9em", style "overflow" "auto" ] [ text inputJson ]



-- State for rendering messages


type alias RenderState =
    { currentSender : Maybe String
    , accumulatedContent : String
    , elements : List (Html Msg)
    }



-- Render messages by grouping content from the same sender


renderMessages : Model -> List ChatItem -> List (Html Msg)
renderMessages model items =
    let
        initialState : RenderState
        initialState =
            { currentSender = Nothing
            , accumulatedContent = ""
            , elements = []
            }

        -- Helper to render accumulated content when we have some
        renderAccumulatedContent : RenderState -> List (Html Msg)
        renderAccumulatedContent state =
            if String.trim state.accumulatedContent == "" then
                state.elements

            else
                let
                    senderClass =
                        case state.currentSender of
                            Just "USER" ->
                                "user-message"

                            Just "swe-swe" ->
                                "bot-message"

                            _ ->
                                ""
                in
                state.elements ++ [ pre [ class "message-content", class senderClass, style "white-space" "pre-wrap" ] (ansiToElmHtml state.accumulatedContent) ]

        renderItem : ChatItem -> RenderState -> RenderState
        renderItem item state =
            case item of
                ChatUser sender ->
                    -- Render any accumulated content first, then start new sender
                    let
                        elementsWithAccumulated =
                            renderAccumulatedContent state
                    in
                    { currentSender = Just sender
                    , accumulatedContent = ""
                    , elements = elementsWithAccumulated ++ [ div [ class "message-sender user-sender" ] [ text sender ] ]
                    }

                ChatBot sender ->
                    -- Render any accumulated content first, then start new sender
                    let
                        elementsWithAccumulated =
                            renderAccumulatedContent state
                    in
                    { currentSender = Just sender
                    , accumulatedContent = ""
                    , elements = elementsWithAccumulated ++ [ div [ class "message-sender bot-sender" ] [ text sender ] ]
                    }

                ChatContent content ->
                    -- Accumulate content for the current sender
                    { state | accumulatedContent = state.accumulatedContent ++ content }

                ChatToolUse toolUse ->
                    -- Render tool use in compact format
                    let
                        elementsWithAccumulated =
                            renderAccumulatedContent state

                        toolName =
                            Maybe.withDefault "Unknown" toolUse.name

                        toolElement =
                            case toolUse.input of
                                Just input ->
                                    if toolName == "Edit" then
                                        -- Handle Edit tool with diff display
                                        case
                                            Decode.decodeValue
                                                (Decode.map3 (\fp old new -> { filePath = fp, oldString = old, newString = new })
                                                    (Decode.field "file_path" Decode.string)
                                                    (Decode.field "old_string" Decode.string)
                                                    (Decode.field "new_string" Decode.string)
                                                )
                                                input
                                        of
                                            Ok edit ->
                                                div [ class "tool-use" ]
                                                    [ div [ class "tool-header" ]
                                                        [ text ("[Edit] " ++ edit.filePath) ]
                                                    , renderDiff edit.oldString edit.newString
                                                    ]

                                            Err _ ->
                                                div [ class "tool-use" ]
                                                    [ text ("[" ++ toolName ++ "] ")
                                                    , Html.code [] [ text (Encode.encode 0 input) ]
                                                    ]

                                    else if toolName == "MultiEdit" then
                                        -- Handle MultiEdit tool with multiple diffs
                                        case
                                            Decode.decodeValue
                                                (Decode.map2 (\fp edits -> { filePath = fp, edits = edits })
                                                    (Decode.field "file_path" Decode.string)
                                                    (Decode.field "edits" (Decode.list Decode.value))
                                                )
                                                input
                                        of
                                            Ok multiEdit ->
                                                let
                                                    editElements =
                                                        List.indexedMap
                                                            (\idx editJson ->
                                                                div [ class "multi-edit-item" ]
                                                                    [ div [ class "edit-number" ]
                                                                        [ text ("Edit " ++ String.fromInt (idx + 1) ++ " of " ++ String.fromInt (List.length multiEdit.edits)) ]
                                                                    , renderEditAsDiff editJson
                                                                    ]
                                                            )
                                                            multiEdit.edits
                                                in
                                                div [ class "tool-use" ]
                                                    [ div [ class "tool-header" ]
                                                        [ text ("[MultiEdit] " ++ multiEdit.filePath) ]
                                                    , div [ class "multi-edit-container" ] editElements
                                                    ]

                                            Err _ ->
                                                div [ class "tool-use" ]
                                                    [ text ("[" ++ toolName ++ "] ")
                                                    , Html.code [] [ text (Encode.encode 0 input) ]
                                                    ]

                                    else
                                        -- Other tools show JSON
                                        div [ class "tool-use" ]
                                            [ text ("[" ++ toolName ++ "] ")
                                            , Html.code [] [ text (Encode.encode 0 input) ]
                                            ]

                                Nothing ->
                                    div [ class "tool-use" ]
                                        [ text ("[" ++ toolName ++ "]") ]
                    in
                    { state
                        | accumulatedContent = ""
                        , elements = elementsWithAccumulated ++ [ toolElement ]
                    }

                ChatToolResult content ->
                    -- Render accumulated content first, then render the tool result
                    let
                        elementsWithAccumulated =
                            renderAccumulatedContent state

                        toolResultElement =
                            details [ class "tool-result" ]
                                [ summary [] [ text "Tool Result" ]
                                , div [ class "tool-result-content" ] (ansiToElmHtml content)
                                ]
                    in
                    { state
                        | accumulatedContent = ""
                        , elements = elementsWithAccumulated ++ [ toolResultElement ]
                    }

                ChatToolUseWithResult toolUse result ->
                    -- Render tool use and result as details/summary
                    let
                        elementsWithAccumulated =
                            renderAccumulatedContent state

                        toolName =
                            Maybe.withDefault "Unknown" toolUse.name

                        toolElement =
                            case toolUse.input of
                                Just input ->
                                    if toolName == "Edit" then
                                        -- Handle Edit tool with diff display
                                        case
                                            Decode.decodeValue
                                                (Decode.map3 (\fp old new -> { filePath = fp, oldString = old, newString = new })
                                                    (Decode.field "file_path" Decode.string)
                                                    (Decode.field "old_string" Decode.string)
                                                    (Decode.field "new_string" Decode.string)
                                                )
                                                input
                                        of
                                            Ok edit ->
                                                details [ class "tool-result" ]
                                                    [ summary []
                                                        [ text ("[Edit] " ++ edit.filePath) ]
                                                    , div [ class "tool-result-content" ]
                                                        [ renderDiff edit.oldString edit.newString
                                                        , div [ class "result-separator" ] [ text "Result:" ]
                                                        , div [] (ansiToElmHtml result)
                                                        ]
                                                    ]

                                            Err _ ->
                                                details [ class "tool-result" ]
                                                    [ summary []
                                                        [ text ("[" ++ toolName ++ "] ")
                                                        , Html.code [] [ text (Encode.encode 0 input) ]
                                                        ]
                                                    , div [ class "tool-result-content" ] (ansiToElmHtml result)
                                                    ]

                                    else if toolName == "MultiEdit" then
                                        -- Handle MultiEdit tool with multiple diffs
                                        case
                                            Decode.decodeValue
                                                (Decode.map2 (\fp edits -> { filePath = fp, edits = edits })
                                                    (Decode.field "file_path" Decode.string)
                                                    (Decode.field "edits" (Decode.list Decode.value))
                                                )
                                                input
                                        of
                                            Ok multiEdit ->
                                                details [ class "tool-result" ]
                                                    [ summary []
                                                        [ text ("[MultiEdit] " ++ multiEdit.filePath ++ " (" ++ String.fromInt (List.length multiEdit.edits) ++ " edits)") ]
                                                    , div [ class "tool-result-content" ]
                                                        [ div [ class "multi-edit-container" ]
                                                            (List.indexedMap
                                                                (\idx editJson ->
                                                                    div [ class "multi-edit-item" ]
                                                                        [ div [ class "edit-number" ]
                                                                            [ text ("Edit " ++ String.fromInt (idx + 1) ++ " of " ++ String.fromInt (List.length multiEdit.edits)) ]
                                                                        , renderEditAsDiff editJson
                                                                        ]
                                                                )
                                                                multiEdit.edits
                                                            )
                                                        , div [ class "result-separator" ] [ text "Result:" ]
                                                        , div [] (ansiToElmHtml result)
                                                        ]
                                                    ]

                                            Err _ ->
                                                details [ class "tool-result" ]
                                                    [ summary []
                                                        [ text ("[" ++ toolName ++ "] ")
                                                        , Html.code [] [ text (Encode.encode 0 input) ]
                                                        ]
                                                    , div [ class "tool-result-content" ] (ansiToElmHtml result)
                                                    ]

                                    else
                                        -- Other tools show JSON
                                        details [ class "tool-result" ]
                                            [ summary []
                                                [ text ("[" ++ toolName ++ "] ")
                                                , Html.code [] [ text (Encode.encode 0 input) ]
                                                ]
                                            , div [ class "tool-result-content" ] (ansiToElmHtml result)
                                            ]

                                Nothing ->
                                    details [ class "tool-result" ]
                                        [ summary []
                                            [ text ("[" ++ toolName ++ "]") ]
                                        , div [ class "tool-result-content" ] (ansiToElmHtml result)
                                        ]
                    in
                    { state
                        | accumulatedContent = ""
                        , elements = elementsWithAccumulated ++ [ toolElement ]
                    }

                ChatTodoWrite todos ->
                    -- Render accumulated content first, then render the todos
                    let
                        elementsWithAccumulated =
                            renderAccumulatedContent state

                        renderTodo todo =
                            let
                                statusSymbol =
                                    if todo.status == "completed" then
                                        "☑ "

                                    else
                                        "☐ "

                                todoStyle =
                                    if todo.priority == "high" then
                                        [ style "font-weight" "bold" ]

                                    else
                                        []
                            in
                            div ([ class "todo-item" ] ++ todoStyle)
                                [ text (statusSymbol ++ todo.content) ]

                        todoListElement =
                            div [ class "todo-list" ]
                                [ div [ class "todo-header" ] [ text "📋 Todo List" ]
                                , div [ class "todo-items" ] (List.map renderTodo todos)
                                ]
                    in
                    { state
                        | accumulatedContent = ""
                        , elements = elementsWithAccumulated ++ [ todoListElement ]
                    }

                ChatClaudeJSON _ ->
                    -- This should not appear in messages as it's parsed in update
                    state

                ChatExecStart ->
                    -- Don't render anything for exec start
                    state

                ChatExecEnd ->
                    -- Don't render anything for exec end
                    state

                ChatPermissionRequest toolName errorMessage toolInput ->
                    -- Render permission request as a notice
                    let
                        elementsWithAccumulated =
                            renderAccumulatedContent state

                        inputDisplay =
                            case toolInput of
                                Just input ->
                                    case Decode.decodeString Decode.value input of
                                        Ok jsonValue ->
                                            pre [ class "permission-notice-input" ]
                                                [ text (Encode.encode 2 jsonValue) ]

                                        Err _ ->
                                            pre [ class "permission-notice-input" ]
                                                [ text input ]

                                Nothing ->
                                    text ""

                        permissionElement =
                            div [ class "permission-notice" ]
                                [ div [ class "permission-notice-header" ]
                                    [ text ("⚠️ Permission required for: " ++ toolName) ]
                                , div [ class "permission-notice-body" ]
                                    [ text errorMessage
                                    , inputDisplay
                                    ]
                                ]
                    in
                    { state
                        | accumulatedContent = ""
                        , elements = elementsWithAccumulated ++ [ permissionElement ]
                    }

                ChatPermissionResponse toolName action username ->
                    -- Render permission response based on who responded
                    let
                        elementsWithAccumulated =
                            renderAccumulatedContent state

                        actionText =
                            case action of
                                "allowed" ->
                                    "✓ Allowed " ++ toolName ++ " access"

                                "allowed_permanent" ->
                                    "✓ Always allowed " ++ toolName ++ " access"

                                "denied" ->
                                    "✗ Denied " ++ toolName ++ " access"

                                _ ->
                                    action ++ " " ++ toolName

                        -- Determine message class based on the current sender
                        messageClass =
                            case state.currentSender of
                                Just "USER" ->
                                    "user-message"

                                Just "swe-swe" ->
                                    "bot-message"

                                _ ->
                                    "user-message"

                        -- Default to user-message for permission responses
                        responseElement =
                            div [ class "message-wrapper" ]
                                [ div [ class "message", class messageClass ]
                                    [ span [ class "message-content" ] [ text actionText ] ]
                                ]
                    in
                    { state
                        | accumulatedContent = ""
                        , elements = elementsWithAccumulated ++ [ responseElement ]
                    }

                ChatFuzzySearchResults _ ->
                    -- Don't render fuzzy search results in the chat view
                    state

        finalState =
            List.foldl renderItem initialState items
    in
    renderAccumulatedContent finalState
