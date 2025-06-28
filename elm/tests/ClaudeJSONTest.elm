module ClaudeJSONTest exposing (suite)

import Expect
import Json.Decode as Decode
import Main exposing (..)
import Test exposing (..)


suite : Test
suite =
    describe "Claude JSON parsing"
        [ describe "claudeMessageDecoder"
            [ test "decodes assistant message with text content" <|
                \_ ->
                    let
                        json =
                            """
                            {
                                "type": "assistant",
                                "message": {
                                    "role": "assistant",
                                    "content": [
                                        {
                                            "type": "text",
                                            "text": "Hello! How can I help you?"
                                        }
                                    ]
                                }
                            }
                            """

                        expected =
                            Ok
                                { type_ = "assistant"
                                , subtype = Nothing
                                , durationMs = Nothing
                                , result = Nothing
                                , message =
                                    Just
                                        { role = Just "assistant"
                                        , content =
                                            [ { type_ = "text"
                                              , text = Just "Hello! How can I help you?"
                                              , name = Nothing
                                              , input = Nothing
                                              , content = Nothing
                                              }
                                            ]
                                        }
                                }
                    in
                    Decode.decodeString claudeMessageDecoder json
                        |> Expect.equal expected
            , test "decodes assistant message with tool use" <|
                \_ ->
                    let
                        json =
                            """
                            {
                                "type": "assistant",
                                "message": {
                                    "role": "assistant",
                                    "content": [
                                        {
                                            "type": "tool_use",
                                            "name": "Read",
                                            "input": {
                                                "file_path": "/test/file.txt",
                                                "description": "Read a test file"
                                            }
                                        }
                                    ]
                                }
                            }
                            """

                        result =
                            Decode.decodeString claudeMessageDecoder json
                    in
                    case result of
                        Ok msg ->
                            case msg.message of
                                Just messageContent ->
                                    case List.head messageContent.content of
                                        Just content ->
                                            Expect.all
                                                [ \_ -> content.type_ |> Expect.equal "tool_use"
                                                , \_ -> content.name |> Expect.equal (Just "Read")
                                                , \_ -> content.input |> Expect.notEqual Nothing
                                                ]
                                                ()

                                        Nothing ->
                                            Expect.fail "No content in message"

                                Nothing ->
                                    Expect.fail "No message field"

                        Err err ->
                            Expect.fail (Decode.errorToString err)
            , test "decodes result message" <|
                \_ ->
                    let
                        json =
                            """
                            {
                                "type": "result",
                                "subtype": "success",
                                "duration_ms": 1234,
                                "result": "Task completed successfully"
                            }
                            """

                        result =
                            Decode.decodeString claudeMessageDecoder json
                    in
                    case result of
                        Ok msg ->
                            Expect.all
                                [ \_ -> msg.type_ |> Expect.equal "result"
                                , \_ -> msg.subtype |> Expect.equal (Just "success")
                                , \_ -> msg.durationMs |> Expect.equal (Just 1234)
                                , \_ -> msg.result |> Expect.equal (Just "Task completed successfully")
                                ]
                                ()

                        Err err ->
                            Expect.fail (Decode.errorToString err)
            , test "decodes user message with tool result" <|
                \_ ->
                    let
                        json =
                            """
                            {
                                "type": "user",
                                "message": {
                                    "role": "user",
                                    "content": [
                                        {
                                            "type": "tool_result",
                                            "content": "File contents here"
                                        }
                                    ]
                                }
                            }
                            """

                        result =
                            Decode.decodeString claudeMessageDecoder json
                    in
                    case result of
                        Ok msg ->
                            case msg.message of
                                Just messageContent ->
                                    case List.head messageContent.content of
                                        Just content ->
                                            Expect.all
                                                [ \_ -> content.type_ |> Expect.equal "tool_result"
                                                , \_ -> content.content |> Expect.equal (Just "File contents here")
                                                ]
                                                ()

                                        Nothing ->
                                            Expect.fail "No content in message"

                                Nothing ->
                                    Expect.fail "No message field"

                        Err err ->
                            Expect.fail (Decode.errorToString err)
            ]
        , describe "parseClaudeMessage"
            [ test "parses assistant text message into ChatContent" <|
                \_ ->
                    let
                        claudeMsg =
                            { type_ = "assistant"
                            , subtype = Nothing
                            , durationMs = Nothing
                            , result = Nothing
                            , message =
                                Just
                                    { role = Just "assistant"
                                    , content =
                                        [ { type_ = "text"
                                          , text = Just "Hello world!"
                                          , name = Nothing
                                          , input = Nothing
                                          , content = Nothing
                                          }
                                        ]
                                    }
                            }

                        expected =
                            [ ChatContent "Hello world!" ]
                    in
                    parseClaudeMessage claudeMsg
                        |> Expect.equal expected
            , test "parses tool use message with command and description" <|
                \_ ->
                    let
                        inputJson =
                            Decode.decodeString Decode.value
                                """{"command": "ls -la", "description": "List files"}"""
                                |> Result.toMaybe

                        claudeMsg =
                            { type_ = "assistant"
                            , subtype = Nothing
                            , durationMs = Nothing
                            , result = Nothing
                            , message =
                                Just
                                    { role = Just "assistant"
                                    , content =
                                        [ { type_ = "tool_use"
                                          , text = Nothing
                                          , name = Just "Bash"
                                          , input = inputJson
                                          , content = Nothing
                                          }
                                        ]
                                    }
                            }

                        result =
                            parseClaudeMessage claudeMsg
                    in
                    case result of
                        [ ChatContent content ] ->
                            Expect.all
                                [ \_ -> content |> String.contains "[Using tool: Bash]" |> Expect.equal True
                                , \_ -> content |> String.contains "Command: ls -la" |> Expect.equal True
                                , \_ -> content |> String.contains "Description: List files" |> Expect.equal True
                                ]
                                ()

                        _ ->
                            Expect.fail "Expected single ChatContent item"
            , test "parses result message with formatting" <|
                \_ ->
                    let
                        claudeMsg =
                            { type_ = "result"
                            , subtype = Just "success"
                            , durationMs = Just 5000
                            , result = Nothing
                            , message = Nothing
                            }

                        result =
                            parseClaudeMessage claudeMsg
                    in
                    case result of
                        [ ChatContent content ] ->
                            Expect.all
                                [ \_ -> content |> String.contains "✓ Task completed (success)" |> Expect.equal True
                                , \_ -> content |> String.contains "Duration: 5000ms" |> Expect.equal True
                                , \_ -> content |> String.contains "━━━" |> Expect.equal True
                                ]
                                ()

                        _ ->
                            Expect.fail "Expected single ChatContent item"
            , test "parses user tool result message" <|
                \_ ->
                    let
                        claudeMsg =
                            { type_ = "user"
                            , subtype = Nothing
                            , durationMs = Nothing
                            , result = Nothing
                            , message =
                                Just
                                    { role = Just "user"
                                    , content =
                                        [ { type_ = "tool_result"
                                          , text = Nothing
                                          , name = Nothing
                                          , input = Nothing
                                          , content = Just "Command output here"
                                          }
                                        ]
                                    }
                            }

                        expected =
                            [ ChatToolResult "Command output here\n" ]
                    in
                    parseClaudeMessage claudeMsg
                        |> Expect.equal expected
            , test "handles multiple content items" <|
                \_ ->
                    let
                        claudeMsg =
                            { type_ = "assistant"
                            , subtype = Nothing
                            , durationMs = Nothing
                            , result = Nothing
                            , message =
                                Just
                                    { role = Just "assistant"
                                    , content =
                                        [ { type_ = "text"
                                          , text = Just "First part"
                                          , name = Nothing
                                          , input = Nothing
                                          , content = Nothing
                                          }
                                        , { type_ = "text"
                                          , text = Just "Second part"
                                          , name = Nothing
                                          , input = Nothing
                                          , content = Nothing
                                          }
                                        ]
                                    }
                            }

                        expected =
                            [ ChatContent "First part", ChatContent "Second part" ]
                    in
                    parseClaudeMessage claudeMsg
                        |> Expect.equal expected
            ]
        ]