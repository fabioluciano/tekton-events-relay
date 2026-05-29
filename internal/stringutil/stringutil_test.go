package stringutil

import "testing"

func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"shorter than limit", "hello", 10, "hello"},
		{"equal to limit", "hello", 5, "hello"},
		{"longer than limit", "hello world", 5, "hello"},
		{"empty string", "", 5, ""},
		{"zero limit", "hello", 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Truncate(tt.s, tt.n); got != tt.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
			}
		})
	}
}

func TestTruncateWithEllipsis(t *testing.T) {
	tests := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"shorter than limit", "hello", 10, "hello"},
		{"equal to limit", "hello", 5, "hello"},
		{"longer than limit", "hello world", 8, "hello..."},
		{"very short limit", "hello", 2, "he"},
		{"empty string", "", 5, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TruncateWithEllipsis(tt.s, tt.n); got != tt.want {
				t.Errorf("TruncateWithEllipsis(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
			}
		})
	}
}
