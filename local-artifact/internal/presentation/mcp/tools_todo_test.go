package mcp

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
)

func TestToolsCall_TodoRegistryPathSupportsCanonicalAndAlias(t *testing.T) {
	ctx := context.Background()
	s := New(t.TempDir())

	for _, toolName := range []string{toolArtifactTodo, "artifact.todo"} {
		toolName := toolName
		t.Run(toolName, func(t *testing.T) {
			respAny, rpcErr := s.handleToolsCall(ctx, mustRawJSON(t, map[string]any{
				"name": toolName,
				"arguments": map[string]any{
					"operation": "read",
					"artifact":  map[string]any{"name": "plan/task-registry-path"},
				},
			}))
			if rpcErr != nil {
				t.Fatalf("tools/call %q rpc error: %+v", toolName, rpcErr)
			}

			resp := respAny.(toolResult)
			if resp.IsError {
				t.Fatalf("tools/call %q unexpectedly returned tool error: %+v", toolName, resp)
			}

			out, ok := resp.StructuredContent.(todoOut)
			if !ok {
				t.Fatalf("tools/call %q expected todoOut structured content, got %T", toolName, resp.StructuredContent)
			}
			if out.Exists {
				t.Fatalf("tools/call %q expected exists=false for missing todo artifact", toolName)
			}
			if out.Name != "plan/task-registry-path/todo" {
				t.Fatalf("tools/call %q unexpected todo artifact name: %q", toolName, out.Name)
			}
			if len(out.TodoList) != 0 {
				t.Fatalf("tools/call %q expected empty todoList, got %+v", toolName, out.TodoList)
			}
			if out.URIByName == "" {
				t.Fatalf("tools/call %q expected uriByName to be populated", toolName)
			}
		})
	}
}

func TestToolTodo_ReadMissingReturnsEmptyList(t *testing.T) {
	ctx := context.Background()
	s := New(t.TempDir())

	respAny, rpcErr := s.toolTodo(ctx, mustRawJSON(t, map[string]any{
		"operation": "read",
		"artifact":  map[string]any{"name": "plan/task-123"},
	}))
	if rpcErr != nil {
		t.Fatalf("todo read rpc error: %+v", rpcErr)
	}
	resp := respAny.(toolResult)
	if resp.IsError {
		t.Fatalf("todo read unexpectedly returned tool error: %+v", resp)
	}
	out := resp.StructuredContent.(todoOut)
	if out.Exists {
		t.Fatal("expected exists=false for missing todo artifact")
	}
	if len(out.TodoList) != 0 {
		t.Fatalf("expected empty todoList, got %+v", out.TodoList)
	}
	if out.Name != "plan/task-123/todo" {
		t.Fatalf("unexpected todo artifact name: %q", out.Name)
	}
	if got := firstContentText(resp); got != "todo list not found; returning empty list" {
		t.Fatalf("unexpected read-missing content text: %q", got)
	}
}

func TestToolTodo_WriteThenReadRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := New(t.TempDir())

	writeRespAny, rpcErr := s.toolTodo(ctx, mustRawJSON(t, map[string]any{
		"operation": "write",
		"artifact":  map[string]any{"name": "plan/task-123"},
		"todoList": []map[string]any{
			{"id": 1, "title": "Draft plan", "status": "not-started"},
			{"id": 2, "title": "Implement", "status": "in-progress"},
		},
	}))
	if rpcErr != nil {
		t.Fatalf("todo write rpc error: %+v", rpcErr)
	}
	writeResp := writeRespAny.(toolResult)
	if writeResp.IsError {
		t.Fatalf("todo write unexpectedly returned tool error: %+v", writeResp)
	}
	writeOut := writeResp.StructuredContent.(todoOut)
	if !writeOut.Exists || writeOut.Ref == "" || writeOut.URIByRef == "" || writeOut.URIByName == "" {
		t.Fatalf("unexpected write metadata: %+v", writeOut)
	}
	if writeOut.Name != "plan/task-123/todo" {
		t.Fatalf("unexpected todo artifact name: %q", writeOut.Name)
	}
	if len(writeOut.TodoList) != 2 {
		t.Fatalf("expected two todo items, got %d", len(writeOut.TodoList))
	}
	if got := firstContentText(writeResp); got != "todo list saved (2 items)" {
		t.Fatalf("unexpected write content text: %q", got)
	}

	readRespAny, rpcErr := s.toolTodo(ctx, mustRawJSON(t, map[string]any{
		"operation": "read",
		"artifact":  map[string]any{"name": "plan/task-123"},
	}))
	if rpcErr != nil {
		t.Fatalf("todo read rpc error: %+v", rpcErr)
	}
	readResp := readRespAny.(toolResult)
	if readResp.IsError {
		t.Fatalf("todo read unexpectedly returned tool error: %+v", readResp)
	}
	readOut := readResp.StructuredContent.(todoOut)
	if !readOut.Exists || readOut.Ref != writeOut.Ref {
		t.Fatalf("unexpected read metadata: %+v", readOut)
	}
	if len(readOut.TodoList) != 2 || readOut.TodoList[1].Status != "in-progress" {
		t.Fatalf("unexpected read todo list: %+v", readOut.TodoList)
	}
	if got := firstContentText(readResp); got != "todo list loaded (2 items)" {
		t.Fatalf("unexpected read content text: %q", got)
	}
}

