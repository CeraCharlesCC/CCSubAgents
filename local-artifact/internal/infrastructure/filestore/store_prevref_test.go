package filestore

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/domain"
)

func TestStorePrevRef_SameNameCreatesVersionLinkAndPreservesOldRef(t *testing.T) {
	ctx := context.Background()
	store := New(t.TempDir())

	firstRef := "20260216T120000Z-aaaaaaaaaaaaaaaa"
	secondRef := "20260216T120001Z-bbbbbbbbbbbbbbbb"

	firstIn := domain.Artifact{
		Ref:       firstRef,
		Name:      "plan/task-123",
		Kind:      domain.ArtifactKindText,
		MimeType:  "text/plain; charset=utf-8",
		SizeBytes: 5,
		SHA256:    "sha-first",
		CreatedAt: time.Now().UTC(),
	}
	firstOut, err := store.Save(ctx, firstIn, []byte("first"))
	if err != nil {
		t.Fatalf("first save failed: %v", err)
	}
	if firstOut.PrevRef != "" {
		t.Fatalf("expected first prevRef empty, got %q", firstOut.PrevRef)
	}

	secondIn := domain.Artifact{
		Ref:       secondRef,
		Name:      "plan/task-123",
		Kind:      domain.ArtifactKindText,
		MimeType:  "text/plain; charset=utf-8",
		SizeBytes: 6,
		SHA256:    "sha-second",
		CreatedAt: time.Now().UTC(),
	}
	secondOut, err := store.Save(ctx, secondIn, []byte("second"))
	if err != nil {
		t.Fatalf("second save failed: %v", err)
	}
	if secondOut.PrevRef != firstRef {
		t.Fatalf("expected second prevRef=%q, got %q", firstRef, secondOut.PrevRef)
	}

	resolvedRef, err := store.Resolve(ctx, "plan/task-123")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolvedRef != secondRef {
		t.Fatalf("expected resolve=%q, got %q", secondRef, resolvedRef)
	}

	latestMeta, latestData, err := store.Get(ctx, domain.Selector{Name: "plan/task-123"})
	if err != nil {
		t.Fatalf("get latest by name failed: %v", err)
	}
	if latestMeta.Ref != secondRef || latestMeta.PrevRef != firstRef || string(latestData) != "second" {
		t.Fatalf("unexpected latest artifact: meta=%+v data=%q", latestMeta, string(latestData))
	}

	firstMeta, firstData, err := store.Get(ctx, domain.Selector{Ref: firstRef})
	if err != nil {
		t.Fatalf("get old by ref failed: %v", err)
	}
	if firstMeta.Ref != firstRef || string(firstData) != "first" {
		t.Fatalf("unexpected first artifact: meta=%+v data=%q", firstMeta, string(firstData))
	}

	listed, err := store.List(ctx, "plan/", 10)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected one listed artifact, got %d", len(listed))
	}
	if listed[0].Ref != secondRef || listed[0].PrevRef != firstRef {
		t.Fatalf("unexpected listed latest artifact: %+v", listed[0])
	}
}

func TestStorePrevRef_ConcurrentSameNameSavesKeepVersionLink(t *testing.T) {
	ctx := context.Background()
	store := New(t.TempDir())

	name := "plan/task-concurrent"
	refs := []string{
		"20260216T120100Z-aaaaaaaaaaaaaaaa",
		"20260216T120101Z-bbbbbbbbbbbbbbbb",
	}
	payloadByRef := map[string]string{
		refs[0]: "first",
		refs[1]: "second",
	}

	artifacts := []domain.Artifact{
		{
			Ref:       refs[0],
			Name:      name,
			Kind:      domain.ArtifactKindText,
			MimeType:  "text/plain; charset=utf-8",
			SizeBytes: int64(len(payloadByRef[refs[0]])),
			SHA256:    "sha-first",
			CreatedAt: time.Now().UTC(),
		},
		{
			Ref:       refs[1],
			Name:      name,
			Kind:      domain.ArtifactKindText,
			MimeType:  "text/plain; charset=utf-8",
			SizeBytes: int64(len(payloadByRef[refs[1]])),
			SHA256:    "sha-second",
			CreatedAt: time.Now().UTC(),
		},
	}

	start := make(chan struct{})
	errCh := make(chan error, len(artifacts))
	var wg sync.WaitGroup
	for idx := range artifacts {
		idx := idx
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := store.Save(ctx, artifacts[idx], []byte(payloadByRef[artifacts[idx].Ref]))
			errCh <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent save failed: %v", err)
		}
	}

	resolvedRef, err := store.Resolve(ctx, name)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	latest, latestData, err := store.Get(ctx, domain.Selector{Name: name})
	if err != nil {
		t.Fatalf("get latest by name failed: %v", err)
	}
	if latest.Ref != resolvedRef {
		t.Fatalf("expected latest ref to match resolve (%q), got %q", resolvedRef, latest.Ref)
	}
	if latest.PrevRef == "" {
		t.Fatal("expected latest artifact to link to previous ref")
	}
	if latest.PrevRef == latest.Ref {
		t.Fatalf("expected latest prevRef to differ from latest ref, got %q", latest.PrevRef)
	}
	if latest.PrevRef != refs[0] && latest.PrevRef != refs[1] {
		t.Fatalf("unexpected latest prevRef %q", latest.PrevRef)
	}
	if expected := payloadByRef[latest.Ref]; string(latestData) != expected {
		t.Fatalf("unexpected latest payload: got %q want %q", string(latestData), expected)
	}

	for _, ref := range refs {
		meta, data, err := store.Get(ctx, domain.Selector{Ref: ref})
		if err != nil {
			t.Fatalf("get by ref %s failed: %v", ref, err)
		}
		if meta.Ref != ref {
			t.Fatalf("expected ref %q, got %q", ref, meta.Ref)
		}
		if expected := payloadByRef[ref]; string(data) != expected {
			t.Fatalf("unexpected payload for ref %s: got %q want %q", ref, string(data), expected)
		}
	}

	listed, err := store.List(ctx, "plan/", 10)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected one listed artifact, got %d", len(listed))
	}
	if listed[0].Ref != latest.Ref || listed[0].PrevRef != latest.PrevRef {
		t.Fatalf("unexpected listed artifact: %+v (latest: %+v)", listed[0], latest)
	}
}
