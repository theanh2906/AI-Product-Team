package web

import (
	"strings"
	"testing"
)

func TestWindowsFolderPickerUsesTopMostOwner(t *testing.T) {
	for _, expected := range []string{
		"$owner.TopMost = $true",
		"ShowDialog($owner)",
	} {
		if !strings.Contains(windowsFolderPickerScript, expected) {
			t.Fatalf("folder picker script must contain %q", expected)
		}
	}
}
