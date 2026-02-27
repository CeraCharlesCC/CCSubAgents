package installer

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/integration/txn"
)

func TestRunGlobal_UsesEngineExecuteForLifecycleCommands(t *testing.T) {
	original := engineExecute
	t.Cleanup(func() { engineExecute = original })

	wantErr := errors.New("engine-called")
	seen := map[string]txn.Plan{}
	engineExecute = func(_ context.Context, _ txn.Engine, plan txn.Plan) error {
		seen[plan.Command] = plan
		return wantErr
	}

	r := &Runner{
		homeDir: func() (string, error) { return t.TempDir(), nil },
		getenv:  func(string) string { return "" },
	}

	for _, command := range []Command{CommandInstall, CommandUpdate, CommandUninstall} {
		err := r.runGlobal(context.Background(), command)
		if !errors.Is(err, wantErr) {
			t.Fatalf("expected engine sentinel for %s, got %v", command, err)
		}
	}

	for _, command := range []Command{CommandInstall, CommandUpdate, CommandUninstall} {
		key := "global-" + string(command)
		plan, ok := seen[key]
		if !ok {
			t.Fatalf("missing engine plan for command %q", key)
		}
		if plan.ScopeID != "global-flow" {
			t.Fatalf("scope mismatch for %q: got=%q want=%q", key, plan.ScopeID, "global-flow")
		}
		if len(plan.Steps) != 1 {
			t.Fatalf("expected one step for %q, got %d", key, len(plan.Steps))
		}
	}
}

func TestRunLocal_UsesEngineExecuteForLifecycleCommands(t *testing.T) {
	original := engineExecute
	t.Cleanup(func() { engineExecute = original })

	wantErr := errors.New("engine-called")
	seen := map[string]txn.Plan{}
	engineExecute = func(_ context.Context, _ txn.Engine, plan txn.Plan) error {
		seen[plan.Command] = plan
		return wantErr
	}

	r := &Runner{
		homeDir: func() (string, error) { return t.TempDir(), nil },
		getenv:  os.Getenv,
	}

	for _, command := range []Command{CommandInstall, CommandUpdate, CommandUninstall} {
		err := r.runLocal(context.Background(), command)
		if !errors.Is(err, wantErr) {
			t.Fatalf("expected engine sentinel for %s, got %v", command, err)
		}
	}

	for _, command := range []Command{CommandInstall, CommandUpdate, CommandUninstall} {
		key := "local-" + string(command)
		plan, ok := seen[key]
		if !ok {
			t.Fatalf("missing engine plan for command %q", key)
		}
		if plan.ScopeID != "local-flow" {
			t.Fatalf("scope mismatch for %q: got=%q want=%q", key, plan.ScopeID, "local-flow")
		}
		if len(plan.Steps) != 1 {
			t.Fatalf("expected one step for %q, got %d", key, len(plan.Steps))
		}
	}
}
