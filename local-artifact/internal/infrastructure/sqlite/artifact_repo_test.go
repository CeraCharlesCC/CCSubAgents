package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/artifacts"
)

func TestArtifactRepository_SaveGetResolveListDeleteListVersions(t *testing.T) {
	repo := newArtifactRepo(t)
	ctx := context.Background()

	firstData := []byte("first")
	firstRef := "20260216T120000Z-aaaaaaaaaaaaaaaa"
	firstOut := mustSaveVersion(t, ctx, repo, firstRef, "plan/task-1", "text/plain; charset=utf-8", firstData, time.Now(), artifacts.SaveOptions{})
	if firstOut.PrevRef != "" {
		t.Fatalf("expected first prevRef empty, got %q", firstOut.PrevRef)
	}

	secondData := []byte("second")
	secondRef := "20260216T120001Z-bbbbbbbbbbbbbbbb"
	secondOut := mustSaveVersion(t, ctx, repo, secondRef, "plan/task-1", "text/plain; charset=utf-8", secondData, time.Now().Add(time.Second), artifacts.SaveOptions{ExpectedPrevRef: firstRef})
	if secondOut.PrevRef != firstRef {
		t.Fatalf("expected second prevRef=%q, got %q", firstRef, secondOut.PrevRef)
	}

	resolved, err := repo.Resolve(ctx, "plan/task-1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved != secondRef {
		t.Fatalf("expected latest ref=%q got=%q", secondRef, resolved)
	}

	oldMeta, oldData, err := repo.Get(ctx, artifacts.Selector{Ref: firstRef})
	if err != nil {
		t.Fatalf("get old by ref: %v", err)
	}
	if oldMeta.Ref != firstRef || string(oldData) != "first" {
		t.Fatalf("unexpected old payload: meta=%+v data=%q", oldMeta, string(oldData))
	}

	latestMeta, latestData, err := repo.Get(ctx, artifacts.Selector{Name: "plan/task-1"})
	if err != nil {
		t.Fatalf("get latest by name: %v", err)
	}
	if latestMeta.Ref != secondRef || string(latestData) != "second" {
		t.Fatalf("unexpected latest payload: meta=%+v data=%q", latestMeta, string(latestData))
	}

	listed, err := repo.List(ctx, "plan/", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 || listed[0].Ref != secondRef {
		t.Fatalf("unexpected list result: %+v", listed)
	}

	versions, err := repo.ListVersions(ctx, "plan/task-1", 10)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions got %d", len(versions))
	}
	if versions[0].Ref != secondRef || versions[1].Ref != firstRef {
		t.Fatalf("unexpected version order: %+v", versions)
	}

	tomb, err := repo.Delete(ctx, artifacts.Selector{Name: "plan/task-1"})
	if err != nil {
		t.Fatalf("delete by name: %v", err)
	}
	if !tomb.Tombstone {
		t.Fatalf("expected tombstone delete result, got %+v", tomb)
	}
	if tomb.PrevRef != secondRef {
		t.Fatalf("expected tombstone prevRef=%q, got %q", secondRef, tomb.PrevRef)
	}

	if _, err := repo.Resolve(ctx, "plan/task-1"); !errors.Is(err, artifacts.ErrNotFound) {
		t.Fatalf("expected resolve not found after delete, got %v", err)
	}

	tombMeta, tombData, err := repo.Get(ctx, artifacts.Selector{Ref: tomb.Ref})
	if err != nil {
		t.Fatalf("get tombstone by ref: %v", err)
	}
	if !tombMeta.Tombstone || len(tombData) != 0 {
		t.Fatalf("unexpected tombstone payload: meta=%+v dataLen=%d", tombMeta, len(tombData))
	}
}

func TestArtifactRepository_ListVersions_UsesParentChainOrderForSameSecondWrites(t *testing.T) {
	repo := newArtifactRepo(t)
	ctx := context.Background()
	createdAt := time.Date(2026, 2, 16, 12, 0, 0, 0, time.UTC)

	refs := []string{
		"20260216T120000Z-ffffffffffffffff",
		"20260216T120000Z-1111111111111111",
		"20260216T120000Z-0000000000000000",
	}
	payloads := [][]byte{[]byte("first"), []byte("second"), []byte("third")}

	firstOut := mustSaveVersion(t, ctx, repo, refs[0], "plan/same-second", "text/plain; charset=utf-8", payloads[0], createdAt, artifacts.SaveOptions{})
	secondOut := mustSaveVersion(t, ctx, repo, refs[1], "plan/same-second", "text/plain; charset=utf-8", payloads[1], createdAt, artifacts.SaveOptions{ExpectedPrevRef: firstOut.Ref})
	thirdOut := mustSaveVersion(t, ctx, repo, refs[2], "plan/same-second", "text/plain; charset=utf-8", payloads[2], createdAt, artifacts.SaveOptions{ExpectedPrevRef: secondOut.Ref})

	versions, err := repo.ListVersions(ctx, "plan/same-second", 10)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("expected 3 versions, got %d", len(versions))
	}
	if versions[0].Ref != thirdOut.Ref || versions[1].Ref != secondOut.Ref || versions[2].Ref != firstOut.Ref {
		t.Fatalf("unexpected ListVersions order: %+v", versions)
	}
	if versions[0].PrevRef != secondOut.Ref || versions[1].PrevRef != firstOut.Ref || versions[2].PrevRef != "" {
		t.Fatalf("unexpected ListVersions prevRef chain: %+v", versions)
	}
}

