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
import Html.Attributes exposing (checked, class, disabled, placeholder, selected, style, type_, value)
import Html.Events exposing (keyCode, on, onClick, onInput, targetValue)
import Json.Decode as Decode
import Json.Encode as Encode
import Html.Attributes exposing (autofocus)



-- PORTS


port sendMessage : String -> Cmd msg


port messageReceiver : (String -> msg) -> Sub msg


port scrollToBottom : () -> Cmd msg


port connectionStatusReceiver : (Bool -> msg) -> Sub msg


port systemThemeChanged : (String -> msg) -> Sub msg


port focusMessageInput : () -> Cmd msg



-- MAIN


type alias Flags =
    { systemTheme : String
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
    , pendingToolUses : Dict String ClaudeContent
    , allowedTools : List String
    , skipPermissions : Bool
    , permissionDialog : Maybe PermissionDialogState
    }


type alias PermissionDialogState =
    { toolName : String
    , errorMessage : String
    , toolInput : Maybe String
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
    in
    ( { input = ""
      , messages = []
      , currentSender = Nothing
      , theme = System
      , isConnected = False
      , systemTheme = initialTheme
      , isTyping = False
      , isFirstUserMessage = True
      , pendingToolUses = Dict.empty
      , allowedTools = []
      , skipPermissions = False
      , permissionDialog = Nothing
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


update : Msg -> Model -> ( Model, Cmd Msg )
update msg model =
    case msg of
        Input text ->
            ( { model | input = text }, Cmd.none )

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
                                ]
                            )
                in
                ( { model | input = "", isFirstUserMessage = False }
                , Cmd.batch [ sendMessage messageJson, scrollToBottom () ]
                )

        Receive json ->
            case Decode.decodeString chatItemDecoder json of
                Ok chatItem ->
                    case chatItem of
                        ChatUser sender ->
                            ( { model
                                | messages = model.messages ++ [ chatItem ]
                                , currentSender = Just sender
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
                                | permissionDialog = Just { toolName = toolName, errorMessage = errorMessage, toolInput = toolInput }
                                , messages = model.messages ++ [ chatItem ]
                              }
                            , scrollToBottom ()
                            )

                Err _ ->
                    ( model, Cmd.none )

        ThemeChanged themeString ->
            let
                newTheme =
                    stringToTheme themeString
            in
            ( { model | theme = newTheme }, Cmd.none )

        KeyDown key shiftKey metaKey ->
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
            case model.permissionDialog of
                Just dialog ->
                    let
                        newAllowedTools =
                            model.allowedTools ++ [ dialog.toolName ]

                        responseMessage =
                            Encode.encode 0
                                (Encode.object
                                    [ ( "type", Encode.string "permission_response" )
                                    , ( "allowedTools", Encode.list Encode.string newAllowedTools )
                                    , ( "skipPermissions", Encode.bool False )
                                    ]
                                )
                    in
                    ( { model | permissionDialog = Nothing }
                    , sendMessage responseMessage
                    )

                Nothing ->
                    ( model, Cmd.none )

        AllowPermissionPermanent ->
            case model.permissionDialog of
                Just dialog ->
                    let
                        newAllowedTools =
                            model.allowedTools ++ [ dialog.toolName ]

                        responseMessage =
                            Encode.encode 0
                                (Encode.object
                                    [ ( "type", Encode.string "permission_response" )
                                    , ( "allowedTools", Encode.list Encode.string newAllowedTools )
                                    , ( "skipPermissions", Encode.bool False )
                                    ]
                                )
                    in
                    ( { model | permissionDialog = Nothing, allowedTools = newAllowedTools }
                    , sendMessage responseMessage
                    )

                Nothing ->
                    ( model, Cmd.none )

        DenyPermission ->
            let
                responseMessage =
                    Encode.encode 0
                        (Encode.object
                            [ ( "type", Encode.string "permission_response" )
                            , ( "allowedTools", Encode.list Encode.string model.allowedTools )
                            , ( "skipPermissions", Encode.bool False )
                            ]
                        )
            in
            ( { model | permissionDialog = Nothing }
            , sendMessage responseMessage
            )

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
                , ( "toolInput", case toolInput of
                    Just input -> Encode.string input
                    Nothing -> Encode.null
                  )
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
            [ textarea
                [ class "message-input"
                , placeholder "Type a message... (Enter to send, Shift+Enter for new line)"
                , value model.input
                , onInput Input
                , onKeyDown KeyDown
                , autofocus True
                ]
                []
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


-- Format tool input for display in permission dialog


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
                    case Decode.decodeString 
                        (Decode.map3 (\fp old new -> { filePath = fp, oldString = old, newString = new })
                            (Decode.field "file_path" Decode.string)
                            (Decode.field "old_string" Decode.string)
                            (Decode.field "new_string" Decode.string)
                        ) inputJson of
                        Ok edit ->
                            div []
                                [ p [ style "margin" "0.25rem 0" ] [ text ("File: " ++ edit.filePath) ]
                                , details [ style "margin-top" "0.5rem" ]
                                    [ summary [] [ text "View changes" ]
                                    , div [ style "margin-top" "0.5rem" ]
                                        [ p [ style "font-weight" "bold" ] [ text "Replace:" ]
                                        , pre [ style "background-color" "#ffeeee", style "padding" "0.5rem", style "overflow" "auto" ] 
                                            [ text (String.left 200 edit.oldString ++ if String.length edit.oldString > 200 then "..." else "") ]
                                        , p [ style "font-weight" "bold", style "margin-top" "0.5rem" ] [ text "With:" ]
                                        , pre [ style "background-color" "#eeffee", style "padding" "0.5rem", style "overflow" "auto" ] 
                                            [ text (String.left 200 edit.newString ++ if String.length edit.newString > 200 then "..." else "") ]
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
                    case Decode.decodeString 
                        (Decode.map2 (\fp edits -> { filePath = fp, editsCount = edits })
                            (Decode.field "file_path" Decode.string)
                            (Decode.field "edits" (Decode.list Decode.value) |> Decode.map List.length)
                        ) inputJson of
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
                state.elements ++ [ div [ class "message-content", class senderClass ] (ansiToElmHtml state.accumulatedContent) ]

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

                        inputDisplay =
                            case toolUse.input of
                                Just input ->
                                    -- Special handling for MultiEdit with potentially large edits array
                                    if toolName == "MultiEdit" then
                                        case Decode.decodeValue (Decode.field "edits" (Decode.list Decode.value)) input of
                                            Ok edits ->
                                                let
                                                    editCount =
                                                        List.length edits

                                                    truncatedInput =
                                                        case Decode.decodeValue (Decode.dict Decode.value) input of
                                                            Ok dict ->
                                                                Dict.insert "edits" (Encode.string ("[" ++ String.fromInt editCount ++ " edits]")) dict
                                                                    |> Encode.dict identity identity
                                                                    |> Encode.encode 0

                                                            Err _ ->
                                                                Encode.encode 0 input
                                                in
                                                truncatedInput

                                            Err _ ->
                                                Encode.encode 0 input

                                    else
                                        Encode.encode 0 input

                                Nothing ->
                                    ""

                        toolElement =
                            div [ class "tool-use" ]
                                [ text ("[" ++ toolName ++ "] ")
                                , Html.code [] [ text inputDisplay ]
                                ]
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

                        inputDisplay =
                            case toolUse.input of
                                Just input ->
                                    -- Special handling for MultiEdit with potentially large edits array
                                    if toolName == "MultiEdit" then
                                        case Decode.decodeValue (Decode.field "edits" (Decode.list Decode.value)) input of
                                            Ok edits ->
                                                let
                                                    editCount =
                                                        List.length edits

                                                    truncatedInput =
                                                        case Decode.decodeValue (Decode.dict Decode.value) input of
                                                            Ok dict ->
                                                                Dict.insert "edits" (Encode.string ("[" ++ String.fromInt editCount ++ " edits]")) dict
                                                                    |> Encode.dict identity identity
                                                                    |> Encode.encode 0

                                                            Err _ ->
                                                                Encode.encode 0 input
                                                in
                                                truncatedInput

                                            Err _ ->
                                                Encode.encode 0 input

                                    else
                                        Encode.encode 0 input

                                Nothing ->
                                    ""

                        summaryContent =
                            "[" ++ toolName ++ "] "

                        toolElement =
                            details [ class "tool-result" ]
                                [ summary []
                                    [ text summaryContent
                                    , Html.code [] [ text inputDisplay ]
                                    ]
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

                ChatPermissionRequest toolName errorMessage _ ->
                    -- Render permission request as a notice
                    let
                        elementsWithAccumulated =
                            renderAccumulatedContent state

                        permissionElement =
                            div [ class "permission-notice" ]
                                [ div [ class "permission-notice-header" ]
                                    [ text ("⚠️ Permission required for: " ++ toolName) ]
                                , div [ class "permission-notice-body" ]
                                    [ text errorMessage ]
                                ]
                    in
                    { state
                        | accumulatedContent = ""
                        , elements = elementsWithAccumulated ++ [ permissionElement ]
                    }

        finalState =
            List.foldl renderItem initialState items
    in
    renderAccumulatedContent finalState
