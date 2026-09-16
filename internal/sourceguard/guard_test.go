package sourceguard

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuardRestoresPlaceholderRewrite(t *testing.T) {
	project := t.TempDir()
	sourcePath := filepath.Join(project, "src", "main.tsx")
	original := strings.Repeat("export const value = 1;\n", 80)
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	guard, err := Begin(context.Background(), project, nil, "DEV-001", "developer")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte("PLACEHOLDER"), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := guard.VerifyAndRestore(context.Background())
	if err == nil {
		t.Fatal("expected suspicious rewrite to be reported")
	}
	if len(report.Restored) != 1 || report.Restored[0].RelativePath != "src/main.tsx" {
		t.Fatalf("unexpected restored files: %+v", report.Restored)
	}
	restored, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != original {
		t.Fatal("source file was not restored from the recovery snapshot")
	}
	if _, err := os.Stat(filepath.Join(report.RecoveryDir, "manifest.json")); err != nil {
		t.Fatalf("expected recovery manifest to be kept for debugging: %v", err)
	}
}

func TestGuardIgnoresOrdinarySourceEditAndCleansSnapshot(t *testing.T) {
	project := t.TempDir()
	sourcePath := filepath.Join(project, "src", "main.ts")
	original := strings.Repeat("console.log('before');\n", 80)
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	guard, err := Begin(context.Background(), project, nil, "DEV-002", "developer")
	if err != nil {
		t.Fatal(err)
	}
	recoveryDir := guard.recoveryDir
	updated := original + "console.log('after');\n"
	if err := os.WriteFile(sourcePath, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := guard.VerifyAndRestore(context.Background())
	if err != nil {
		t.Fatalf("ordinary source edit should not be blocked: %v", err)
	}
	if len(report.Restored) != 0 {
		t.Fatalf("unexpected restored files: %+v", report.Restored)
	}
	if _, err := os.Stat(recoveryDir); !os.IsNotExist(err) {
		t.Fatalf("expected clean snapshot to be removed, stat err=%v", err)
	}
}

func TestGuardSkipsProductCrewStorage(t *testing.T) {
	project := t.TempDir()
	requestFile := filepath.Join(project, ".productcrew", "requests", "request-1", "description.md")
	if err := os.MkdirAll(filepath.Dir(requestFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(requestFile, []byte(strings.Repeat("request\n", 200)), 0o600); err != nil {
		t.Fatal(err)
	}

	guard, err := Begin(context.Background(), project, nil, "DEV-003", "developer")
	if err != nil {
		t.Fatal(err)
	}
	if len(guard.roots) != 1 || len(guard.roots[0].Files) != 0 {
		t.Fatalf("expected .productcrew files to be excluded, got %+v", guard.roots)
	}
}
