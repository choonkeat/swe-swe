module Ansi exposing (AnsiState, ansiToElmHtml, ansiToHtml, initAnsiState, parseAnsiWithState)

import Html
import HtmlData exposing (Html(..), br, code, div, pre, span, text)
import HtmlData.Attributes exposing (class, style)
import HtmlData.Extra
import Regex



-- Types for ANSI parsing


type AnsiCode
    = Reset
    | Bold
    | Underline
    | Italic
    | Color16 Color16
    | Color256 Int
    | ColorRGB { r : Int, g : Int, b : Int }
    | BgColor16 Color16
    | BgColor256 Int
    | BgColorRGB { r : Int, g : Int, b : Int }


type Color16
    = Black
    | Red
    | Green
    | Yellow
    | Blue
    | Magenta
    | Cyan
    | White
    | BrightBlack
    | BrightRed
    | BrightGreen
    | BrightYellow
    | BrightBlue
    | BrightMagenta
    | BrightCyan
    | BrightWhite



-- Types for streaming ANSI parsing


type alias AnsiState =
    { currentStyle : StyleState
    , incompleteSequence : String
    }


type alias StyleState =
    { foregroundColor : Maybe String
    , backgroundColor : Maybe String
    , bold : Bool
    , underline : Bool
    , italic : Bool
    }


type alias ParseResult msg =
    { elements : List (HtmlData.Html msg)
    , newState : AnsiState
    }



-- Initialize ANSI state


initAnsiState : AnsiState
initAnsiState =
    { currentStyle = initStyleState
    , incompleteSequence = ""
    }


initStyleState : StyleState
initStyleState =
    { foregroundColor = Nothing
    , backgroundColor = Nothing
    , bold = False
    , underline = False
    , italic = False
    }



-- Parse ANSI text with state (for streaming)


parseAnsiWithState : AnsiState -> String -> ParseResult msg
parseAnsiWithState state input =
    let
        fullInput =
            state.incompleteSequence ++ input

        -- Preprocess input to handle carriage returns
        processedInput =
            processCarriageReturns fullInput

        result =
            parseAnsiStringStateful state processedInput

        elementsWithLineBreaks =
            handleLineBreaks result.elements
    in
    { result | elements = elementsWithLineBreaks }



-- Process carriage returns in input string
-- When we encounter \r, we discard everything from the last \n (or start) up to the \r


processCarriageReturns : String -> String
processCarriageReturns input =
    let
        -- Split by lines and process each line for carriage returns
        processLine : String -> String
        processLine line =
            let
                parts = String.split "\r" line
            in
            case List.reverse parts of
                [] ->
                    ""
                
                last :: _ ->
                    last
    in
    input
        |> String.split "\n"
        |> List.map processLine
        |> String.join "\n"


-- Internal function to parse ANSI string with current state


parseAnsiStringStateful : AnsiState -> String -> ParseResult msg
parseAnsiStringStateful state input =
    parseAnsiHelper state.currentStyle input [] ""



-- Recursive helper to parse ANSI sequences


parseAnsiHelper : StyleState -> String -> List (HtmlData.Html msg) -> String -> ParseResult msg
parseAnsiHelper currentStyle remaining acc buffer =
    case String.uncons remaining of
        Nothing ->
            let
                finalElements =
                    if String.isEmpty buffer then
                        List.reverse acc

                    else
                        List.reverse (createStyledElement currentStyle buffer :: acc)
            in
            { elements = finalElements
            , newState = { currentStyle = currentStyle, incompleteSequence = "" }
            }

        Just ( '\u{001B}', rest ) ->
            -- Start of escape sequence
            let
                elementsWithBuffer =
                    if String.isEmpty buffer then
                        acc

                    else
                        createStyledElement currentStyle buffer :: acc
            in
            case parseEscapeSequence rest of
                Ok ( codes, afterSequence ) ->
                    let
                        newStyle =
                            List.foldl applyAnsiCode currentStyle codes
                    in
                    parseAnsiHelper newStyle afterSequence elementsWithBuffer ""

                Err incompleteSeq ->
                    -- Incomplete sequence - save for next parse
                    { elements = List.reverse elementsWithBuffer
                    , newState =
                        { currentStyle = currentStyle
                        , incompleteSequence = "\u{001B}" ++ incompleteSeq
                        }
                    }

        Just ( char, rest ) ->
            -- Regular character
            parseAnsiHelper currentStyle rest acc (buffer ++ String.fromChar char)



