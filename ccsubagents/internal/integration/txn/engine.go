package txn

import (
	"context"
	"fmt"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/integration/action"
)

type Step struct {
	ID        string
	DependsOn []string
	Apply     func(context.Context, *Session) error
	Verify    func(context.Context, *Session) error
}

type Plan struct {
	ScopeID string
	Command string
	Steps   []Step
}

type Engine struct {
	StateDir string
	BlobDir  string
}

func (e Engine) Execute(ctx context.Context, plan Plan) error {
	if plan.ScopeID == "" {
		return fmt.Errorf("plan scope is required")
	}
	nodes := make([]action.Node, 0, len(plan.Steps))
	stepsByID := make(map[string]Step, len(plan.Steps))
	for _, step := range plan.Steps {
		nodes = append(nodes, action.Node{ID: step.ID, DependsOn: step.DependsOn})
		stepsByID[step.ID] = step
	}
	order, err := action.TopoSort(nodes)
	if err != nil {
		return err
	}
	session, err := Begin(e.StateDir, e.BlobDir, plan.ScopeID, plan.Command, order)
	if err != nil {
		return err
	}
	defer session.Close()

	for _, id := range order {
		step := stepsByID[id]
		if step.Apply != nil {
			if err := step.Apply(ctx, session); err != nil {
				_ = session.Rollback()
				return err
			}
		}
		if err := session.MarkApplied(id); err != nil {
			_ = session.Rollback()
			return err
		}
		if step.Verify != nil {
			if err := step.Verify(ctx, session); err != nil {
				_ = session.Rollback()
				return err
			}
		}
	}
	return session.Commit()
}
