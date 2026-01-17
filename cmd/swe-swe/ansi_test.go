package main

import (
	"strings"
	"testing"
)

func TestTrueColorBg(t *testing.T) {
	tests := []struct {
		name    string
		r, g, b uint8
		want    string
	}{
		{"black", 0, 0, 0, "\x1b[48;2;0;0;0m"},
		{"white", 255, 255, 255, "\x1b[48;2;255;255;255m"},
		{"red", 255, 0, 0, "\x1b[48;2;255;0;0m"},
		{"our blue", 0, 122, 204, "\x1b[48;2;0;122;204m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TrueColorBg(tt.r, tt.g, tt.b)
			if got != tt.want {
				t.Errorf("TrueColorBg(%d, %d, %d) = %q, want %q",
					tt.r, tt.g, tt.b, got, tt.want)
			}
		})
	}
}

func TestTrueColorFg(t *testing.T) {
	tests := []struct {
		name    string
		r, g, b uint8
		want    string
	}{
		{"black", 0, 0, 0, "\x1b[38;2;0;0;0m"},
		{"white", 255, 255, 255, "\x1b[38;2;255;255;255m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TrueColorFg(tt.r, tt.g, tt.b)
			if got != tt.want {
				t.Errorf("TrueColorFg(%d, %d, %d) = %q, want %q",
					tt.r, tt.g, tt.b, got, tt.want)
			}
		})
	}
}

func TestAnsiReset(t *testing.T) {
	want := "\x1b[0m"
	got := AnsiReset()
	if got != want {
		t.Errorf("AnsiReset() = %q, want %q", got, want)
	}
}

func TestColorSwatch(t *testing.T) {
	tests := []struct {
		name      string
		cssColor  string
		label     string
		wantLabel bool // whether label should appear in output
		wantAnsi  bool // whether ANSI codes should appear
	}{
		{"valid hex", "#ff0000", "red", true, true},
		{"valid name", "blue", "blue", true, true},
		{"invalid color", "not-a-color", "unknown", true, false},
		{"empty color", "", "empty", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ColorSwatch(tt.cssColor, tt.label)
			if tt.wantLabel && !strings.Contains(got, tt.label) {
				t.Errorf("ColorSwatch(%q, %q) should contain label %q, got %q",
					tt.cssColor, tt.label, tt.label, got)
			}
			hasAnsi := strings.Contains(got, "\x1b[")
			if tt.wantAnsi && !hasAnsi {
				t.Errorf("ColorSwatch(%q, %q) should contain ANSI codes, got %q",
					tt.cssColor, tt.label, got)
			}
			if !tt.wantAnsi && hasAnsi {
				t.Errorf("ColorSwatch(%q, %q) should NOT contain ANSI codes, got %q",
					tt.cssColor, tt.label, got)
			}
		})
	}
}

func TestPresetColorsExist(t *testing.T) {
	// Verify all preset colors are valid CSS colors
	for _, preset := range PresetColors {
		t.Run(preset.Name, func(t *testing.T) {
			_, _, _, ok := ParseCSSColor(preset.Color)
			if !ok {
				t.Errorf("Preset color %q has invalid color %q", preset.Name, preset.Color)
			}
		})
	}

	// Verify we have at least the important presets
	expectedNames := []string{"blue", "red", "green", "orange"}
	for _, expected := range expectedNames {
		found := false
		for _, preset := range PresetColors {
			if preset.Name == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected preset color %q not found", expected)
		}
	}
}