-- Parse escape sequence and extract ANSI codes


parseEscapeSequence : String -> Result String ( List AnsiCode, String )
parseEscapeSequence input =
    if String.startsWith "[" input then
        let
            rest =
                String.dropLeft 1 input
        in
        parseCSI rest ""

    else
        -- Unknown escape sequence type - consume the escape char and continue
        Ok ( [], input )



-- Parse CSI (Control Sequence Introducer) sequences


parseCSI : String -> String -> Result String ( List AnsiCode, String )
parseCSI input buffer =
    case String.uncons input of
        Nothing ->
            -- Incomplete sequence
            Err ("[" ++ buffer)

        Just ( char, rest ) ->
            if char == 'm' then
                -- Complete SGR sequence
                let
                    codes =
                        parseSGRCodes buffer
                in
                Ok ( codes, rest )

            else if isCSIChar char then
                -- Continue parsing if we haven't exceeded reasonable length
                if String.length buffer > 20 then
                    -- Too long, probably invalid - consume and continue
                    Ok ( [], rest )

                else
                    parseCSI rest (buffer ++ String.fromChar char)

            else
                -- Invalid character in sequence - consume and continue
                Ok ( [], rest )



-- Check if character is valid in CSI sequence


isCSIChar : Char -> Bool
isCSIChar char =
    Char.isDigit char || char == ';'



-- Parse SGR (Select Graphic Rendition) codes


parseSGRCodes : String -> List AnsiCode
parseSGRCodes buffer =
    if String.isEmpty buffer then
        [ Reset ]

    else
    -- Handle complex sequences like "38;2;r;g;b"
    if
        String.contains "38;2;" buffer
            || String.contains "48;2;" buffer
            || String.contains "38;5;" buffer
            || String.contains "48;5;" buffer
    then
        parseComplexSGR buffer

    else
        String.split ";" buffer
            |> List.filterMap parseSingleSGR



-- Parse complex SGR codes (like RGB sequences)


parseComplexSGR : String -> List AnsiCode
parseComplexSGR buffer =
    let
        parts =
            String.split ";" buffer
    in
    parseComplexHelper parts []


parseComplexHelper : List String -> List AnsiCode -> List AnsiCode
parseComplexHelper parts acc =
    case parts of
        [] ->
            List.reverse acc

        "38" :: "2" :: r :: g :: b :: rest ->
            case ( String.toInt r, String.toInt g, String.toInt b ) of
                ( Just rVal, Just gVal, Just bVal ) ->
                    parseComplexHelper rest (ColorRGB { r = rVal, g = gVal, b = bVal } :: acc)

                _ ->
                    parseComplexHelper rest acc

        "48" :: "2" :: r :: g :: b :: rest ->
            case ( String.toInt r, String.toInt g, String.toInt b ) of
                ( Just rVal, Just gVal, Just bVal ) ->
                    parseComplexHelper rest (BgColorRGB { r = rVal, g = gVal, b = bVal } :: acc)

                _ ->
                    parseComplexHelper rest acc

        "38" :: "5" :: n :: rest ->
            case String.toInt n of
                Just nVal ->
                    parseComplexHelper rest (Color256 nVal :: acc)

                _ ->
                    parseComplexHelper rest acc

        "48" :: "5" :: n :: rest ->
            case String.toInt n of
                Just nVal ->
                    parseComplexHelper rest (BgColor256 nVal :: acc)

                _ ->
                    parseComplexHelper rest acc

        single :: rest ->
            case parseSingleSGR single of
                Just code ->
                    parseComplexHelper rest (code :: acc)

                Nothing ->
                    parseComplexHelper rest acc



-- Parse a single SGR parameter


