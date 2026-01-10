package main

import "fmt"

// PresetColor represents a preset color for the status bar
type PresetColor struct {
	Name  string
	Color string
}

// PresetColors defines the available preset colors for --status-bar-color
var PresetColors = []PresetColor{
	{"blue", "#007acc"},   // default
	{"red", "#dc2626"},    // production danger
	{"green", "#16a34a"},  // safe/local
	{"orange", "#ea580c"}, // staging/warning
	{"purple", "#9333ea"}, // special env
	{"gray", "#4b5563"},   // neutral
}

// TrueColorBg returns an ANSI escape sequence for 24-bit background color
func TrueColorBg(r, g, b uint8) string {
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)
}

// TrueColorFg returns an ANSI escape sequence for 24-bit foreground color
func TrueColorFg(r, g, b uint8) string {
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)
}

// AnsiReset returns the ANSI reset escape sequence
func AnsiReset() string {
	return "\x1b[0m"
}

// ColorSwatch returns a colored block with a label for terminal display
func ColorSwatch(cssColor, label string) string {
	r, g, b, ok := ParseCSSColor(cssColor)
	if !ok {
		return label
	}
	return TrueColorBg(r, g, b) + "  " + AnsiReset() + " " + label
}

// PrintColorSwatches prints all preset color swatches to stdout
func PrintColorSwatches() {
	fmt.Println("Status bar color presets:")
	fmt.Println()
	for _, preset := range PresetColors {
		suffix := ""
		if preset.Name == "blue" {
			suffix = " (default)"
		}
		fmt.Printf("  %s%s\n", ColorSwatch(preset.Color, preset.Name), suffix)
	}
	fmt.Println()
	fmt.Println("Or use any CSS color: #ff5500, darkgreen, navy, etc.")
}
