package main

import (
	"slices"
	"testing"
)

func TestUnauthenticatedProviderCredentialNamesOnlyLoadedKeys(t *testing.T) {
	t.Setenv("XROUTER_TEST_PROVIDER_KEY", "loaded")
	t.Setenv("XROUTER_TEST_EMPTY_PROVIDER_KEY", "")

	cfg := Config{
		Providers: map[string]ProviderConfig{
			"empty":  {APIKeyEnv: "XROUTER_TEST_EMPTY_PROVIDER_KEY"},
			"inline": {APIKey: "inline"},
			"loaded": {APIKeyEnv: "XROUTER_TEST_PROVIDER_KEY"},
			"none":   {},
		},
	}

	got := unauthenticatedProviderCredentialNames(cfg)
	want := []string{"inline", "loaded"}
	if !slices.Equal(got, want) {
		t.Fatalf("unexpected provider credential names: got %v want %v", got, want)
	}
}
