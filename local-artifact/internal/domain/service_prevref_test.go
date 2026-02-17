package domain

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"
)

type memoryRepo struct {
	byRef  map[string]Artifact
	data   map[string][]byte
	byName map[string]string
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{
		byRef:  map[string]Artifact{},
		data:   map[string][]byte{},
		byName: map[string]string{},
	}
}

func (r *memoryRepo) Save(_ context.Context, a Artifact, data []byte, opts SaveOptions) (Artifact, error) {
	existingRef := strings.TrimSpace(r.byName[a.Name])
	if expected := strings.TrimSpace(opts.ExpectedPrevRef); expected != "" && expected != existingRef {
		return Artifact{}, ErrConflict
	}
	if existingRef != "" && existingRef != a.Ref {
		a.PrevRef = existingRef
	}
	r.byRef[a.Ref] = a
	r.data[a.Ref] = append([]byte(nil), data...)
	r.byName[a.Name] = a.Ref
	return a, nil
}

func (r *memoryRepo) Resolve(_ context.Context, name string) (string, error) {
	ref := strings.TrimSpace(r.byName[name])
	if ref == "" {
		return "", ErrNotFound
	}
	return ref, nil
}

func (r *memoryRepo) Get(_ context.Context, sel Selector) (Artifact, []byte, error) {
	ref := strings.TrimSpace(sel.Ref)
	if ref == "" {
		name := strings.TrimSpace(sel.Name)
		resolvedRef := strings.TrimSpace(r.byName[name])
		if resolvedRef == "" {
			return Artifact{}, nil, ErrNotFound
		}
		ref = resolvedRef
	}

	a, ok := r.byRef[ref]
	if !ok {
		return Artifact{}, nil, ErrNotFound
	}
	return a, append([]byte(nil), r.data[ref]...), nil
}

func (r *memoryRepo) List(_ context.Context, prefix string, limit int) ([]Artifact, error) {
	if limit <= 0 {
		limit = 200
	}

	names := make([]string, 0, len(r.byName))
	for name := range r.byName {
		if prefix == "" || strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) > limit {
		names = names[:limit]
	}

	out := make([]Artifact, 0, len(names))
	for _, name := range names {
		ref := r.byName[name]
		a := r.byRef[ref]
		out = append(out, a)
	}
	return out, nil
}

func (r *memoryRepo) Delete(_ context.Context, sel Selector) (Artifact, error) {
	ref := strings.TrimSpace(sel.Ref)
	if ref == "" {
		name := strings.TrimSpace(sel.Name)
		resolvedRef := strings.TrimSpace(r.byName[name])
		if resolvedRef == "" {
			return Artifact{}, ErrNotFound
		}
		ref = resolvedRef
	}
	a, ok := r.byRef[ref]
	if !ok {
		return Artifact{}, ErrNotFound
	}
	delete(r.byRef, ref)
	delete(r.data, ref)
	for name, nameRef := range r.byName {
		if nameRef == ref {
			delete(r.byName, name)
		}
	}
	return a, nil
}

func TestServiceSaveText_SameNameCreatesVersionLinkAndKeepsOldRefReadable(t *testing.T) {
	repo := newMemoryRepo()
	svc := NewService(repo)
	refs := []string{
		"20260216T101010Z-aaaaaaaaaaaaaaaa",
		"20260216T101011Z-bbbbbbbbbbbbbbbb",
	}
	refIdx := 0
	svc.refGenerator = func() (string, error) {
		if refIdx >= len(refs) {
			return "", errors.New("out of refs")
		}
		ref := refs[refIdx]
		refIdx++
		return ref, nil
	}

	ctx := context.Background()
	first, err := svc.SaveText(ctx, SaveTextInput{Name: "plan/task-123", Text: "first"})
	if err != nil {
		t.Fatalf("first save failed: %v", err)
	}
	if first.PrevRef != "" {
		t.Fatalf("expected empty prevRef on first save, got %q", first.PrevRef)
	}

	second, err := svc.SaveText(ctx, SaveTextInput{Name: "plan/task-123", Text: "second"})
	if err != nil {
		t.Fatalf("second save failed: %v", err)
	}
	if second.PrevRef != first.Ref {
		t.Fatalf("expected second prevRef=%q, got %q", first.Ref, second.PrevRef)
	}

	resolvedRef, err := svc.Resolve(ctx, "plan/task-123")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolvedRef != second.Ref {
		t.Fatalf("expected resolve=%q, got %q", second.Ref, resolvedRef)
	}

	firstMeta, firstData, err := svc.Get(ctx, Selector{Ref: first.Ref})
	if err != nil {
		t.Fatalf("get first by ref failed: %v", err)
	}
	if firstMeta.Ref != first.Ref || string(firstData) != "first" {
		t.Fatalf("unexpected first artifact data: meta=%+v data=%q", firstMeta, string(firstData))
	}

	latestMeta, latestData, err := svc.Get(ctx, Selector{Name: "plan/task-123"})
	if err != nil {
		t.Fatalf("get latest by name failed: %v", err)
	}
	if latestMeta.Ref != second.Ref || string(latestData) != "second" {
		t.Fatalf("unexpected latest artifact data: meta=%+v data=%q", latestMeta, string(latestData))
	}

	listed, err := svc.List(ctx, "plan/", 10)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected 1 listed item, got %d", len(listed))
	}
	if listed[0].Ref != second.Ref || listed[0].PrevRef != first.Ref {
		t.Fatalf("unexpected listed latest artifact: %+v", listed[0])
	}
}