parseSingleSGR : String -> Maybe AnsiCode
parseSingleSGR param =
    case String.toInt param of
        Just n ->
            case n of
                0 ->
                    Just Reset

                1 ->
                    Just Bold

                3 ->
                    Just Italic

                4 ->
                    Just Underline

                -- Foreground colors (30-37, 90-97)
                30 ->
                    Just (Color16 Black)

                31 ->
                    Just (Color16 Red)

                32 ->
                    Just (Color16 Green)

                33 ->
                    Just (Color16 Yellow)

                34 ->
                    Just (Color16 Blue)

                35 ->
                    Just (Color16 Magenta)

                36 ->
                    Just (Color16 Cyan)

                37 ->
                    Just (Color16 White)

                90 ->
                    Just (Color16 BrightBlack)

                91 ->
                    Just (Color16 BrightRed)

                92 ->
                    Just (Color16 BrightGreen)

                93 ->
                    Just (Color16 BrightYellow)

                94 ->
                    Just (Color16 BrightBlue)

                95 ->
                    Just (Color16 BrightMagenta)

                96 ->
                    Just (Color16 BrightCyan)

                97 ->
                    Just (Color16 BrightWhite)

                -- Background colors (40-47, 100-107)
                40 ->
                    Just (BgColor16 Black)

                41 ->
                    Just (BgColor16 Red)

                42 ->
                    Just (BgColor16 Green)

                43 ->
                    Just (BgColor16 Yellow)

                44 ->
                    Just (BgColor16 Blue)

                45 ->
                    Just (BgColor16 Magenta)

                46 ->
                    Just (BgColor16 Cyan)

                47 ->
                    Just (BgColor16 White)

                100 ->
                    Just (BgColor16 BrightBlack)

                101 ->
                    Just (BgColor16 BrightRed)

                102 ->
                    Just (BgColor16 BrightGreen)

                103 ->
                    Just (BgColor16 BrightYellow)

                104 ->
                    Just (BgColor16 BrightBlue)

                105 ->
                    Just (BgColor16 BrightMagenta)

                106 ->
                    Just (BgColor16 BrightCyan)

                107 ->
                    Just (BgColor16 BrightWhite)

                _ ->
                    Nothing

        Nothing ->
            Nothing



-- Parse RGB color values


parseRGBColor : String -> Maybe { r : Int, g : Int, b : Int }
parseRGBColor str =
    case String.split ";" str of
        [ rStr, gStr, bStr ] ->
            case ( String.toInt rStr, String.toInt gStr, String.toInt bStr ) of
                ( Just r, Just g, Just b ) ->
                    Just { r = r, g = g, b = b }

                _ ->
                    Nothing

        _ ->
            Nothing



-- Apply ANSI code to style state


applyAnsiCode : AnsiCode -> StyleState -> StyleState
applyAnsiCode code style =
    case code of
        Reset ->
            initStyleState

        Bold ->
            { style | bold = True }

        Underline ->
            { style | underline = True }

        Italic ->
            { style | italic = True }

        Color16 color ->
            { style | foregroundColor = Just (color16ToCss color) }

        Color256 n ->
            { style | foregroundColor = Just (color256ToCss n) }

        ColorRGB rgb ->
            { style | foregroundColor = Just (rgbToCss rgb) }

        BgColor16 color ->
            { style | backgroundColor = Just (color16ToCss color) }

        BgColor256 n ->
            { style | backgroundColor = Just (color256ToCss n) }

        BgColorRGB rgb ->
            { style | backgroundColor = Just (rgbToCss rgb) }



-- Convert color representations to CSS


color16ToCss : Color16 -> String
color16ToCss color =
    case color of
        Black ->
            "rgb(0,0,0)"

        Red ->
            "rgb(194,54,33)"

        Green ->
            "rgb(37,188,36)"

        Yellow ->
            "rgb(173,173,39)"

        Blue ->
            "rgb(73,46,225)"

        Magenta ->
            "rgb(211,56,211)"

        Cyan ->
            "rgb(51,187,200)"

        White ->
            "rgb(203,204,205)"

        BrightBlack ->
            "rgb(129,131,131)"

        BrightRed ->
            "rgb(252,57,31)"

        BrightGreen ->
            "rgb(49,231,34)"

        BrightYellow ->
            "rgb(234,236,35)"

        BrightBlue ->
            "rgb(88,51,255)"

        BrightMagenta ->
            "rgb(249,53,248)"

        BrightCyan ->
            "rgb(20,240,240)"

        BrightWhite ->
            "rgb(233,235,235)"


