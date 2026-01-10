package main

import (
	"math"
	"testing"
)

func TestParseCSSColor(t *testing.T) {
	tests := []struct {
		name    string
		color   string
		wantR   uint8
		wantG   uint8
		wantB   uint8
		wantOK  bool
	}{
		// Hex colors - 6 digit
		{"hex 6 digit black", "#000000", 0, 0, 0, true},
		{"hex 6 digit white", "#ffffff", 255, 255, 255, true},
		{"hex 6 digit red", "#ff0000", 255, 0, 0, true},
		{"hex 6 digit green", "#00ff00", 0, 255, 0, true},
		{"hex 6 digit blue", "#0000ff", 0, 0, 255, true},
		{"hex 6 digit uppercase", "#FFFFFF", 255, 255, 255, true},
		{"hex 6 digit mixed case", "#FfFfFf", 255, 255, 255, true},
		{"our default blue", "#007acc", 0, 122, 204, true},
		{"red for production", "#dc2626", 220, 38, 38, true},

		// Hex colors - 3 digit
		{"hex 3 digit black", "#000", 0, 0, 0, true},
		{"hex 3 digit white", "#fff", 255, 255, 255, true},
		{"hex 3 digit red", "#f00", 255, 0, 0, true},
		{"hex 3 digit uppercase", "#FFF", 255, 255, 255, true},
		{"hex 3 digit abc", "#abc", 170, 187, 204, true},

		// Named colors
		{"named red", "red", 255, 0, 0, true},
		{"named blue", "blue", 0, 0, 255, true},
		{"named green", "green", 0, 128, 0, true}, // CSS green is #008000, not #00ff00
		{"named yellow", "yellow", 255, 255, 0, true},
		{"named darkgreen", "darkgreen", 0, 100, 0, true},
		{"named navy", "navy", 0, 0, 128, true},
		{"named case insensitive", "RED", 255, 0, 0, true},
		{"named mixed case", "DarkGreen", 0, 100, 0, true},
		{"named with whitespace", "  red  ", 255, 0, 0, true},

		// Invalid colors
		{"empty string", "", 0, 0, 0, false},
		{"no hash", "ff0000", 0, 0, 0, false},
		{"invalid hex char", "#gggggg", 0, 0, 0, false},
		{"too short", "#ff", 0, 0, 0, false},
		{"too long", "#fffffff", 0, 0, 0, false},
		{"unknown name", "not-a-color", 0, 0, 0, false},
		{"partial hex", "#12345", 0, 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, g, b, ok := ParseCSSColor(tt.color)
			if ok != tt.wantOK {
				t.Errorf("ParseCSSColor(%q) ok = %v, want %v", tt.color, ok, tt.wantOK)
			}
			if ok && (r != tt.wantR || g != tt.wantG || b != tt.wantB) {
				t.Errorf("ParseCSSColor(%q) = (%d, %d, %d), want (%d, %d, %d)",
					tt.color, r, g, b, tt.wantR, tt.wantG, tt.wantB)
			}
		})
	}
}

func TestRelativeLuminance(t *testing.T) {
	tests := []struct {
		name string
		r, g, b uint8
		want float64
	}{
		{"black", 0, 0, 0, 0.0},
		{"white", 255, 255, 255, 1.0},
		{"red", 255, 0, 0, 0.2126},
		{"green", 0, 255, 0, 0.7152},
		{"blue", 0, 0, 255, 0.0722},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RelativeLuminance(tt.r, tt.g, tt.b)
			if math.Abs(got-tt.want) > 0.001 {
				t.Errorf("RelativeLuminance(%d, %d, %d) = %f, want %f",
					tt.r, tt.g, tt.b, got, tt.want)
			}
		})
	}
}

func TestContrastingTextColor(t *testing.T) {
	tests := []struct {
		name    string
		bgColor string
		want    string
	}{
		// Dark backgrounds -> white text
		{"black hex", "#000000", "#fff"},
		{"our blue", "#007acc", "#fff"},
		{"red for production", "#dc2626", "#fff"},
		{"dark green", "darkgreen", "#fff"},
		{"navy", "navy", "#fff"},
		{"dark gray", "#333333", "#fff"},
		{"purple", "#9333ea", "#fff"},

		// Light backgrounds -> black text
		{"white hex", "#ffffff", "#000"},
		{"white name", "white", "#000"},
		{"yellow", "yellow", "#000"},
		{"light yellow", "#fbbf24", "#000"},
		{"light gray", "#cccccc", "#000"},
		{"lime", "lime", "#000"},
		{"aqua", "aqua", "#000"},
		{"light green", "lightgreen", "#000"},

		// Edge cases
		{"empty string", "", "#fff"},         // default to white
		{"invalid color", "not-a-color", "#fff"}, // default to white
		{"medium gray", "#808080", "#fff"},   // gray is typically dark enough
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ContrastingTextColor(tt.bgColor)
			if got != tt.want {
				t.Errorf("ContrastingTextColor(%q) = %q, want %q", tt.bgColor, got, tt.want)
			}
		})
	}
}

func TestNamedColorsExist(t *testing.T) {
	// Verify some important named colors exist in our map
	importantColors := []string{
		"red", "green", "blue", "yellow", "orange", "purple",
		"black", "white", "gray", "grey",
		"darkred", "darkgreen", "darkblue",
		"lightgray", "lightgrey",
		"navy", "teal", "maroon", "olive",
	}

	for _, name := range importantColors {
		t.Run(name, func(t *testing.T) {
			_, _, _, ok := ParseCSSColor(name)
			if !ok {
				t.Errorf("Named color %q should be parseable", name)
			}
		})
	}
}
