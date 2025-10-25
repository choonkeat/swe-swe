port module GenerateTestData exposing (main)

{-| This module generates JSON test data that Go can use to validate
that it can decode Elm-generated JSON correctly.
-}

import Json.Encode as Encode
import Platform


port output : String -> Cmd msg


type Msg
    = NoOp


main : Program () () Msg
main =
    Platform.worker
        { init = \_ -> ( (), output (generateTestData |> Encode.encode 2) )
        , update = \_ model -> ( model, Cmd.none )
        , subscriptions = \_ -> Sub.none
        }


generateTestData : Encode.Value
generateTestData =
    Encode.object
        [ ( "generated_by", Encode.string "Elm GenerateTestData.elm" )
        , ( "test_cases"
          , Encode.list identity
                [ -- ChatItem test cases
                  testCase "ChatItem"
                    "ChatItem with user type"
                    (Encode.object
                        [ ( "type", Encode.string "user" )
                        , ( "sender", Encode.string "USER" )
                        ]
                    )
                , testCase "ChatItem"
                    "ChatItem with content"
                    (Encode.object
                        [ ( "type", Encode.string "content" )
                        , ( "content", Encode.string "Test content from Elm" )
                        ]
                    )
                , testCase "ChatItem"
                    "ChatItem with permission request"
                    (Encode.object
                        [ ( "type", Encode.string "permission_request" )
                        , ( "sender", Encode.string "Write" )
                        , ( "content", Encode.string "Permission needed" )
                        , ( "toolInput", Encode.string "{\"path\":\"/test.elm\"}" )
                        ]
                    )

                -- ClientMessage test cases
                , testCase "ClientMessage"
                    "ClientMessage basic"
                    (Encode.object
                        [ ( "type", Encode.string "message" )
                        , ( "sender", Encode.string "USER" )
                        , ( "content", Encode.string "Hello from Elm" )
                        ]
                    )
                , testCase "ClientMessage"
                    "ClientMessage with sessions"
                    (Encode.object
                        [ ( "type", Encode.string "message" )
                        , ( "sender", Encode.string "USER" )
                        , ( "content", Encode.string "Test" )
                        , ( "sessionID", Encode.string "elm-browser-123" )
                        , ( "claudeSessionID", Encode.string "elm-claude-456" )
                        ]
                    )
                , testCase "ClientMessage"
                    "ClientMessage permission response"
                    (Encode.object
                        [ ( "type", Encode.string "permission_response" )
                        , ( "allowedTools", Encode.list Encode.string [ "Read", "Grep", "LS" ] )
                        , ( "skipPermissions", Encode.bool False )
                        ]
                    )
                , testCase "ClientMessage"
                    "ClientMessage fuzzy search"
                    (Encode.object
                        [ ( "type", Encode.string "fuzzy_search" )
                        , ( "query", Encode.string "*.elm" )
                        , ( "maxResults", Encode.int 25 )
                        ]
                    )

                -- ToolUseInfo equivalent
                , testCase "ToolUseInfo"
                    "ToolUseInfo for Write"
                    (Encode.object
                        [ ( "name", Encode.string "Write" )
                        , ( "input", Encode.string "{\"file_path\":\"/src/Main.elm\",\"content\":\"module Main\"}" )
                        ]
                    )
                , testCase "ToolUseInfo"
                    "ToolUseInfo for TodoWrite"
                    (Encode.object
                        [ ( "name", Encode.string "TodoWrite" )
                        , ( "input"
                          , Encode.string
                                "{\"todos\":[{\"content\":\"Elm task\",\"status\":\"pending\",\"activeForm\":\"Working on Elm task\",\"id\":\"elm-1\",\"priority\":\"high\"}]}"
                          )
                        ]
                    )

                -- ClaudeMessage test cases (complex nested structure)
                , testCase "ClaudeMessage"
                    "ClaudeMessage with text content"
                    (Encode.object
                        [ ( "type", Encode.string "assistant" )
                        , ( "message"
                          , Encode.object
                                [ ( "role", Encode.string "assistant" )
                                , ( "content"
                                  , Encode.list identity
                                        [ Encode.object
                                            [ ( "type", Encode.string "text" )
                                            , ( "text", Encode.string "Response from Elm test" )
                                            ]
                                        ]
                                  )
                                ]
                          )
                        ]
                    )
                , testCase "ClaudeMessage"
                    "ClaudeMessage with tool use"
                    (Encode.object
                        [ ( "type", Encode.string "assistant" )
                        , ( "message"
                          , Encode.object
                                [ ( "role", Encode.string "assistant" )
                                , ( "content"
                                  , Encode.list identity
                                        [ Encode.object
                                            [ ( "type", Encode.string "tool_use" )
                                            , ( "name", Encode.string "Edit" )
                                            , ( "id", Encode.string "elm-tool-123" )
                                            , ( "input"
                                              , Encode.object
                                                    [ ( "file_path", Encode.string "/test.elm" )
                                                    , ( "old_string", Encode.string "foo" )
                                                    , ( "new_string", Encode.string "bar" )
                                                    ]
                                              )
                                            ]
                                        ]
                                  )
                                ]
                          )
                        ]
                    )
                , testCase "ClaudeMessage"
                    "ClaudeMessage with tool result"
                    (Encode.object
                        [ ( "type", Encode.string "user" )
                        , ( "message"
                          , Encode.object
                                [ ( "role", Encode.string "user" )
                                , ( "content"
                                  , Encode.list identity
                                        [ Encode.object
                                            [ ( "type", Encode.string "tool_result" )
                                            , ( "tool_use_id", Encode.string "elm-tool-123" )
                                            , ( "content", Encode.string "Edit successful" )
                                            , ( "is_error", Encode.bool False )
                                            ]
                                        ]
                                  )
                                ]
                          )
                        ]
                    )
                ]
          )
        ]


testCase : String -> String -> Encode.Value -> Encode.Value
testCase type_ description json =
    Encode.object
        [ ( "type", Encode.string type_ )
        , ( "description", Encode.string description )
        , ( "json", json )
        ]