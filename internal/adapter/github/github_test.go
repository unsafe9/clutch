package github

import "testing"

func TestName(t *testing.T) {
	if got := New().Name(); got != "github" {
		t.Fatalf("Name() = %q, want github", got)
	}
}

func TestParseKey(t *testing.T) {
	tests := []struct {
		key, repo, number string
	}{
		{"123", "", "123"},
		{"#123", "", "123"},
		{"owner/repo#123", "owner/repo", "123"},
		{"owner/repo/123", "owner/repo", "123"},
	}
	for _, tt := range tests {
		repo, number := parseKey(tt.key)
		if repo != tt.repo || number != tt.number {
			t.Errorf("parseKey(%q) = (%q, %q), want (%q, %q)", tt.key, repo, number, tt.repo, tt.number)
		}
	}
}