func TestArtifactRepository_DeleteByHistoricalRef_DoesNotTombstoneHead(t *testing.T) {
	repo := newArtifactRepo(t)
	ctx := context.Background()

	firstData := []byte("first")
	firstRef := "20260216T120010Z-aaaaaaaaaaaaaaaa"
	firstOut := mustSaveVersion(t, ctx, repo, firstRef, "plan/delete-history", "text/plain; charset=utf-8", firstData, time.Now(), artifacts.SaveOptions{})

	secondData := []byte("second")
	secondOut := mustSaveVersion(t, ctx, repo, "20260216T120011Z-bbbbbbbbbbbbbbbb", "plan/delete-history", "text/plain; charset=utf-8", secondData, time.Now().Add(time.Second), artifacts.SaveOptions{ExpectedPrevRef: firstOut.Ref})

	_, err := repo.Delete(ctx, artifacts.Selector{Ref: firstOut.Ref})
	if !errors.Is(err, artifacts.ErrNotFound) {
		t.Fatalf("expected delete by historical ref to return ErrNotFound, got %v", err)
	}

	resolved, err := repo.Resolve(ctx, "plan/delete-history")
	if err != nil {
		t.Fatalf("resolve after historical delete attempt: %v", err)
	}
	if resolved != secondOut.Ref {
		t.Fatalf("expected head ref to remain %q, got %q", secondOut.Ref, resolved)
	}
}

func TestArtifactRepository_DeleteByHeadRef_Tombstones(t *testing.T) {
	repo := newArtifactRepo(t)
	ctx := context.Background()

	data := []byte("head")
	out := mustSaveVersion(t, ctx, repo, "20260216T120012Z-cccccccccccccccc", "plan/delete-head", "text/plain; charset=utf-8", data, time.Now(), artifacts.SaveOptions{})

	tomb, err := repo.Delete(ctx, artifacts.Selector{Ref: out.Ref})
	if err != nil {
		t.Fatalf("delete by head ref: %v", err)
	}
	if !tomb.Tombstone {
		t.Fatalf("expected tombstone result, got %+v", tomb)
	}
	if tomb.PrevRef != out.Ref {
		t.Fatalf("expected tombstone prevRef=%q, got %q", out.Ref, tomb.PrevRef)
	}

	if _, err := repo.Resolve(ctx, "plan/delete-head"); !errors.Is(err, artifacts.ErrNotFound) {
		t.Fatalf("expected resolve ErrNotFound after delete-by-ref tombstone, got %v", err)
	}
}

func TestArtifactRepository_List_LiteralWildcardPrefixes(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name       string
		prefix     string
		literal    string
		nonMatch   string
		matchRef   string
		otherRef   string
		wantListed string
	}{
		{
			name:       "percent",
			prefix:     "plan/%",
			literal:    "plan/%literal",
			nonMatch:   "plan/abc",
			matchRef:   "20260216T120013Z-dddddddddddddddd",
			otherRef:   "20260216T120014Z-eeeeeeeeeeeeeeee",
			wantListed: "plan/%literal",
		},
		{
			name:       "underscore",
			prefix:     "plan/_",
			literal:    "plan/_literal",
			nonMatch:   "plan/abc",
			matchRef:   "20260216T120015Z-ffffffffffffffff",
			otherRef:   "20260216T120016Z-0000000000000000",
			wantListed: "plan/_literal",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			repo := newArtifactRepo(t)
			_ = mustSaveVersion(t, ctx, repo, tc.matchRef, tc.literal, "text/plain; charset=utf-8", []byte("a"), time.Now(), artifacts.SaveOptions{})
			_ = mustSaveVersion(t, ctx, repo, tc.otherRef, tc.nonMatch, "text/plain; charset=utf-8", []byte("b"), time.Now().Add(time.Second), artifacts.SaveOptions{})

			listed, err := repo.List(ctx, tc.prefix, 10)
			if err != nil {
				t.Fatalf("list with literal wildcard prefix %q: %v", tc.prefix, err)
			}
			if len(listed) != 1 || listed[0].Name != tc.wantListed {
				t.Fatalf("expected only %s, got %+v", tc.wantListed, listed)
			}
		})
	}
}

func TestEscapeLikePrefix_EscapesSpecialCharacters(t *testing.T) {
	got := escapeLikePrefix(`a%_b\\c`)
	want := `a\%\_b\\\\c`
	if got != want {
		t.Fatalf("escapeLikePrefix mismatch: got=%q want=%q", got, want)
	}
}
