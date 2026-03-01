package filestore

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/artifacts"
)

func TestStorePrevRef_SameNameCreatesVersionLinkAndPreservesOldRef(t *testing.T) {
	ctx := context.Background()
	store := New(t.TempDir())

	firstRef := "20260216T120000Z-aaaaaaaaaaaaaaaa"
	secondRef := "20260216T120001Z-bbbbbbbbbbbbbbbb"
	name := "plan/task-123"

	firstOut := mustSaveText(t, ctx, store, firstRef, name, "first", artifacts.SaveOptions{})
	if firstOut.PrevRef != "" {
		t.Fatalf("expected first prevRef empty, got %q", firstOut.PrevRef)
	}

	secondOut := mustSaveText(t, ctx, store, secondRef, name, "second", artifacts.SaveOptions{})
	if secondOut.PrevRef != firstRef {
		t.Fatalf("expected second prevRef=%q, got %q", firstRef, secondOut.PrevRef)
	}

	resolvedRef, err := store.Resolve(ctx, name)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolvedRef != secondRef {
		t.Fatalf("expected resolve=%q, got %q", secondRef, resolvedRef)
	}

	latestMeta, latestData := mustGetText(t, ctx, store, artifacts.Selector{Name: name})
	if latestMeta.Ref != secondRef || latestMeta.PrevRef != firstRef || latestData != "second" {
		t.Fatalf("unexpected latest artifact: meta=%+v data=%q", latestMeta, latestData)
	}

	firstMeta, firstData := mustGetText(t, ctx, store, artifacts.Selector{Ref: firstRef})
	if firstMeta.Ref != firstRef || firstData != "first" {
		t.Fatalf("unexpected first artifact: meta=%+v data=%q", firstMeta, firstData)
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

	start := make(chan struct{})
	errCh := make(chan error, len(refs))
	var wg sync.WaitGroup
	for _, ref := range refs {
		ref := ref
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			payload := payloadByRef[ref]
			_, err := store.Save(ctx, newTextArtifact(ref, name, payload), []byte(payload), artifacts.SaveOptions{})
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
	latest, latestData := mustGetText(t, ctx, store, artifacts.Selector{Name: name})
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
	if expected := payloadByRef[latest.Ref]; latestData != expected {
		t.Fatalf("unexpected latest payload: got %q want %q", latestData, expected)
	}

	for _, ref := range refs {
		meta, data := mustGetText(t, ctx, store, artifacts.Selector{Ref: ref})
		if meta.Ref != ref {
			t.Fatalf("expected ref %q, got %q", ref, meta.Ref)
		}
		if expected := payloadByRef[ref]; data != expected {
			t.Fatalf("unexpected payload for ref %s: got %q want %q", ref, data, expected)
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
	name := "plan/task-cas"

	_ = mustSaveText(t, ctx, store, firstRef, name, "first", artifacts.SaveOptions{})
	out := mustSaveText(t, ctx, store, secondRef, name, "second", artifacts.SaveOptions{ExpectedPrevRef: firstRef})
	if out.PrevRef != firstRef {
		t.Fatalf("expected prevRef=%q, got %q", firstRef, out.PrevRef)
	}
}

func TestStorePrevRef_ExpectedPrevRefStaleReturnsConflict(t *testing.T) {
	ctx := context.Background()
	store := New(t.TempDir())

	firstRef := "20260216T120020Z-aaaaaaaaaaaaaaaa"
	staleRef := "20260216T120019Z-cccccccccccccccc"
	name := "plan/task-cas-stale"

	_ = mustSaveText(t, ctx, store, firstRef, name, "first", artifacts.SaveOptions{})
	_, err := store.Save(
		ctx,
		newTextArtifact("20260216T120021Z-bbbbbbbbbbbbbbbb", name, "second"),
		[]byte("second"),
		artifacts.SaveOptions{ExpectedPrevRef: staleRef},
	)
	if err == nil {
		t.Fatal("expected stale expectedPrevRef to return conflict")
	}
	if !errors.Is(err, artifacts.ErrConflict) {
		t.Fatalf("expected conflict error, got %v", err)
	}

	resolved, err := store.Resolve(ctx, name)
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
	_ = mustSaveText(t, ctx, store, firstRef, name, "first", artifacts.SaveOptions{})

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
		out artifacts.Artifact
		err error
	}

	start := make(chan struct{})
	resultCh := make(chan saveResult, len(refs))
	var wg sync.WaitGroup

	for _, ref := range refs {
		ref := ref
		artifact := newTextArtifact(ref, name, payloadByRef[ref])
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			out, err := store.Save(ctx, artifact, []byte(payloadByRef[ref]), artifacts.SaveOptions{ExpectedPrevRef: firstRef})
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
		case errors.Is(result.err, artifacts.ErrConflict):
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

	latest, latestData := mustGetText(t, ctx, store, artifacts.Selector{Name: name})
	if latest.Ref != successRef || latest.PrevRef != firstRef {
		t.Fatalf("unexpected latest chain: latest=%+v firstRef=%q", latest, firstRef)
	}
	if expected := payloadByRef[successRef]; latestData != expected {
		t.Fatalf("unexpected latest payload: got %q want %q", latestData, expected)
	}

	firstMeta, firstData := mustGetText(t, ctx, store, artifacts.Selector{Ref: firstRef})
	if firstMeta.Ref != firstRef || firstData != "first" {
		t.Fatalf("unexpected seed artifact state: meta=%+v data=%q", firstMeta, firstData)
	}

	if _, _, err := store.Get(ctx, artifacts.Selector{Ref: conflictRef}); !errors.Is(err, artifacts.ErrNotFound) {
		t.Fatalf("expected conflicted ref %q to be absent, got err=%v", conflictRef, err)
	}
}
