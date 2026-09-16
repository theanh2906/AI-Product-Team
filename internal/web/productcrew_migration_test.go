package web

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateProductCrewDirectoryPreservesLegacyData(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, ".mini-ai-product-team")
	target := filepath.Join(home, ".productcrew")
	if err := os.MkdirAll(filepath.Join(legacy, "logs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "settings.json"), []byte(`{"theme":"dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "logs", "application.jsonl"), []byte("event\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := migrateProductCrewDirectory(home, target); err != nil {
		t.Fatal(err)
	}

	for _, relative := range []string{"settings.json", filepath.Join("logs", "application.jsonl")} {
		if _, err := os.Stat(filepath.Join(target, relative)); err != nil {
			t.Fatalf("migrated file %s: %v", relative, err)
		}
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy directory still exists after atomic migration: %v", err)
	}
}

func TestMigrateProductCrewDirectoryDoesNotOverwriteNewData(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, ".mini-ai-product-team")
	target := filepath.Join(home, ".productcrew")
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "settings.json"), []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "settings.json"), []byte("current"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := migrateProductCrewDirectory(home, target); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(target, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "current" {
		t.Fatalf("new data was overwritten: %q", data)
	}
}