func TestServiceSaveText_RefGenerationErrorIsSurfaced(t *testing.T) {
	repo := newMemoryRepo()
	svc := NewService(repo)
	svc.refGenerator = func() (string, error) {
		return "", errors.New("rng failed")
	}

	_, err := svc.SaveText(context.Background(), SaveTextInput{Name: "plan/task-123", Text: "payload"})
	if err == nil {
		t.Fatal("expected save to fail when ref generation fails")
	}
	if !errors.Is(err, ErrInternal) {
		t.Fatalf("expected ErrInternal, got: %v", err)
	}
	if !strings.Contains(err.Error(), "rng failed") {
		t.Fatalf("expected surfaced rng error text, got: %v", err)
	}
}

func TestServiceSaveText_ExpectedPrevRefMatchSucceeds(t *testing.T) {
	repo := newMemoryRepo()
	svc := NewService(repo)
	refs := []string{"20260216T101010Z-aaaaaaaaaaaaaaaa", "20260216T101011Z-bbbbbbbbbbbbbbbb"}
	idx := 0
	svc.refGenerator = func() (string, error) {
		ref := refs[idx]
		idx++
		return ref, nil
	}

	ctx := context.Background()
	first, err := svc.SaveText(ctx, SaveTextInput{Name: "plan/task-guard", Text: "first"})
	if err != nil {
		t.Fatalf("first save failed: %v", err)
	}

	second, err := svc.SaveText(ctx, SaveTextInput{Name: "plan/task-guard", Text: "second", ExpectedPrevRef: first.Ref})
	if err != nil {
		t.Fatalf("second save with matching expectedPrevRef failed: %v", err)
	}
	if second.PrevRef != first.Ref {
		t.Fatalf("expected prevRef=%q, got %q", first.Ref, second.PrevRef)
	}
}

func TestServiceSaveText_ExpectedPrevRefStaleReturnsConflict(t *testing.T) {
	repo := newMemoryRepo()
	svc := NewService(repo)
	refs := []string{"20260216T101020Z-aaaaaaaaaaaaaaaa", "20260216T101021Z-bbbbbbbbbbbbbbbb"}
	idx := 0
	svc.refGenerator = func() (string, error) {
		ref := refs[idx]
		idx++
		return ref, nil
	}

	ctx := context.Background()
	first, err := svc.SaveText(ctx, SaveTextInput{Name: "plan/task-guard-stale", Text: "first"})
	if err != nil {
		t.Fatalf("first save failed: %v", err)
	}

	_, err = svc.SaveText(ctx, SaveTextInput{Name: "plan/task-guard-stale", Text: "second", ExpectedPrevRef: "20260216T101019Z-cccccccccccccccc"})
	if err == nil {
		t.Fatal("expected conflict error for stale expectedPrevRef")
	}
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}

	resolved, err := svc.Resolve(ctx, "plan/task-guard-stale")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolved != first.Ref {
		t.Fatalf("expected latest ref to remain %q, got %q", first.Ref, resolved)
	}
}

func TestServiceNewRef_ProducesValidRefShape(t *testing.T) {
	ref, err := newRef()
	if err != nil {
		t.Fatalf("newRef failed: %v", err)
	}
	if _, err := normalizeAndValidateRef(ref); err != nil {
		t.Fatalf("newRef did not match ref pattern: ref=%q err=%v", ref, err)
	}
	if !strings.Contains(ref, "Z-") {
		t.Fatalf("unexpected ref format: %q", ref)
	}
	if nowUTCSecond().After(time.Now().UTC().Add(2 * time.Second)) {
		t.Fatal("nowUTCSecond should be in UTC current-time range")
	}
}
