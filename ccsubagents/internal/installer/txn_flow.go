package installer

import (
	"context"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/integration/txn"
)

var engineExecute = func(ctx context.Context, engine txn.Engine, plan txn.Plan) error {
	return engine.Execute(ctx, plan)
}

func executeFlowWithEngine(ctx context.Context, stateDir, blobDir, scopeID, command, stepID string, apply func(context.Context) error) error {
	engine := txn.Engine{StateDir: stateDir, BlobDir: blobDir}
	return engineExecute(ctx, engine, txn.Plan{
		ScopeID: scopeID,
		Command: command,
		Steps: []txn.Step{{
			ID: stepID,
			Apply: func(stepCtx context.Context, session *txn.Session) error {
				if err := session.MarkApplied(stepID); err != nil {
					return err
				}
				if apply == nil {
					return nil
				}
				return apply(stepCtx)
			},
		}},
	})
}
