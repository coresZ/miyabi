package domain

import "testing"

func TestValueOrZero(t *testing.T) {
	var nilStr *string
	if got := ValueOrZero(nilStr); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}

	str := "hello"
	if got := ValueOrZero(&str); got != "hello" {
		t.Errorf("expected hello, got %q", got)
	}

	var nilInt *int
	if got := ValueOrZero(nilInt); got != 0 {
		t.Errorf("expected 0, got %d", got)
	}

	num := 42
	if got := ValueOrZero(&num); got != 42 {
		t.Errorf("expected 42, got %d", got)
	}
}