func TestToolTodo_WriteWithRefSelectorResolvesBaseName(t *testing.T) {
	ctx := context.Background()
	s := New(t.TempDir())

	baseRespAny, rpcErr := s.toolSaveText(ctx, mustRawJSON(t, map[string]any{
		"name": "plan/task-by-ref",
		"text": "base",
	}))
	if rpcErr != nil {
		t.Fatalf("base save rpc error: %+v", rpcErr)
	}
	baseOut := baseRespAny.(toolResult).StructuredContent.(saveOut)

	writeRespAny, rpcErr := s.toolTodo(ctx, mustRawJSON(t, map[string]any{
		"operation": "write",
		"artifact":  map[string]any{"ref": baseOut.Ref},
		"todoList": []map[string]any{
			{"id": 1, "title": "Linked task", "status": "completed"},
		},
	}))
	if rpcErr != nil {
		t.Fatalf("todo write by ref rpc error: %+v", rpcErr)
	}
	writeResp := writeRespAny.(toolResult)
	if writeResp.IsError {
		t.Fatalf("todo write by ref unexpectedly returned tool error: %+v", writeResp)
	}
	writeOut := writeResp.StructuredContent.(todoOut)
	if writeOut.Name != "plan/task-by-ref/todo" {
		t.Fatalf("unexpected resolved todo name: %q", writeOut.Name)
	}
}

func TestToolTodo_StaleExpectedPrevRefReturnsConflict(t *testing.T) {
	ctx := context.Background()
	s := New(t.TempDir())

	firstAny, rpcErr := s.toolTodo(ctx, mustRawJSON(t, map[string]any{
		"operation": "write",
		"artifact":  map[string]any{"name": "plan/task-guard"},
		"todoList":  []map[string]any{{"id": 1, "title": "First", "status": "not-started"}},
	}))
	if rpcErr != nil {
		t.Fatalf("first todo write rpc error: %+v", rpcErr)
	}
	first := firstAny.(toolResult).StructuredContent.(todoOut)

	secondAny, rpcErr := s.toolTodo(ctx, mustRawJSON(t, map[string]any{
		"operation":       "write",
		"artifact":        map[string]any{"name": "plan/task-guard"},
		"todoList":        []map[string]any{{"id": 1, "title": "Second", "status": "in-progress"}},
		"expectedPrevRef": "20260216T101019Z-cccccccccccccccc",
	}))
	if rpcErr != nil {
		t.Fatalf("second todo write rpc error: %+v", rpcErr)
	}
	second := secondAny.(toolResult)
	if !second.IsError {
		t.Fatalf("expected stale expectedPrevRef to return tool error, got %+v", second)
	}
	if !contentContains(second, "conflict") {
		t.Fatalf("expected conflict message, got %+v", second.Content)
	}

	readAny, rpcErr := s.toolTodo(ctx, mustRawJSON(t, map[string]any{
		"operation": "read",
		"artifact":  map[string]any{"name": "plan/task-guard"},
	}))
	if rpcErr != nil {
		t.Fatalf("todo read rpc error: %+v", rpcErr)
	}
	readOut := readAny.(toolResult).StructuredContent.(todoOut)
	if readOut.Ref != first.Ref {
		t.Fatalf("expected latest ref to remain %q, got %q", first.Ref, readOut.Ref)
	}
}