color256ToCss : Int -> String
color256ToCss n =
    -- Simplified 256-color palette conversion
    if n < 16 then
        -- Standard colors
        color16ToCss (indexToColor16 n)

    else if n < 232 then
        -- 216-color cube
        let
            idx =
                n - 16

            r =
                (idx // 36) * 51

            g =
                ((idx // 6) |> modBy 6) * 51

            b =
                (idx |> modBy 6) * 51
        in
        "rgb(" ++ String.fromInt r ++ "," ++ String.fromInt g ++ "," ++ String.fromInt b ++ ")"

    else
        -- Grayscale
        let
            gray =
                8 + (n - 232) * 10
        in
        "rgb(" ++ String.fromInt gray ++ "," ++ String.fromInt gray ++ "," ++ String.fromInt gray ++ ")"


indexToColor16 : Int -> Color16
indexToColor16 n =
    case n of
        0 ->
            Black

        1 ->
            Red

        2 ->
            Green

        3 ->
            Yellow

        4 ->
            Blue

        5 ->
            Magenta

        6 ->
            Cyan

        7 ->
            White

        8 ->
            BrightBlack

        9 ->
            BrightRed

        10 ->
            BrightGreen

        11 ->
            BrightYellow

        12 ->
            BrightBlue

        13 ->
            BrightMagenta

        14 ->
            BrightCyan

        _ ->
            BrightWhite


rgbToCss : { r : Int, g : Int, b : Int } -> String
rgbToCss rgb =
    "rgb(" ++ String.fromInt rgb.r ++ "," ++ String.fromInt rgb.g ++ "," ++ String.fromInt rgb.b ++ ")"



-- Create styled element based on current style


createStyledElement : StyleState -> String -> HtmlData.Html msg
createStyledElement styleState content =
    if String.isEmpty content then
        text ""

    else
        let
            styles =
                List.filterMap identity
                    [ Maybe.map (\c -> style "color" c) styleState.foregroundColor
                    , Maybe.map (\c -> style "background-color" c) styleState.backgroundColor
                    , if styleState.bold then
                        Just (style "font-weight" "bold")

                      else
                        Nothing
                    , if styleState.underline then
                        Just (style "text-decoration" "underline")

                      else
                        Nothing
                    , if styleState.italic then
                        Just (style "font-style" "italic")

                      else
                        Nothing
                    ]
        in
        if List.isEmpty styles then
            text content

        else
            span styles [ text content ]



-- Handle line breaks in ANSI-processed elements


handleLineBreaks : List (HtmlData.Html msg) -> List (HtmlData.Html msg)
handleLineBreaks elements =
    elements
        |> List.concatMap expandNewlines



-- Expand newlines within text and span elements


expandNewlines : HtmlData.Html msg -> List (HtmlData.Html msg)
expandNewlines element =
    case element of
        Text content ->
            if String.isEmpty content then
                []

            else if String.contains "\n" content then
                let
                    parts =
                        String.split "\n" content

                    endsWithNewline =
                        (List.reverse parts |> List.head) == Just ""

                    nonEmptyParts =
                        List.filter (not << String.isEmpty) parts

                    textElements =
                        List.map text nonEmptyParts

                    withLineBreaks =
                        List.intersperse (br [] []) textElements

                    finalResult =
                        if endsWithNewline then
                            withLineBreaks ++ [ br [] [] ]

                        else
                            withLineBreaks
                in
                finalResult

            else
                [ element ]

        Element "span" attrs children ->
            case children of
                [ Text content ] ->
                    if String.contains "\n" content then
                        let
                            parts =
                                String.split "\n" content

                            endsWithNewline =
                                (List.reverse parts |> List.head) == Just ""

                            filteredParts =
                                if endsWithNewline then
                                    List.reverse parts |> List.drop 1 |> List.reverse

                                else
                                    parts

                            withLineBreaks =
                                List.intersperse (br [] [])
                                    (List.map (\part -> span attrs [ text part ]) filteredParts)

                            finalResult =
                                if endsWithNewline then
                                    withLineBreaks ++ [ br [] [] ]

                                else
                                    withLineBreaks
                        in
                        finalResult

                    else
                        [ element ]

                _ ->
                    [ element ]

        _ ->
            [ element ]



-- Handle plain text (including newlines)


handlePlainText : String -> List (HtmlData.Html msg)
handlePlainText input =
    if String.contains "\n" input then
        let
            parts =
                String.split "\n" input

            -- Build elements handling empty strings as newlines
            buildElements : List String -> Bool -> Bool -> List (HtmlData.Html msg)
            buildElements partsList isFirst needsBreak =
                case partsList of
                    [] ->
                        []

                    "" :: rest ->
                        -- Empty string represents a newline
                        if needsBreak then
                            -- Already have a break, don't add another
                            buildElements rest False True

                        else
                            -- Need a break
                            br [] [] :: buildElements rest False True

                    part :: rest ->
                        -- Non-empty part
                        let
                            elem =
                                text part

                            result =
                                if needsBreak then
                                    br [] [] :: elem :: buildElements rest False False

                                else if isFirst then
                                    elem :: buildElements rest False False

                                else
                                    br [] [] :: elem :: buildElements rest False False
                        in
                        result
        in
        buildElements parts True False

    else
        [ text input ]



-- Format code blocks with syntax highlighting


formatCodeBlock : String -> List (HtmlData.Html msg)
formatCodeBlock input =
    let
        languagePattern =
            Regex.fromString "```([\\w]*)\\n([\\s\\S]*?)```"

        processMatches : List Regex.Match -> Int -> List (HtmlData.Html msg) -> List (HtmlData.Html msg)
        processMatches matches lastEnd acc =
            case matches of
                [] ->
                    -- Add any remaining text after the last match
                    if lastEnd < String.length input then
                        acc ++ handlePlainText (String.dropLeft lastEnd input)

                    else
                        acc

                match :: rest ->
                    let
                        -- Add text before this match
                        beforeText =
                            if match.index > lastEnd then
                                handlePlainText (String.slice lastEnd match.index input)

                            else
                                []

                        -- Extract language and content from submatches
                        ( language, content ) =
                            case match.submatches of
                                langMaybe :: contMaybe :: _ ->
                                    ( Maybe.withDefault "" langMaybe
                                    , Maybe.withDefault "" contMaybe
                                    )

                                _ ->
                                    ( "", "" )

                        -- Create the code block
                        codeBlock =
                            pre []
                                [ code [ class ("language-" ++ language) ]
                                    (let
                                        result =
                                            parseAnsiWithState initAnsiState content

                                        elementsWithLineBreaks =
                                            handleLineBreaks result.elements
                                     in
                                     elementsWithLineBreaks
                                    )
                                ]

                        newAcc =
                            acc ++ beforeText ++ [ codeBlock ]

                        newLastEnd =
                            match.index + String.length match.match
                    in
                    processMatches rest newLastEnd newAcc
    in
    case languagePattern of
        Just regex ->
            let
                matches =
                    Regex.find regex input
            in
            if List.isEmpty matches then
                -- No code blocks found, treat as plain text
                handlePlainText input

            else
                processMatches matches 0 []

        Nothing ->
            -- Regex compilation failed, treat as plain text
            handlePlainText input



-- ANSI to HTML conversion (returns HtmlData for testing)


ansiToHtml : String -> HtmlData.Html msg
ansiToHtml input =
    let
        -- Preprocess input to handle carriage returns
        processedInput =
            processCarriageReturns input

        -- Detect if this is a code block by looking for triple backticks
        codeBlockPattern =
            Regex.fromString "```[\\w]*\\n[\\s\\S]*?```"

        isCodeBlock =
            case codeBlockPattern of
                Just regex ->
                    Regex.contains regex processedInput

                Nothing ->
                    False

        -- Check if input contains ANSI escape sequences
        hasAnsiCodes =
            String.contains "\u{001B}[" processedInput
    in
    if isCodeBlock then
        -- Handle as a code block
        div [] (formatCodeBlock processedInput)

    else if not hasAnsiCodes then
        -- Handle as plain text without ANSI parsing
        div [] (handlePlainText processedInput)

    else
        -- Handle text with ANSI codes
        let
            result =
                parseAnsiWithState initAnsiState processedInput

            elementsWithLineBreaks =
                handleLineBreaks result.elements
        in
        div [] elementsWithLineBreaks



-- ANSI to HTML conversion (returns regular Html for use in views)


ansiToElmHtml : String -> List (Html.Html msg)
ansiToElmHtml input =
    ansiToHtml input
        |> HtmlData.Extra.toElmHtml
        |> List.singleton
