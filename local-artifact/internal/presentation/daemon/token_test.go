package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveOrCreateToken_PersistsExplicitToken(t *testing.T) {
	root := t.TempDir()
	const explicit = "fixed-token-value"

	got, err := ResolveOrCreateToken(root, explicit)
	if err != nil {
		t.Fatalf("resolve explicit token: %v", err)
	}
	if got != explicit {
		t.Fatalf("token mismatch: got=%q want=%q", got, explicit)
	}

	stored, err := ReadToken(root)
	if err != nil {
		t.Fatalf("read persisted token: %v", err)
	}
	if stored != explicit {
		t.Fatalf("persisted token mismatch: got=%q want=%q", stored, explicit)
	}

	info, err := os.Stat(tokenFilePath(root))
	if err != nil {
		t.Fatalf("stat token file: %v", err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	if gotMode := info.Mode().Perm(); gotMode != 0o600 {
		t.Fatalf("token file mode mismatch: got=%#o want=%#o", gotMode, 0o600)
	}
}

func TestResolveOrCreateToken_RegeneratesWhenFileEmpty(t *testing.T) {
	root := t.TempDir()
	tokenPath := filepath.Join(root, "daemon", tokenFileName)
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o755); err != nil {
		t.Fatalf("create token directory: %v", err)
	}
	if err := os.WriteFile(tokenPath, []byte("  \n"), 0o600); err != nil {
		t.Fatalf("seed empty token file: %v", err)
	}

	got, err := ResolveOrCreateToken(root, "")
	if err != nil {
		t.Fatalf("resolve token from empty file: %v", err)
	}
	if strings.TrimSpace(got) == "" {
		t.Fatalf("expected non-empty regenerated token")
	}

	persisted, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	if strings.TrimSpace(string(persisted)) != got {
		t.Fatalf("persisted token mismatch: got=%q want=%q", strings.TrimSpace(string(persisted)), got)
	}
}
