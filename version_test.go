package main

import (
	"strings"
	"testing"
)

func TestVersionString(t *testing.T) {
	got := versionString()
	for _, want := range []string{"xrouter", version, commit} {
		if !strings.Contains(got, want) {
			t.Fatalf("version string %q missing %q", got, want)
		}
	}
}
