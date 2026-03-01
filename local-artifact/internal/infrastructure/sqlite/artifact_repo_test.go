package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/artifacts"
)

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

func TestArtifactRepository_SaveGetResolveListDeleteListVersions(t *testing.T) {
	repo, err := NewArtifactRepository(t.TempDir())
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}
	t.Cleanup(func() {
		_ = repo.Close()
	})
	ctx := context.Background()

	firstData := []byte("first")
	first := makeVersion("20260216T120000Z-aaaaaaaaaaaaaaaa", "plan/task-1", "text/plain; charset=utf-8", firstData, time.Now())
	firstOut, err := repo.Save(ctx, first, firstData, artifacts.SaveOptions{})
	if err != nil {
		t.Fatalf("save first: %v", err)
	}
	if firstOut.PrevRef != "" {
		t.Fatalf("expected first prevRef empty, got %q", firstOut.PrevRef)
	}

	secondData := []byte("second")
	second := makeVersion("20260216T120001Z-bbbbbbbbbbbbbbbb", "plan/task-1", "text/plain; charset=utf-8", secondData, time.Now().Add(time.Second))
	secondOut, err := repo.Save(ctx, second, secondData, artifacts.SaveOptions{ExpectedPrevRef: first.Ref})
	if err != nil {
		t.Fatalf("save second: %v", err)
	}
	if secondOut.PrevRef != first.Ref {
		t.Fatalf("expected second prevRef=%q, got %q", first.Ref, secondOut.PrevRef)
	}

	resolved, err := repo.Resolve(ctx, "plan/task-1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved != second.Ref {
		t.Fatalf("expected latest ref=%q got=%q", second.Ref, resolved)
	}

	oldMeta, oldData, err := repo.Get(ctx, artifacts.Selector{Ref: first.Ref})
	if err != nil {
		t.Fatalf("get old by ref: %v", err)
	}
	if oldMeta.Ref != first.Ref || string(oldData) != "first" {
		t.Fatalf("unexpected old payload: meta=%+v data=%q", oldMeta, string(oldData))
	}

	latestMeta, latestData, err := repo.Get(ctx, artifacts.Selector{Name: "plan/task-1"})
	if err != nil {
		t.Fatalf("get latest by name: %v", err)
	}
	if latestMeta.Ref != second.Ref || string(latestData) != "second" {
		t.Fatalf("unexpected latest payload: meta=%+v data=%q", latestMeta, string(latestData))
	}

	listed, err := repo.List(ctx, "plan/", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 || listed[0].Ref != second.Ref {
		t.Fatalf("unexpected list result: %+v", listed)
	}

	versions, err := repo.ListVersions(ctx, "plan/task-1", 10)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions got %d", len(versions))
	}
	if versions[0].Ref != second.Ref || versions[1].Ref != first.Ref {
		t.Fatalf("unexpected version order: %+v", versions)
	}

	tomb, err := repo.Delete(ctx, artifacts.Selector{Name: "plan/task-1"})
	if err != nil {
		t.Fatalf("delete by name: %v", err)
	}
	if !tomb.Tombstone {
		t.Fatalf("expected tombstone delete result, got %+v", tomb)
	}
	if tomb.PrevRef != second.Ref {
		t.Fatalf("expected tombstone prevRef=%q, got %q", second.Ref, tomb.PrevRef)
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
	repo, err := NewArtifactRepository(t.TempDir())
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}
	t.Cleanup(func() {
		_ = repo.Close()
	})
	ctx := context.Background()
	createdAt := time.Date(2026, 2, 16, 12, 0, 0, 0, time.UTC)

	refs := []string{
		"20260216T120000Z-ffffffffffffffff",
		"20260216T120000Z-1111111111111111",
		"20260216T120000Z-0000000000000000",
	}
	payloads := [][]byte{[]byte("first"), []byte("second"), []byte("third")}

	first := makeVersion(refs[0], "plan/same-second", "text/plain; charset=utf-8", payloads[0], createdAt)
	firstOut, err := repo.Save(ctx, first, payloads[0], artifacts.SaveOptions{})
	if err != nil {
		t.Fatalf("save first: %v", err)
	}

	second := makeVersion(refs[1], "plan/same-second", "text/plain; charset=utf-8", payloads[1], createdAt)
	secondOut, err := repo.Save(ctx, second, payloads[1], artifacts.SaveOptions{ExpectedPrevRef: firstOut.Ref})
	if err != nil {
		t.Fatalf("save second: %v", err)
	}

	third := makeVersion(refs[2], "plan/same-second", "text/plain; charset=utf-8", payloads[2], createdAt)
	thirdOut, err := repo.Save(ctx, third, payloads[2], artifacts.SaveOptions{ExpectedPrevRef: secondOut.Ref})
	if err != nil {
		t.Fatalf("save third: %v", err)
	}

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
	repo, err := NewArtifactRepository(t.TempDir())
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}
	t.Cleanup(func() {
		_ = repo.Close()
	})
	ctx := context.Background()

	firstData := []byte("first")
	first := makeVersion("20260216T120010Z-aaaaaaaaaaaaaaaa", "plan/delete-history", "text/plain; charset=utf-8", firstData, time.Now())
	firstOut, err := repo.Save(ctx, first, firstData, artifacts.SaveOptions{})
	if err != nil {
		t.Fatalf("save first: %v", err)
	}

	secondData := []byte("second")
	second := makeVersion("20260216T120011Z-bbbbbbbbbbbbbbbb", "plan/delete-history", "text/plain; charset=utf-8", secondData, time.Now().Add(time.Second))
	secondOut, err := repo.Save(ctx, second, secondData, artifacts.SaveOptions{ExpectedPrevRef: firstOut.Ref})
	if err != nil {
		t.Fatalf("save second: %v", err)
	}

	_, err = repo.Delete(ctx, artifacts.Selector{Ref: firstOut.Ref})
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
	repo, err := NewArtifactRepository(t.TempDir())
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}
	t.Cleanup(func() {
		_ = repo.Close()
	})
	ctx := context.Background()

	data := []byte("head")
	version := makeVersion("20260216T120012Z-cccccccccccccccc", "plan/delete-head", "text/plain; charset=utf-8", data, time.Now())
	out, err := repo.Save(ctx, version, data, artifacts.SaveOptions{})
	if err != nil {
		t.Fatalf("save: %v", err)
	}

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

