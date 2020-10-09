package main

import (
	"testing"
)

func TestParseVersion(t *testing.T) {
	tests := []*struct {
		ver   string
		major int
		minor int
		patch int
	}{
		{
			ver:   "Google Chrome 1.2.3.4",
			major: 1,
			minor: 2,
			patch: 3,
		},
		{
			ver:   "88.0.4297.0",
			major: 88,
			minor: 0,
			patch: 4297,
		},
	}

	for _, tt := range tests {
		major, minor, patch, found := parseVersion(tt.ver)
		if !found {
			t.Fatalf("failed to parse version from %q", tt.ver)
		}
		if major != tt.major {
			t.Fatalf("wrong major version, wanted %d, got %d", tt.major, major)
		}
		if minor != tt.minor {
			t.Fatalf("wrong minor version, wanted %d, got %d", tt.minor, minor)
		}
		if patch != tt.patch {
			t.Fatalf("wrong patch version, wanted %d, got %d", tt.patch, patch)
		}
	}
}
