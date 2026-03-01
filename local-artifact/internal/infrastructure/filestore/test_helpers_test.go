package filestore

import (
	"context"
	"testing"
	"time"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/artifacts"
)

func newTextArtifact(ref, name, payload string) artifacts.Artifact {
	return artifacts.Artifact{
		Ref:       ref,
		Name:      name,
		Kind:      artifacts.ArtifactKindText,
		MimeType:  "text/plain; charset=utf-8",
		SizeBytes: int64(len(payload)),
		SHA256:    "sha-" + ref,
		CreatedAt: time.Now().UTC(),
	}
}

func mustSaveText(t *testing.T, ctx context.Context, store *Store, ref, name, payload string, opts artifacts.SaveOptions) artifacts.Artifact {
	t.Helper()
	out, err := store.Save(ctx, newTextArtifact(ref, name, payload), []byte(payload), opts)
	if err != nil {
		t.Fatalf("save %q (%s): %v", name, ref, err)
	}
	return out
}

func mustGetText(t *testing.T, ctx context.Context, store *Store, selector artifacts.Selector) (artifacts.Artifact, string) {
	t.Helper()
	meta, data, err := store.Get(ctx, selector)
	if err != nil {
		t.Fatalf("get %+v: %v", selector, err)
	}
	return meta, string(data)
}
