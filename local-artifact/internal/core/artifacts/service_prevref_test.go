package artifacts

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
)

type memoryRepo struct {
	byRef  map[string]ArtifactVersion
	data   map[string][]byte
	byName map[string]string
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{
		byRef:  map[string]ArtifactVersion{},
		data:   map[string][]byte{},
		byName: map[string]string{},
	}
}

func (r *memoryRepo) Save(_ context.Context, a ArtifactVersion, data []byte, opts SaveOptions) (ArtifactVersion, error) {
	existingRef := strings.TrimSpace(r.byName[a.Name])
	if expected := strings.TrimSpace(opts.ExpectedPrevRef); expected != "" && expected != existingRef {
		return ArtifactVersion{}, ErrConflict
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

func (r *memoryRepo) Get(_ context.Context, sel Selector) (ArtifactVersion, []byte, error) {
	ref := strings.TrimSpace(sel.Ref)
	if ref == "" {
		resolved := strings.TrimSpace(r.byName[sel.Name])
		if resolved == "" {
			return ArtifactVersion{}, nil, ErrNotFound
		}
		ref = resolved
	}
	a, ok := r.byRef[ref]
	if !ok {
		return ArtifactVersion{}, nil, ErrNotFound
	}
	if a.Tombstone {
		return a, []byte{}, nil
	}
	return a, append([]byte(nil), r.data[ref]...), nil
}

func (r *memoryRepo) List(_ context.Context, prefix string, limit int) ([]ArtifactVersion, error) {
	names := make([]string, 0)
	for name := range r.byName {
		if prefix == "" || strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if limit <= 0 {
		limit = 200
	}
	if len(names) > limit {
		names = names[:limit]
	}
	out := make([]ArtifactVersion, 0, len(names))
	for _, name := range names {
		out = append(out, r.byRef[r.byName[name]])
	}
	return out, nil
}

func (r *memoryRepo) ListVersions(_ context.Context, name string, limit int) ([]ArtifactVersion, error) {
	if limit <= 0 {
		limit = 200
	}
	versions := make([]ArtifactVersion, 0)
	ref := strings.TrimSpace(r.byName[name])
	for ref != "" {
		v, ok := r.byRef[ref]
		if !ok {
			break
		}
		versions = append(versions, v)
		ref = strings.TrimSpace(v.PrevRef)
	}
	if len(versions) > limit {
		versions = versions[:limit]
	}
	return versions, nil
}

func (r *memoryRepo) Delete(_ context.Context, sel Selector) (ArtifactVersion, error) {
	ref := strings.TrimSpace(sel.Ref)
	if ref == "" {
		ref = strings.TrimSpace(r.byName[sel.Name])
	}
	if ref == "" {
		return ArtifactVersion{}, ErrNotFound
	}
	a, ok := r.byRef[ref]
	if !ok {
		return ArtifactVersion{}, ErrNotFound
	}
	delete(r.byName, a.Name)
	return a, nil
}

func TestServiceSaveText_SameNameCreatesPrevRefChain(t *testing.T) {
	repo := newMemoryRepo()
	svc := NewService(repo)
	refs := []string{"20260216T101010Z-aaaaaaaaaaaaaaaa", "20260216T101011Z-bbbbbbbbbbbbbbbb"}
	idx := 0
	svc.refGenerator = func() (string, error) {
		if idx >= len(refs) {
			return "", errors.New("out of refs")
		}
		ref := refs[idx]
		idx++
		return ref, nil
	}

	ctx := context.Background()
	first, err := svc.SaveText(ctx, SaveTextInput{Name: "plan/task-123", Text: "first"})
	if err != nil {
		t.Fatalf("first save failed: %v", err)
	}
	second, err := svc.SaveText(ctx, SaveTextInput{Name: "plan/task-123", Text: "second"})
	if err != nil {
		t.Fatalf("second save failed: %v", err)
	}
	if second.PrevRef != first.Ref {
		t.Fatalf("expected prevRef=%q got=%q", first.Ref, second.PrevRef)
	}

	resolved, err := svc.Resolve(ctx, "plan/task-123")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolved != second.Ref {
		t.Fatalf("expected latest ref=%q got=%q", second.Ref, resolved)
	}

	versions, err := svc.ListVersions(ctx, "plan/task-123", 10)
	if err != nil {
		t.Fatalf("list versions failed: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions got %d", len(versions))
	}
	if versions[0].Ref != second.Ref || versions[1].Ref != first.Ref {
		t.Fatalf("unexpected version order: %+v", versions)
	}
}
