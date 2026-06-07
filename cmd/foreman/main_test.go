package main

import "testing"

func TestVersionExists(t *testing.T) {
	if version == "" {
		t.Fatal("version is empty")
	}
}
