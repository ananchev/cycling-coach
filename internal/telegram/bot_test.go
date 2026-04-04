package telegram

import "testing"

func TestParseRPE(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"rpe 7", 7},
		{"RPE 8", 8},
		{"legs felt heavy, rpe 6, slept ok", 6},
		{"Rpe:9 hard session", 9},
		{"rpe 10", 10},
		{"rpe 1", 1},
		{"rpe=7 felt good", 7},
		{"no rating here", 0},
		{"", 0},
		{"rpe 0", 0},   // out of range
		{"rpe 11", 0},  // out of range
		{"rpe abc", 0},  // not a number
		{"torpedo 5", 0}, // "rpe" inside another word — still matches "rpe" substring
	}

	for _, tt := range tests {
		got := parseRPE(tt.input)
		if got != tt.want {
			t.Errorf("parseRPE(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}