func TestToolTodo_WriteWithoutTodoListReturnsInvalidInput(t *testing.T) {
	ctx := context.Background()
	s := New(t.TempDir())

	writeRespAny, rpcErr := s.toolTodo(ctx, mustRawJSON(t, map[string]any{
		"operation": "write",
		"artifact":  map[string]any{"name": "plan/task-no-list"},
	}))
	if rpcErr != nil {
		t.Fatalf("todo write rpc error: %+v", rpcErr)
	}
	writeResp := writeRespAny.(toolResult)
	if !writeResp.IsError {
		t.Fatalf("expected todo write without todoList to return tool error, got %+v", writeResp)
	}
	if !contentContains(writeResp, "invalid input") || !contentContains(writeResp, "todoList is required for write") {
		t.Fatalf("expected invalid-input todoList-required message, got %+v", writeResp.Content)
	}

	readRespAny, rpcErr := s.toolTodo(ctx, mustRawJSON(t, map[string]any{
		"operation": "read",
		"artifact":  map[string]any{"name": "plan/task-no-list"},
	}))
	if rpcErr != nil {
		t.Fatalf("todo read rpc error: %+v", rpcErr)
	}
	readResp := readRespAny.(toolResult)
	if readResp.IsError {
		t.Fatalf("todo read unexpectedly returned tool error after failed write: %+v", readResp)
	}
	readOut := readResp.StructuredContent.(todoOut)
	if readOut.Exists {
		t.Fatalf("expected todo artifact to remain absent after failed write, got %+v", readOut)
	}
}

func TestToolTodo_RejectsUnknownTopLevelField(t *testing.T) {
	ctx := context.Background()
	s := New(t.TempDir())

	respAny, rpcErr := s.toolTodo(ctx, mustRawJSON(t, map[string]any{
		"operation": "read",
		"artifact":  map[string]any{"name": "plan/task-unknown-top"},
		"unknown":   true,
	}))
	if rpcErr != nil {
		t.Fatalf("todo read rpc error: %+v", rpcErr)
	}

	resp := respAny.(toolResult)
	if !resp.IsError {
		t.Fatalf("expected unknown top-level field to return tool error, got %+v", resp)
	}

	const expected = "Invalid arguments: expected {operation, artifact, todoList?, expectedPrevRef?}"
	if firstContentText(resp) != expected {
		t.Fatalf("expected invalid-arguments contract %q, got %+v", expected, resp.Content)
	}
}

func TestToolTodo_RejectsUnknownArtifactSelectorField(t *testing.T) {
	ctx := context.Background()
	s := New(t.TempDir())

	respAny, rpcErr := s.toolTodo(ctx, mustRawJSON(t, map[string]any{
		"operation": "read",
		"artifact": map[string]any{
			"name":    "plan/task-unknown-artifact",
			"unknown": "x",
		},
	}))
	if rpcErr != nil {
		t.Fatalf("todo read rpc error: %+v", rpcErr)
	}

	resp := respAny.(toolResult)
	if !resp.IsError {
		t.Fatalf("expected unknown artifact field to return tool error, got %+v", resp)
	}

	const expected = "Invalid arguments: expected {operation, artifact, todoList?, expectedPrevRef?}"
	if firstContentText(resp) != expected {
		t.Fatalf("expected invalid-arguments contract %q, got %+v", expected, resp.Content)
	}
}

func TestToolTodo_RejectsUnknownTodoItemField(t *testing.T) {
	ctx := context.Background()
	s := New(t.TempDir())

	respAny, rpcErr := s.toolTodo(ctx, mustRawJSON(t, map[string]any{
		"operation": "write",
		"artifact":  map[string]any{"name": "plan/task-unknown-item"},
		"todoList": []map[string]any{
			{
				"id":      1,
				"title":   "Task",
				"status":  "not-started",
				"unknown": "x",
			},
		},
	}))
	if rpcErr != nil {
		t.Fatalf("todo write rpc error: %+v", rpcErr)
	}

	resp := respAny.(toolResult)
	if !resp.IsError {
		t.Fatalf("expected unknown todo item field to return tool error, got %+v", resp)
	}

	const expected = "Invalid arguments: expected {operation, artifact, todoList?, expectedPrevRef?}"
	if firstContentText(resp) != expected {
		t.Fatalf("expected invalid-arguments contract %q, got %+v", expected, resp.Content)
	}
}

