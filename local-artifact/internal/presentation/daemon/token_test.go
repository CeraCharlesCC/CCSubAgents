package daemon

import (
	"os"
	"runtime"
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
