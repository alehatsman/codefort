package storage

import (
	"strings"
	"testing"
)

func TestHashTokenStable(t *testing.T) {
	// Same input → same hash. Different input → different hash.
	a := HashToken("cf_test_abc")
	b := HashToken("cf_test_abc")
	c := HashToken("cf_test_abd")
	if a != b {
		t.Errorf("hash of identical input differs: %q vs %q", a, b)
	}
	if a == c {
		t.Errorf("hash collision for distinct inputs: %q vs %q", a, c)
	}
	if len(a) != 64 {
		t.Errorf("expected SHA-256 hex length 64, got %d", len(a))
	}
}

func TestGenerateTokenStringFormat(t *testing.T) {
	for range 5 {
		s, err := GenerateTokenString()
		if err != nil {
			t.Fatalf("GenerateTokenString: %v", err)
		}
		if !strings.HasPrefix(s, TokenPrefix) {
			t.Errorf("token %q missing prefix %q", s, TokenPrefix)
		}
		// prefix + 32 bytes hex-encoded = prefix + 64 chars
		want := len(TokenPrefix) + 64
		if len(s) != want {
			t.Errorf("token length %d, want %d (%q)", len(s), want, s)
		}
	}
}

func TestGenerateTokenStringEntropy(t *testing.T) {
	// 5 generations should all differ.
	seen := map[string]bool{}
	for range 5 {
		s, err := GenerateTokenString()
		if err != nil {
			t.Fatalf("GenerateTokenString: %v", err)
		}
		if seen[s] {
			t.Errorf("collision in generated tokens: %q seen twice", s)
		}
		seen[s] = true
	}
}