func TestArtifactRepository_List_LiteralPercentPrefix(t *testing.T) {
	repo, err := NewArtifactRepository(t.TempDir())
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}
	t.Cleanup(func() {
		_ = repo.Close()
	})
	ctx := context.Background()

	dataA := []byte("a")
	artA := makeVersion("20260216T120013Z-dddddddddddddddd", "plan/%literal", "text/plain; charset=utf-8", dataA, time.Now())
	if _, err := repo.Save(ctx, artA, dataA, artifacts.SaveOptions{}); err != nil {
		t.Fatalf("save percent literal: %v", err)
	}

	dataB := []byte("b")
	artB := makeVersion("20260216T120014Z-eeeeeeeeeeeeeeee", "plan/abc", "text/plain; charset=utf-8", dataB, time.Now().Add(time.Second))
	if _, err := repo.Save(ctx, artB, dataB, artifacts.SaveOptions{}); err != nil {
		t.Fatalf("save non-matching: %v", err)
	}

	listed, err := repo.List(ctx, "plan/%", 10)
	if err != nil {
		t.Fatalf("list with percent literal prefix: %v", err)
	}
	if len(listed) != 1 || listed[0].Name != "plan/%literal" {
		t.Fatalf("expected only plan/%%literal, got %+v", listed)
	}
}

func TestArtifactRepository_List_LiteralUnderscorePrefix(t *testing.T) {
	repo, err := NewArtifactRepository(t.TempDir())
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}
	t.Cleanup(func() {
		_ = repo.Close()
	})
	ctx := context.Background()

	dataA := []byte("a")
	artA := makeVersion("20260216T120015Z-ffffffffffffffff", "plan/_literal", "text/plain; charset=utf-8", dataA, time.Now())
	if _, err := repo.Save(ctx, artA, dataA, artifacts.SaveOptions{}); err != nil {
		t.Fatalf("save underscore literal: %v", err)
	}

	dataB := []byte("b")
	artB := makeVersion("20260216T120016Z-0000000000000000", "plan/abc", "text/plain; charset=utf-8", dataB, time.Now().Add(time.Second))
	if _, err := repo.Save(ctx, artB, dataB, artifacts.SaveOptions{}); err != nil {
		t.Fatalf("save non-matching: %v", err)
	}

	listed, err := repo.List(ctx, "plan/_", 10)
	if err != nil {
		t.Fatalf("list with underscore literal prefix: %v", err)
	}
	if len(listed) != 1 || listed[0].Name != "plan/_literal" {
		t.Fatalf("expected only plan/_literal, got %+v", listed)
	}
}

func TestEscapeLikePrefix_EscapesSpecialCharacters(t *testing.T) {
	got := escapeLikePrefix(`a%_b\\c`)
	want := `a\%\_b\\\\c`
	if got != want {
		t.Fatalf("escapeLikePrefix mismatch: got=%q want=%q", got, want)
	}
}
