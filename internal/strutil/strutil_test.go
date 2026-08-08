package strutil

import "testing"

func TestIsBlank(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"empty string", "", true},
		{"single space", " ", true},
		{"mixed whitespace", "\t\n ", true},
		{"non-blank", "x", false},
		{"whitespace-padded non-blank", " x ", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsBlank(tt.in); got != tt.want {
				t.Errorf("IsBlank(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
