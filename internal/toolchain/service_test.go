package toolchain

import "testing"

func TestParseVersions(t *testing.T) {
	tests := []struct {
		parse    func(string) string
		input    string
		expected string
	}{
		{parseCodexVersion, "codex-cli 0.142.5", "0.142.5"},
		{parseClaudeVersion, "claude-code 2.1.224", "2.1.224"},
		{parseCopilotVersion, "GitHub Copilot CLI 1.0.70", "1.0.70"},
		{parseGHVersion, "gh version 2.89.0 (2026-03-26)", "2.89.0"},
		{parseGitVersion, "git version 2.55.0.windows.3", "2.55.0.3"},
		{parseNodeVersion, "v22.22.1", "22.22.1"},
		{parseGoVersion, "go version go1.26.5 windows/amd64", "1.26.5"},
	}
	for _, test := range tests {
		if actual := test.parse(test.input); actual != test.expected {
			t.Fatalf("parse %q: expected %q, got %q", test.input, test.expected, actual)
		}
	}
}

func TestParseWingetVersion(t *testing.T) {
	actual := parseWingetVersion("Found GitHub CLI [GitHub.cli]\r\nVersion: 2.97.0\r\nPublisher: GitHub")
	if actual != "2.97.0" {
		t.Fatalf("expected 2.97.0, got %q", actual)
	}
}

func TestIsNewer(t *testing.T) {
	for _, test := range []struct {
		latest, installed string
		expected          bool
	}{
		{"2.97.0", "2.89.0", true},
		{"2.55.0.3", "2.55.0.windows.3", false},
		{"1.26.5", "1.26.5", false},
		{"0.146.1", "0.142.5", true},
	} {
		if actual := isNewer(test.latest, test.installed); actual != test.expected {
			t.Fatalf("isNewer(%q, %q): expected %v, got %v", test.latest, test.installed, test.expected, actual)
		}
	}
}
