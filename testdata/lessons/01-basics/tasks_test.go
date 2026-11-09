package basics

import "testing"

func TestGreeting(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"Аня", "Привет, Аня!"},
		{"Борис", "Привет, Борис!"},
		{"мир", "Привет, мир!"},
	}
	for _, tt := range tests {
		if got := Greeting(tt.in); got != tt.want {
			t.Errorf("Greeting(%q) = %q, хотим %q", tt.in, got, tt.want)
		}
	}
}

func TestSum(t *testing.T) {
	if got := Sum(2, 3); got != 5 {
		t.Errorf("Sum(2, 3) = %d, хотим 5", got)
	}
	if got := Sum(-2, 2); got != 0 {
		t.Errorf("Sum(-2, 2) = %d, хотим 0", got)
	}
	// Отрицательные и нули — чтобы никто не «догадался» складывать через abs.
	if got := Sum(0, 0); got != 0 {
		t.Errorf("Sum(0, 0) = %d, хотим 0", got)
	}
}

func TestWordCount(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"   ", 0},
		{"раз", 1},
		{"раз два три", 3},
		{"раз   два\tтри\nчетыре", 4}, // пробелы, табы и переводы строк — всё разделители
	}
	for _, tt := range tests {
		if got := WordCount(tt.in); got != tt.want {
			t.Errorf("WordCount(%q) = %d, хотим %d", tt.in, got, tt.want)
		}
	}
}
