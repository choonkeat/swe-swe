module AnsiTest exposing (..)

import Ansi exposing (ansiToHtml, parseAnsiWithState, initAnsiState)
import Expect
import HtmlData exposing (Html(..), br, span, text, div)
import HtmlData.Attributes exposing (style)
import Test exposing (..)


suite : Test
suite =
    describe "Ansi.ansiToHtml"
        [ describe "Plain text handling"
            [ test "handles plain text with newline" <|
                \_ ->
                    let
                        input = "starting session | provider: anthropic model: claude-3-7-sonnet-20250219\n"
                        result = ansiToHtml input
                        expected = div [] 
                            [ text "starting session | provider: anthropic model: claude-3-7-sonnet-20250219"
                            , br [] []
                            ]
                    in
                    result |> Expect.equal expected

            , test "handles plain text command" <|
                \_ ->
                    let
                        input = "command: ls -la\n"
                        result = ansiToHtml input
                        expected = div []
                            [ text "command: ls -la"
                            , br [] []
                            ]
                    in
                    result |> Expect.equal expected

            , test "handles plain text without newline" <|
                \_ ->
                    let
                        input = "Welcome to the chat! Type something to start chatting."
                        result = ansiToHtml input
                        expected = div []
                            [ text "Welcome to the chat! Type something to start chatting."
                            ]
                    in
                    result |> Expect.equal expected

            , test "handles empty string" <|
                \_ ->
                    let
                        input = ""
                        result = ansiToHtml input
                        expected = div []
                            [ text ""
                            ]
                    in
                    result |> Expect.equal expected

            , test "handles multiple newlines" <|
                \_ ->
                    let
                        input = "line1\nline2\nline3"
                        result = ansiToHtml input
                        expected = div []
                            [ text "line1"
                            , br [] []
                            , text "line2"
                            , br [] []
                            , text "line3"
                            ]
                    in
                    result |> Expect.equal expected
            ]

        , describe "Carriage return handling"
            [ test "simple carriage return" <|
                \_ ->
                    let
                        input = "abc\rdef"
                        result = ansiToHtml input
                        expected = div []
                            [ text "def"
                            ]
                    in
                    result |> Expect.equal expected

            , test "handles carriage return overwrites" <|
                \_ ->
                    let
                        input = "hello\nworld\rthere"
                        result = ansiToHtml input
                        expected = div []
                            [ text "hello"
                            , br [] []
                            , text "there"
                            ]
                    in
                    result |> Expect.equal expected

            , test "handles carriage return with spaces" <|
                \_ ->
                    let
                        input = "hello\nworld                                \rthere"
                        result = ansiToHtml input
                        expected = div []
                            [ text "hello"
                            , br [] []
                            , text "there"
                            ]
                    in
                    result |> Expect.equal expected

            , test "handles multiple carriage returns" <|
                \_ ->
                    let
                        input = "abc\rdef\rghi"
                        result = ansiToHtml input
                        expected = div []
                            [ text "ghi"
                            ]
                    in
                    result |> Expect.equal expected

            , test "handles carriage return with ANSI codes" <|
                \_ ->
                    let
                        input = "\u{001B}[32mgreen\r\u{001B}[31mred"
                        result = ansiToHtml input
                        expected = div []
                            [ span [style "color" "rgb(194,54,33)"] [text "red"]
                            ]
                    in
                    result |> Expect.equal expected

            , test "handles progress indicator pattern" <|
                \_ ->
                    let
                        input = "diff --git a/elm/src/Ansi.elm b/elm/src/Ansi.elm\n                                                                                \r\u{001B}[32m⠋\u{001B}[0m diff --git"
                        result = ansiToHtml input
                        expected = div []
                            [ text "diff --git a/elm/src/Ansi.elm b/elm/src/Ansi.elm"
                            , br [] []
                            , span [style "color" "rgb(37,188,36)"] [text "⠋"]
                            , text " diff --git"
                            ]
                    in
                    result |> Expect.equal expected
            ]

        , describe "RGB color support (24-bit)"
            [ test "handles RGB colored text" <|
                \_ ->
                    let
                        input = "\u{001B}[38;2;222;222;222mSome colored text\u{001B}[0m\n"
                        result = ansiToHtml input
                        expected = div []
                            [ span [style "color" "rgb(222,222,222)"] [text "Some colored text"]
                            , br [] []
                            ]
                    in
                    result |> Expect.equal expected

            , test "simple RGB without newline" <|
                \_ ->
                    let
                        input = "\u{001B}[38;2;255;0;0mRed\u{001B}[0m"
                        result = ansiToHtml input
                        expected = div []
                            [ span [style "color" "rgb(255,0,0)"] [text "Red"]
                            ]
                    in
                    result |> Expect.equal expected

            , test "RGB in middle of string" <|
                \_ ->
                    let
                        input = "Start \u{001B}[38;2;0;255;0mgreen text\u{001B}[0m end"
                        result = ansiToHtml input
                        expected = div []
                            [ text "Start "
                            , span [style "color" "rgb(0,255,0)"] [text "green text"]
                            , text " end"
                            ]
                    in
                    result |> Expect.equal expected

            , test "multiple RGB sequences in one string" <|
                \_ ->
                    let
                        input = "\u{001B}[38;2;255;0;0mRed\u{001B}[0m and \u{001B}[38;2;0;0;255mBlue\u{001B}[0m"
                        result = ansiToHtml input
                        expected = div []
                            [ span [style "color" "rgb(255,0,0)"] [text "Red"]
                            , text " and "
                            , span [style "color" "rgb(0,0,255)"] [text "Blue"]
                            ]
                    in
                    result |> Expect.equal expected

            , test "RGB background color" <|
                \_ ->
                    let
                        input = "\u{001B}[48;2;255;255;0mYellow background\u{001B}[0m"
                        result = ansiToHtml input
                        expected = div []
                            [ span [style "background-color" "rgb(255,255,0)"] [text "Yellow background"]
                            ]
                    in
                    result |> Expect.equal expected
            ]

        , describe "Standard 16-color support"
            [ test "handles standard red color" <|
                \_ ->
                    let
                        input = "\u{001B}[31mRed text\u{001B}[0m"
                        result = ansiToHtml input
                        expected = div []
                            [ span [style "color" "rgb(194,54,33)"] [text "Red text"]
                            ]
                    in
                    result |> Expect.equal expected

            , test "handles bright colors" <|
                \_ ->
                    let
                        input = "\u{001B}[91mBright red\u{001B}[0m"
                        result = ansiToHtml input
                        expected = div []
                            [ span [style "color" "rgb(252,57,31)"] [text "Bright red"]
                            ]
                    in
                    result |> Expect.equal expected

            , test "handles background colors" <|
                \_ ->
                    let
                        input = "\u{001B}[41mRed background\u{001B}[0m"
                        result = ansiToHtml input
                        expected = div []
                            [ span [style "background-color" "rgb(194,54,33)"] [text "Red background"]
                            ]
                    in
                    result |> Expect.equal expected
            ]

        , describe "Text attributes"
            [ test "handles bold text" <|
                \_ ->
                    let
                        input = "\u{001B}[1mBold text\u{001B}[0m"
                        result = ansiToHtml input
                        expected = div []
                            [ span [style "font-weight" "bold"] [text "Bold text"]
                            ]
                    in
                    result |> Expect.equal expected

            , test "handles underline text" <|
                \_ ->
                    let
                        input = "\u{001B}[4mUnderlined text\u{001B}[0m"
                        result = ansiToHtml input
                        expected = div []
                            [ span [style "text-decoration" "underline"] [text "Underlined text"]
                            ]
                    in
                    result |> Expect.equal expected

            , test "handles italic text" <|
                \_ ->
                    let
                        input = "\u{001B}[3mItalic text\u{001B}[0m"
                        result = ansiToHtml input
                        expected = div []
                            [ span [style "font-style" "italic"] [text "Italic text"]
                            ]
                    in
                    result |> Expect.equal expected

            , test "handles combined attributes" <|
                \_ ->
                    let
                        input = "\u{001B}[1;31mBold red text\u{001B}[0m"
                        result = ansiToHtml input
                        expected = div []
                            [ span [style "color" "rgb(194,54,33)", style "font-weight" "bold"] [text "Bold red text"]
                            ]
                    in
                    result |> Expect.equal expected

            , test "handles multiple attributes with RGB" <|
                \_ ->
                    let
                        input = "\u{001B}[1;4;38;2;255;0;255mBold underlined magenta\u{001B}[0m"
                        result = ansiToHtml input
                        expected = div []
                            [ span [style "color" "rgb(255,0,255)", style "font-weight" "bold", style "text-decoration" "underline"] 
                                [text "Bold underlined magenta"]
                            ]
                    in
                    result |> Expect.equal expected
            ]

        , describe "Streaming tests"
            [ test "streaming: incomplete RGB sequence" <|
                \_ ->
                    let
                        input1 = "\u{001B}[38;2;222;222;222mSome colored"
                        result1 = parseAnsiWithState initAnsiState input1
                        expected1 = [ span [style "color" "rgb(222,222,222)"] [text "Some colored"] ]
                    in
                    result1.elements |> Expect.equal expected1

            , test "streaming: complete RGB sequence across two strings" <|
                \_ ->
                    let
                        input1 = "\u{001B}[38;2;222;222;222mSome colored"
                        result1 = parseAnsiWithState initAnsiState input1
                        
                        input2 = " text\u{001B}[0m\n"
                        result2 = parseAnsiWithState result1.newState input2
                        expected2 = [ span [style "color" "rgb(222,222,222)"] [text " text"], br [] [] ]
                    in
                    result2.elements |> Expect.equal expected2

            , test "streaming: RGB color continues to next string" <|
                \_ ->
                    let
                        input1 = "\u{001B}[38;2;255;0;0mRed text"
                        result1 = parseAnsiWithState initAnsiState input1
                        
                        input2 = " continues here\u{001B}[0m"
                        result2 = parseAnsiWithState result1.newState input2
                        
                        expected1 = [ span [style "color" "rgb(255,0,0)"] [text "Red text"] ]
                        expected2 = [ span [style "color" "rgb(255,0,0)"] [text " continues here"] ]
                    in
                    Expect.all
                        [ \_ -> result1.elements |> Expect.equal expected1
                        , \_ -> result2.elements |> Expect.equal expected2
                        ] ()

            , test "streaming: split escape sequence" <|
                \_ ->
                    let
                        input1 = "Text \u{001B}[38;2"
                        result1 = parseAnsiWithState initAnsiState input1
                        
                        input2 = ";255;0;0mRed\u{001B}[0m"
                        result2 = parseAnsiWithState result1.newState input2
                        
                        expected1 = [ text "Text " ]
                        expected2 = [ span [style "color" "rgb(255,0,0)"] [text "Red"] ]
                    in
                    Expect.all
                        [ \_ -> result1.elements |> Expect.equal expected1
                        , \_ -> result2.elements |> Expect.equal expected2
                        ] ()
            ]

        , describe "Edge cases"
            [ test "handles unknown ANSI codes gracefully" <|
                \_ ->
                    let
                        input = "text \u{001B}[999m more text"
                        result = ansiToHtml input
                        expected = div []
                            [ text "text "
                            , text " more text"
                            ]
                    in
                    result |> Expect.equal expected

            , test "handles malformed RGB sequence" <|
                \_ ->
                    let
                        input = "\u{001B}[38;2;255mIncomplete RGB\u{001B}[0m"
                        result = ansiToHtml input
                        expected = div []
                            [ text "Incomplete RGB"
                            ]
                    in
                    result |> Expect.equal expected

            , test "handles empty reset sequence" <|
                \_ ->
                    let
                        input = "text\u{001B}[m more text"
                        result = ansiToHtml input
                        expected = div []
                            [ text "text"
                            , text " more text"
                            ]
                    in
                    result |> Expect.equal expected

            , test "handles consecutive resets" <|
                \_ ->
                    let
                        input = "\u{001B}[0m\u{001B}[0mtext"
                        result = ansiToHtml input
                        expected = div []
                            [ text "text"
                            ]
                    in
                    result |> Expect.equal expected

            , test "handles 256-color palette" <|
                \_ ->
                    let
                        input = "\u{001B}[38;5;196mColor 196\u{001B}[0m"
                        result = ansiToHtml input
                        expected = div []
                            [ span [style "color" "rgb(255,0,0)"] [text "Color 196"]
                            ]
                    in
                    result |> Expect.equal expected
            ]

        , describe "Complex scenarios"
            [ test "handles nested color changes" <|
                \_ ->
                    let
                        input = "\u{001B}[31mRed \u{001B}[32mGreen \u{001B}[34mBlue\u{001B}[0m"
                        result = ansiToHtml input
                        expected = div []
                            [ span [style "color" "rgb(194,54,33)"] [text "Red "]
                            , span [style "color" "rgb(37,188,36)"] [text "Green "]
                            , span [style "color" "rgb(73,46,225)"] [text "Blue"]
                            ]
                    in
                    result |> Expect.equal expected

            , test "handles color with newlines" <|
                \_ ->
                    let
                        input = "\u{001B}[31mRed\ntext\nacross\nlines\u{001B}[0m"
                        result = ansiToHtml input
                        expected = div []
                            [ span [style "color" "rgb(194,54,33)"] [text "Red"]
                            , br [] []
                            , span [style "color" "rgb(194,54,33)"] [text "text"]
                            , br [] []
                            , span [style "color" "rgb(194,54,33)"] [text "across"]
                            , br [] []
                            , span [style "color" "rgb(194,54,33)"] [text "lines"]
                            ]
                    in
                    result |> Expect.equal expected

            , test "preserves attributes across color changes" <|
                \_ ->
                    let
                        input = "\u{001B}[1mBold \u{001B}[31mred bold\u{001B}[0m normal"
                        result = ansiToHtml input
                        expected = div []
                            [ span [style "font-weight" "bold"] [text "Bold "]
                            , span [style "color" "rgb(194,54,33)", style "font-weight" "bold"] [text "red bold"]
                            , text " normal"
                            ]
                    in
                    result |> Expect.equal expected
            ]

        , describe "Code block formatting"
            [ test "handles code block with content before and after" <|
                \_ ->
                    let
                        input = "Text before```python\ndef hello():\n    print('Hello')\n```Text after"
                        result = ansiToHtml input
                        -- The bug is that formatCodeBlock only returns the code block content,
                        -- discarding "Text before" and "Text after"
                    in
                    case result of
                        Element "div" _ children ->
                            -- Just check that we have content before and after the code block
                            case children of
                                (Text before) :: (Element "pre" _ _) :: (Text after) :: _ ->
                                    Expect.all
                                        [ \_ -> before |> Expect.equal "Text before"
                                        , \_ -> after |> Expect.equal "Text after"
                                        ] ()
                                _ ->
                                    Expect.fail "Expected text before, pre element, and text after"
                        _ ->
                            Expect.fail "Expected div element"
            
            , test "handles multiple code blocks" <|
                \_ ->
                    let
                        input = "First text```js\ncode1()```Middle text```py\ncode2()```End text"
                        result = ansiToHtml input
                    in
                    case result of
                        Element "div" _ children ->
                            case children of
                                (Text t1) :: (Element "pre" _ _) :: (Text t2) :: (Element "pre" _ _) :: (Text t3) :: _ ->
                                    Expect.all
                                        [ \_ -> t1 |> Expect.equal "First text"
                                        , \_ -> t2 |> Expect.equal "Middle text"
                                        , \_ -> t3 |> Expect.equal "End text"
                                        ] ()
                                _ ->
                                    Expect.fail "Expected alternating text and pre elements"
                        _ ->
                            Expect.fail "Expected div element"
            ]
        
        , describe "ANSI detection"
            [ test "detects ANSI codes correctly" <|
                \_ ->
                    let
                        plainInput = "plain text"
                        ansiInput = "\u{001B}[38;2;222;222;222mcolored\u{001B}[0m"
                        plainResult = ansiToHtml plainInput
                        ansiResult = ansiToHtml ansiInput
                        expectedPlain = div [] [text "plain text"]
                        expectedAnsi = div [] [span [style "color" "rgb(222,222,222)"] [text "colored"]]
                    in
                    Expect.all
                        [ \_ -> plainResult |> Expect.equal expectedPlain
                        , \_ -> ansiResult |> Expect.equal expectedAnsi
                        ] ()

            , test "debug escape character detection" <|
                \_ ->
                    let
                        esc = String.fromChar (Char.fromCode 27)  -- ASCII 27 = ESC
                        input = esc ++ "[38;2;255;0;0mTest" ++ esc ++ "[0m"
                        result = ansiToHtml input
                        expected = div []
                            [ span [style "color" "rgb(255,0,0)"] [text "Test"]
                            ]
                    in
                    result |> Expect.equal expected
            ]
        ]