func TestToolTodo_WriteRejectsOmittedRequiredTodoItemFields(t *testing.T) {
	ctx := context.Background()
	s := New(t.TempDir())

	testCases := []struct {
		name           string
		item           map[string]any
		messageSnippet string
	}{
		{
			name:           "missing id",
			item:           map[string]any{"title": "Task", "status": "not-started"},
			messageSnippet: "todoList[0].id is required",
		},
		{
			name:           "missing title",
			item:           map[string]any{"id": 1, "status": "not-started"},
			messageSnippet: "todoList[0].title is required",
		},
		{
			name:           "missing status",
			item:           map[string]any{"id": 1, "title": "Task"},
			messageSnippet: "todoList[0].status must be one of",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			respAny, rpcErr := s.toolTodo(ctx, mustRawJSON(t, map[string]any{
				"operation": "write",
				"artifact":  map[string]any{"name": "plan/task-missing-required-field"},
				"todoList":  []map[string]any{tc.item},
			}))
			if rpcErr != nil {
				t.Fatalf("todo write rpc error: %+v", rpcErr)
			}

			resp := respAny.(toolResult)
			if !resp.IsError {
				t.Fatalf("expected omitted required todo field to return tool error, got %+v", resp)
			}
			if !contentContains(resp, "invalid input") {
				t.Fatalf("expected invalid-input error for omitted required field, got %+v", resp.Content)
			}
			if !contentContains(resp, tc.messageSnippet) {
				t.Fatalf("expected detailed field validation message %q, got %+v", tc.messageSnippet, resp.Content)
			}
		})
	}
}

func TestToolTodo_WriteRejectsNullTodoItemPayload(t *testing.T) {
	ctx := context.Background()
	s := New(t.TempDir())

	respAny, rpcErr := s.toolTodo(ctx, mustRawJSON(t, map[string]any{
		"operation": "write",
		"artifact":  map[string]any{"name": "plan/task-null-item"},
		"todoList":  []any{nil},
	}))
	if rpcErr != nil {
		t.Fatalf("todo write rpc error: %+v", rpcErr)
	}

	resp := respAny.(toolResult)
	if !resp.IsError {
		t.Fatalf("expected null todo item payload to return tool error, got %+v", resp)
	}
	if !contentContains(resp, "invalid input") {
		t.Fatalf("expected invalid-input error for null todo item payload, got %+v", resp.Content)
	}
	if !contentContains(resp, "todoList[0].id is required") {
		t.Fatalf("expected required-field message for null todo item payload, got %+v", resp.Content)
	}
}

func TestToolsCall_TodoUnknownFieldRejectionParity_CanonicalAndAlias(t *testing.T) {
	ctx := context.Background()
	s := New(t.TempDir())

	const expected = "Invalid arguments: expected {operation, artifact, todoList?, expectedPrevRef?}"
	messages := map[string]string{}

	for _, toolName := range []string{toolArtifactTodo, "artifact.todo"} {
		toolName := toolName
		respAny, rpcErr := s.handleToolsCall(ctx, mustRawJSON(t, map[string]any{
			"name": toolName,
			"arguments": map[string]any{
				"operation": "read",
				"artifact":  map[string]any{"name": "plan/task-unknown-parity"},
				"unknown":   true,
			},
		}))
		if rpcErr != nil {
			t.Fatalf("tools/call %q rpc error: %+v", toolName, rpcErr)
		}

		resp := respAny.(toolResult)
		if !resp.IsError {
			t.Fatalf("tools/call %q expected tool error for unknown field, got %+v", toolName, resp)
		}

		msg := firstContentText(resp)
		if msg != expected {
			t.Fatalf("tools/call %q expected invalid-arguments contract %q, got %+v", toolName, expected, resp.Content)
		}
		messages[toolName] = msg
	}

	if messages[toolArtifactTodo] != messages["artifact.todo"] {
		t.Fatalf("expected canonical and alias todo invalid-arguments messages to match, got %q vs %q", messages[toolArtifactTodo], messages["artifact.todo"])
	}
}

