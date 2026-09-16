package deploy

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectorFindsDeployLikeActionsAndLeavesSelectionEmpty(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":{"build":"ng build","deploy":"node scripts/deploy.js","deploy:prod":"node scripts/deploy.js --prod","start":"ng serve"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Makefile"), []byte("test:\n\tgo test ./...\ndeploy:\n\t./deploy.sh\nclean:\n\trm -rf dist\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	profile, err := (Detector{}).Detect(context.Background(), "p1", "Demo", root)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Status != StatusNotConfigured {
		t.Fatalf("status=%s", profile.Status)
	}
	if profile.SelectedActionID != "" {
		t.Fatalf("expected no auto-selected action, got %q", profile.SelectedActionID)
	}
	labels := map[string]bool{}
	for _, action := range profile.Actions {
		labels[action.Label] = true
		if action.Recommended {
			t.Fatalf("deploy detection must never mark an action recommended: %#v", action)
		}
	}
	for _, label := range []string{"npm run deploy", "npm run deploy:prod", "make deploy"} {
		if !labels[label] {
			t.Fatalf("missing %s in %#v", label, profile.Actions)
		}
	}
	for _, unwanted := range []string{"npm run build", "npm run start", "make test", "make clean"} {
		if labels[unwanted] {
			t.Fatalf("unexpected non-deploy action %s in %#v", unwanted, profile.Actions)
		}
	}
}

func TestDetectorSetsNotConfiguredWhenNoDeployScriptsFound(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":{"build":"ng build","test":"jest"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	profile, err := (Detector{}).Detect(context.Background(), "p1", "Demo", root)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Status != StatusNotConfigured || len(profile.Actions) != 0 {
		t.Fatalf("profile=%#v", profile)
	}
}

func TestDetectorExcludesGeneratedFolders(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "fake"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "fake", "package.json"), []byte(`{"scripts":{"deploy":"bad"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	profile, err := (Detector{}).Detect(context.Background(), "p1", "Demo", root)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Status != StatusNotConfigured || len(profile.Actions) != 0 {
		t.Fatalf("profile=%#v", profile)
	}
}
