package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/artifacts"
)

func newArtifactRepo(t *testing.T) *ArtifactRepository {
	t.Helper()
	repo, err := NewArtifactRepository(t.TempDir())
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}
	t.Cleanup(func() {
		_ = repo.Close()
	})
	return repo
}

func shaFor(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func makeVersion(ref, name, mime string, data []byte, createdAt time.Time) artifacts.ArtifactVersion {
	kind := artifacts.ArtifactKindFile
	if len(mime) >= 5 && mime[:5] == "text/" {
		kind = artifacts.ArtifactKindText
	}
	return artifacts.ArtifactVersion{
		Ref:       ref,
		Name:      name,
		Kind:      kind,
		MimeType:  mime,
		SizeBytes: int64(len(data)),
		SHA256:    shaFor(data),
		CreatedAt: createdAt.UTC().Truncate(time.Second),
	}
}

func mustSaveVersion(t *testing.T, ctx context.Context, repo *ArtifactRepository, ref, name, mime string, data []byte, createdAt time.Time, opts artifacts.SaveOptions) artifacts.ArtifactVersion {
	t.Helper()
	v := makeVersion(ref, name, mime, data, createdAt)
	out, err := repo.Save(ctx, v, data, opts)
	if err != nil {
		t.Fatalf("save %q (%s): %v", name, ref, err)
	}
	return out
}
