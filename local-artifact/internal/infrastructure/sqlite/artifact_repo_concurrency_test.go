package sqlite

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/artifacts"
)

func TestArtifactRepository_ConcurrentExpectedPrevRefOneConflict(t *testing.T) {
	repo, err := NewArtifactRepository(t.TempDir())
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}
	t.Cleanup(func() {
		_ = repo.Close()
	})
	ctx := context.Background()

	seedData := []byte("seed")
	seed := makeVersion("20260216T120010Z-aaaaaaaaaaaaaaaa", "plan/task-race", "text/plain; charset=utf-8", seedData, time.Now())
	if _, err := repo.Save(ctx, seed, seedData, artifacts.SaveOptions{}); err != nil {
		t.Fatalf("seed save: %v", err)
	}

	type result struct {
		out artifacts.ArtifactVersion
		err error
	}
	results := make([]result, 2)
	refs := []string{"20260216T120011Z-bbbbbbbbbbbbbbbb", "20260216T120012Z-cccccccccccccccc"}
	payloads := [][]byte{[]byte("second-a"), []byte("second-b")}

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range refs {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			v := makeVersion(refs[i], "plan/task-race", "text/plain; charset=utf-8", payloads[i], time.Now().Add(time.Duration(i+1)*time.Second))
			out, err := repo.Save(ctx, v, payloads[i], artifacts.SaveOptions{ExpectedPrevRef: seed.Ref})
			results[i] = result{out: out, err: err}
		}()
	}
	close(start)
	wg.Wait()

	successes := 0
	conflicts := 0
	for _, r := range results {
		switch {
		case r.err == nil:
			successes++
			if r.out.PrevRef != seed.Ref {
				t.Fatalf("successful save expected prevRef=%q got=%q", seed.Ref, r.out.PrevRef)
			}
		case errors.Is(r.err, artifacts.ErrConflict):
			conflicts++
		default:
			t.Fatalf("unexpected save error: %v", r.err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("expected one success and one conflict, got successes=%d conflicts=%d", successes, conflicts)
	}
}
