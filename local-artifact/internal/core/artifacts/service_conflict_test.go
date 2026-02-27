package artifacts

import (
	"context"
	"errors"
	"testing"
)

func TestServiceSaveText_ExpectedPrevRefConflict(t *testing.T) {
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
	first, err := svc.SaveText(ctx, SaveTextInput{Name: "plan/task-cas", Text: "first"})
	if err != nil {
		t.Fatalf("first save failed: %v", err)
	}
	_, err = svc.SaveText(ctx, SaveTextInput{
		Name:            "plan/task-cas",
		Text:            "second",
		ExpectedPrevRef: "20260216T101019Z-cccccccccccccccc",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}

	resolved, err := svc.Resolve(ctx, "plan/task-cas")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolved != first.Ref {
		t.Fatalf("expected latest ref to remain %q, got %q", first.Ref, resolved)
	}
}
