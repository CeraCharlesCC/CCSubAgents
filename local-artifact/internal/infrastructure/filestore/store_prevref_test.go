package filestore

import (
	"context"
	"errors"
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
	firstOut, err := store.Save(ctx, firstIn, []byte("first"), domain.SaveOptions{})
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
	secondOut, err := store.Save(ctx, secondIn, []byte("second"), domain.SaveOptions{})
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
			_, err := store.Save(ctx, artifacts[idx], []byte(payloadByRef[artifacts[idx].Ref]), domain.SaveOptions{})
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

func TestStorePrevRef_ExpectedPrevRefMatchSucceeds(t *testing.T) {
	ctx := context.Background()
	store := New(t.TempDir())

	firstRef := "20260216T120010Z-aaaaaaaaaaaaaaaa"
	secondRef := "20260216T120011Z-bbbbbbbbbbbbbbbb"

	first := domain.Artifact{
		Ref:       firstRef,
		Name:      "plan/task-cas",
		Kind:      domain.ArtifactKindText,
		MimeType:  "text/plain; charset=utf-8",
		SizeBytes: 5,
		SHA256:    "sha-first",
		CreatedAt: time.Now().UTC(),
	}
	if _, err := store.Save(ctx, first, []byte("first"), domain.SaveOptions{}); err != nil {
		t.Fatalf("first save failed: %v", err)
	}

	second := domain.Artifact{
		Ref:       secondRef,
		Name:      "plan/task-cas",
		Kind:      domain.ArtifactKindText,
		MimeType:  "text/plain; charset=utf-8",
		SizeBytes: 6,
		SHA256:    "sha-second",
		CreatedAt: time.Now().UTC(),
	}
	out, err := store.Save(ctx, second, []byte("second"), domain.SaveOptions{ExpectedPrevRef: firstRef})
	if err != nil {
		t.Fatalf("second save with matching expectedPrevRef failed: %v", err)
	}
	if out.PrevRef != firstRef {
		t.Fatalf("expected prevRef=%q, got %q", firstRef, out.PrevRef)
	}
}

func TestStorePrevRef_ExpectedPrevRefStaleReturnsConflict(t *testing.T) {
	ctx := context.Background()
	store := New(t.TempDir())

	firstRef := "20260216T120020Z-aaaaaaaaaaaaaaaa"
	staleRef := "20260216T120019Z-cccccccccccccccc"

	first := domain.Artifact{
		Ref:       firstRef,
		Name:      "plan/task-cas-stale",
		Kind:      domain.ArtifactKindText,
		MimeType:  "text/plain; charset=utf-8",
		SizeBytes: 5,
		SHA256:    "sha-first",
		CreatedAt: time.Now().UTC(),
	}
	if _, err := store.Save(ctx, first, []byte("first"), domain.SaveOptions{}); err != nil {
		t.Fatalf("first save failed: %v", err)
	}

	second := domain.Artifact{
		Ref:       "20260216T120021Z-bbbbbbbbbbbbbbbb",
		Name:      "plan/task-cas-stale",
		Kind:      domain.ArtifactKindText,
		MimeType:  "text/plain; charset=utf-8",
		SizeBytes: 6,
		SHA256:    "sha-second",
		CreatedAt: time.Now().UTC(),
	}
	_, err := store.Save(ctx, second, []byte("second"), domain.SaveOptions{ExpectedPrevRef: staleRef})
	if err == nil {
		t.Fatal("expected stale expectedPrevRef to return conflict")
	}
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected conflict error, got %v", err)
	}

	resolved, err := store.Resolve(ctx, "plan/task-cas-stale")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolved != firstRef {
		t.Fatalf("expected latest ref to remain %q, got %q", firstRef, resolved)
	}
}

func TestStorePrevRef_ConcurrentCASSameExpectedPrevRef_OneSuccessOneConflict(t *testing.T) {
	ctx := context.Background()
	store := New(t.TempDir())

	name := "plan/task-cas-race"
	firstRef := "20260216T120030Z-aaaaaaaaaaaaaaaa"
	seed := domain.Artifact{
		Ref:       firstRef,
		Name:      name,
		Kind:      domain.ArtifactKindText,
		MimeType:  "text/plain; charset=utf-8",
		SizeBytes: 5,
		SHA256:    "sha-first",
		CreatedAt: time.Now().UTC(),
	}
	if _, err := store.Save(ctx, seed, []byte("first"), domain.SaveOptions{}); err != nil {
		t.Fatalf("seed save failed: %v", err)
	}

	refs := []string{
		"20260216T120031Z-bbbbbbbbbbbbbbbb",
		"20260216T120032Z-cccccccccccccccc",
	}
	payloadByRef := map[string]string{
		refs[0]: "second-a",
		refs[1]: "second-b",
	}

	type saveResult struct {
		ref string
		out domain.Artifact
		err error
	}

	start := make(chan struct{})
	resultCh := make(chan saveResult, len(refs))
	var wg sync.WaitGroup

	for _, ref := range refs {
		ref := ref
		artifact := domain.Artifact{
			Ref:       ref,
			Name:      name,
			Kind:      domain.ArtifactKindText,
			MimeType:  "text/plain; charset=utf-8",
			SizeBytes: int64(len(payloadByRef[ref])),
			SHA256:    "sha-" + ref,
			CreatedAt: time.Now().UTC(),
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			out, err := store.Save(ctx, artifact, []byte(payloadByRef[ref]), domain.SaveOptions{ExpectedPrevRef: firstRef})
			resultCh <- saveResult{ref: ref, out: out, err: err}
		}()
	}

	close(start)
	wg.Wait()
	close(resultCh)

	successes := 0
	conflicts := 0
	successRef := ""
	conflictRef := ""
	for result := range resultCh {
		switch {
		case result.err == nil:
			successes++
			successRef = result.ref
			if result.out.PrevRef != firstRef {
				t.Fatalf("successful CAS save expected prevRef=%q, got %q", firstRef, result.out.PrevRef)
			}
		case errors.Is(result.err, domain.ErrConflict):
			conflicts++
			conflictRef = result.ref
		default:
			t.Fatalf("unexpected concurrent CAS error for %q: %v", result.ref, result.err)
		}
	}

	if successes != 1 || conflicts != 1 {
		t.Fatalf("expected exactly one success and one conflict, got successes=%d conflicts=%d", successes, conflicts)
	}

	resolved, err := store.Resolve(ctx, name)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolved != successRef {
		t.Fatalf("expected resolve to point at successful ref %q, got %q", successRef, resolved)
	}

	latest, latestData, err := store.Get(ctx, domain.Selector{Name: name})
	if err != nil {
		t.Fatalf("get latest failed: %v", err)
	}
	if latest.Ref != successRef || latest.PrevRef != firstRef {
		t.Fatalf("unexpected latest chain: latest=%+v firstRef=%q", latest, firstRef)
	}
	if expected := payloadByRef[successRef]; string(latestData) != expected {
		t.Fatalf("unexpected latest payload: got %q want %q", string(latestData), expected)
	}

	firstMeta, firstData, err := store.Get(ctx, domain.Selector{Ref: firstRef})
	if err != nil {
		t.Fatalf("get seed ref failed: %v", err)
	}
	if firstMeta.Ref != firstRef || string(firstData) != "first" {
		t.Fatalf("unexpected seed artifact state: meta=%+v data=%q", firstMeta, string(firstData))
	}

	if _, _, err := store.Get(ctx, domain.Selector{Ref: conflictRef}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected conflicted ref %q to be absent, got err=%v", conflictRef, err)
	}
}
