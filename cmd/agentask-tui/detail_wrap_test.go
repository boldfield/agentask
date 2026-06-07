package main

import (
	"strings"
	"testing"
)

func TestWrapText(t *testing.T) {
	in := "the quick brown fox jumps over the lazy dog"
	out := wrapText(in, 20)
	for _, line := range strings.Split(out, "\n") {
		if len(line) > 20 {
			t.Errorf("line %q exceeds width 20", line)
		}
	}
	if strings.Join(strings.Fields(out), " ") != in {
		t.Errorf("words not preserved across wrap: %q", out)
	}
	if got := wrapText("a\nb", 10); got != "a\nb" {
		t.Errorf("existing newlines must be preserved, got %q", got)
	}
	if got := wrapText("x y", 0); got != "x y" {
		t.Errorf("width<=0 should pass through, got %q", got)
	}
}