func TestToolTodo_ReadMalformedStoredTODOJSONReturnsError(t *testing.T) {
	ctx := context.Background()
	s := New(t.TempDir())

	saveRespAny, rpcErr := s.toolSaveBlob(ctx, mustRawJSON(t, map[string]any{
		"name":       "plan/task-malformed/todo",
		"mimeType":   "application/json; charset=utf-8",
		"dataBase64": base64.StdEncoding.EncodeToString([]byte("{")),
	}))
	if rpcErr != nil {
		t.Fatalf("save blob rpc error: %+v", rpcErr)
	}
	saveResp := saveRespAny.(toolResult)
	if saveResp.IsError {
		t.Fatalf("save blob unexpectedly returned tool error: %+v", saveResp)
	}

	readRespAny, rpcErr := s.toolTodo(ctx, mustRawJSON(t, map[string]any{
		"operation": "read",
		"artifact":  map[string]any{"name": "plan/task-malformed"},
	}))
	if rpcErr != nil {
		t.Fatalf("todo read rpc error: %+v", rpcErr)
	}
	readResp := readRespAny.(toolResult)
	if !readResp.IsError {
		t.Fatalf("expected malformed stored todo payload to return tool error, got %+v", readResp)
	}
	if !contentContains(readResp, "internal error: invalid stored todo artifact") {
		t.Fatalf("expected deterministic malformed-stored-todo error, got %+v", readResp.Content)
	}
}

func TestToolTodo_ValidationErrors(t *testing.T) {
	ctx := context.Background()
	s := New(t.TempDir())

	cases := []map[string]any{
		{
			"operation": "write",
			"artifact":  map[string]any{"name": "plan/task-invalid"},
			"todoList":  []map[string]any{{"id": 1, "title": "", "status": "not-started"}},
		},
		{
			"operation": "write",
			"artifact":  map[string]any{"name": "plan/task-invalid"},
			"todoList":  []map[string]any{{"id": 1, "title": "A", "status": "done"}},
		},
		{
			"operation": "write",
			"artifact":  map[string]any{"name": "plan/task-invalid"},
			"todoList": []map[string]any{
				{"id": 1, "title": "A", "status": "not-started"},
				{"id": 1, "title": "B", "status": "completed"},
			},
		},
		{
			"operation": "write",
			"artifact":  map[string]any{"name": "plan/task-invalid", "ref": "20260216T101019Z-cccccccccccccccc"},
			"todoList":  []map[string]any{{"id": 1, "title": "A", "status": "not-started"}},
		},
	}

	for idx, tc := range cases {
		respAny, rpcErr := s.toolTodo(ctx, mustRawJSON(t, tc))
		if rpcErr != nil {
			t.Fatalf("case %d rpc error: %+v", idx, rpcErr)
		}
		resp := respAny.(toolResult)
		if !resp.IsError {
			t.Fatalf("case %d expected tool error, got %+v", idx, resp)
		}
		if !contentContains(resp, "invalid input") {
			t.Fatalf("case %d expected invalid input message, got %+v", idx, resp.Content)
		}
	}
}

func TestToolsList_ExposesTodoDefinitionWithStrictNestedSchemas(t *testing.T) {
	toolsResp, rpcErr := New(t.TempDir()).handleToolsList(nil)
	if rpcErr != nil {
		t.Fatalf("tools/list error: %+v", rpcErr)
	}
	tools := toolsResp.(map[string]any)["tools"].([]toolDef)

	var todo toolDef
	found := false
	for _, tool := range tools {
		if tool.Name == toolArtifactTodo {
			todo = tool
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected tools/list to include %q", toolArtifactTodo)
	}

	if todo.InputSchema["additionalProperties"] != false {
		t.Fatalf("expected strict top-level input schema, got %+v", todo.InputSchema)
	}
	artifactProp := todo.InputSchema["properties"].(map[string]any)["artifact"].(map[string]any)
	if artifactProp["additionalProperties"] != false {
		t.Fatalf("expected strict artifact selector schema, got %+v", artifactProp)
	}
	itemSchema := todo.InputSchema["properties"].(map[string]any)["todoList"].(map[string]any)["items"].(map[string]any)
	if itemSchema["additionalProperties"] != false {
		t.Fatalf("expected strict todo item schema, got %+v", itemSchema)
	}
}

func contentContains(result toolResult, needle string) bool {
	for _, entry := range result.Content {
		contentMap, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		text, _ := contentMap["text"].(string)
		if strings.Contains(strings.ToLower(text), strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

func firstContentText(result toolResult) string {
	for _, entry := range result.Content {
		contentMap, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		text, _ := contentMap["text"].(string)
		if text != "" {
			return text
		}
	}
	return ""
}
