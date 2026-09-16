package buildverify

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectorFindsPackageAndMakeActions(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":{"build":"ng build","check":"npm test && npm run build","start":"ng serve","tauri:build:exe:update":"npm run version:bump && tauri build --no-bundle"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Makefile"), []byte("test:\n\tgo test ./...\ninstaller-update:\n\tpowershell ./build.ps1 --installer\nclean:\n\trm -rf dist\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	profile, err := (Detector{}).Detect(context.Background(), "p1", "Demo", root)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Status != "ready" {
		t.Fatalf("status=%s", profile.Status)
	}
	if len(profile.Actions) != 7 {
		t.Fatalf("actions=%d: %#v", len(profile.Actions), profile.Actions)
	}
	labels := map[string]bool{}
	for _, action := range profile.Actions {
		labels[action.Label] = true
	}
	for _, label := range []string{"npm run start", "npm run tauri:build:exe:update", "make installer-update", "make clean"} {
		if !labels[label] {
			t.Fatalf("missing %s in %#v", label, profile.Actions)
		}
	}
	if !profile.Actions[0].Recommended || profile.Actions[0].Label != "make installer-update" {
		t.Fatalf("recommended=%#v", profile.Actions[0])
	}
}

func TestDetectorExcludesGeneratedFolders(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "fake"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "fake", "package.json"), []byte(`{"scripts":{"build":"bad"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	profile, err := (Detector{}).Detect(context.Background(), "p1", "Demo", root)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Status != "not_configured" || len(profile.Actions) != 0 {
		t.Fatalf("profile=%#v", profile)
	}
}
