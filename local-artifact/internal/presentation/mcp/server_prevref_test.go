package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

func TestServerSaveResolveListAndGetPrevRefVersioning(t *testing.T) {
	ctx := context.Background()
	s := New(t.TempDir())

	firstRespAny, rpcErr := s.toolSaveText(ctx, mustRawJSON(t, map[string]any{"name": "plan/task-123", "text": "first"}))
	if rpcErr != nil {
		t.Fatalf("first save rpc error: %+v", rpcErr)
	}
	firstResp := firstRespAny.(toolResult)
	if firstResp.IsError {
		t.Fatalf("first save unexpectedly returned tool error: %+v", firstResp)
	}
	firstOut := firstResp.StructuredContent.(saveOut)
	if firstOut.PrevRef != "" {
		t.Fatalf("expected first prevRef empty, got %q", firstOut.PrevRef)
	}

	secondRespAny, rpcErr := s.toolSaveText(ctx, mustRawJSON(t, map[string]any{"name": "plan/task-123", "text": "second"}))
	if rpcErr != nil {
		t.Fatalf("second save rpc error: %+v", rpcErr)
	}
	secondResp := secondRespAny.(toolResult)
	if secondResp.IsError {
		t.Fatalf("second save unexpectedly returned tool error: %+v", secondResp)
	}
	secondOut := secondResp.StructuredContent.(saveOut)
	if secondOut.PrevRef != firstOut.Ref {
		t.Fatalf("expected second prevRef=%q, got %q", firstOut.Ref, secondOut.PrevRef)
	}

	resolveRespAny, rpcErr := s.toolResolve(ctx, mustRawJSON(t, map[string]any{"name": "plan/task-123"}))
	if rpcErr != nil {
		t.Fatalf("resolve rpc error: %+v", rpcErr)
	}
	resolveResp := resolveRespAny.(toolResult)
	if resolveResp.IsError {
		t.Fatalf("resolve unexpectedly returned tool error: %+v", resolveResp)
	}
	resolveOut := resolveResp.StructuredContent.(map[string]any)
	if resolveOut["ref"] != secondOut.Ref {
		t.Fatalf("expected resolve ref=%q, got %#v", secondOut.Ref, resolveOut["ref"])
	}

	oldGetRespAny, rpcErr := s.toolGet(ctx, mustRawJSON(t, map[string]any{"ref": firstOut.Ref, "mode": "meta"}))
	if rpcErr != nil {
		t.Fatalf("get old by ref rpc error: %+v", rpcErr)
	}
	oldGetResp := oldGetRespAny.(toolResult)
	if oldGetResp.IsError {
		t.Fatalf("get old by ref unexpectedly returned tool error: %+v", oldGetResp)
	}
	oldGetOut := oldGetResp.StructuredContent.(saveOut)
	if oldGetOut.Ref != firstOut.Ref {
		t.Fatalf("expected old get ref=%q, got %q", firstOut.Ref, oldGetOut.Ref)
	}

	listRespAny, rpcErr := s.toolList(ctx, nil)
	if rpcErr != nil {
		t.Fatalf("list rpc error: %+v", rpcErr)
	}
	listResp := listRespAny.(toolResult)
	if listResp.IsError {
		t.Fatalf("list unexpectedly returned tool error: %+v", listResp)
	}
	listOut := listResp.StructuredContent.(map[string]any)
	items, ok := listOut["items"].([]saveOut)
	if !ok {
		t.Fatalf("expected list items []saveOut, got %T", listOut["items"])
	}
	if len(items) != 1 {
		t.Fatalf("expected one listed artifact, got %d", len(items))
	}
	if items[0].Ref != secondOut.Ref || items[0].PrevRef != firstOut.Ref {
		t.Fatalf("unexpected listed artifact: %+v", items[0])
	}
}

func mustRawJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return b
}
