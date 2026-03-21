package mcp

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"
)

var todoToolNames = []string{toolArtifactTodo, "artifact.todo"}

func TestTodoTool_ReadMissingSupportsCanonicalAndAlias(t *testing.T) {
	ctx := context.Background()
	s := newDaemonBackedServer(t)

	for _, toolName := range todoToolNames {
		toolName := toolName
		t.Run(toolName, func(t *testing.T) {
			resp := requireToolOK(t, callToolsCall(t, s, ctx, toolName, map[string]any{
				"operation": "read",
				"artifact":  map[string]any{"name": "plan/task-registry-path"},
			}))
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
			requireContentTextEq(t, resp, "todo list not found; returning empty list")
		})
	}
}

func TestTodoTool_WriteThenReadRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newDaemonBackedServer(t)

	for idx, toolName := range todoToolNames {
		toolName := toolName
		t.Run(toolName, func(t *testing.T) {
			artifactName := fmt.Sprintf("plan/task-123-roundtrip-%d", idx)

			writeResp := requireToolOK(t, callToolsCall(t, s, ctx, toolName, map[string]any{
				"operation": "write",
				"artifact":  map[string]any{"name": artifactName},
				"todoList": []map[string]any{
					{"id": 1, "title": "Draft plan", "status": "not-started"},
					{"id": 2, "title": "Implement", "status": "in-progress"},
				},
			}))
			writeOut, ok := writeResp.StructuredContent.(todoOut)
			if !ok {
				t.Fatalf("%s write expected todoOut structured content, got %T", toolName, writeResp.StructuredContent)
			}
			if !writeOut.Exists || writeOut.Ref == "" || writeOut.URIByRef == "" || writeOut.URIByName == "" {
				t.Fatalf("%s unexpected write metadata: %+v", toolName, writeOut)
			}
			if writeOut.Name != artifactName+"/todo" {
				t.Fatalf("%s unexpected todo artifact name: %q", toolName, writeOut.Name)
			}
			if len(writeOut.TodoList) != 2 {
				t.Fatalf("%s expected two todo items, got %d", toolName, len(writeOut.TodoList))
			}
			requireContentTextEq(t, writeResp, "todo list saved (2 items)")

			readResp := requireToolOK(t, callToolsCall(t, s, ctx, toolName, map[string]any{
				"operation": "read",
				"artifact":  map[string]any{"name": artifactName},
			}))
			readOut, ok := readResp.StructuredContent.(todoOut)
			if !ok {
				t.Fatalf("%s read expected todoOut structured content, got %T", toolName, readResp.StructuredContent)
			}
			if !readOut.Exists || readOut.Ref != writeOut.Ref {
				t.Fatalf("%s unexpected read metadata: %+v", toolName, readOut)
			}
			if len(readOut.TodoList) != 2 || readOut.TodoList[1].Status != "in-progress" {
				t.Fatalf("%s unexpected read todo list: %+v", toolName, readOut.TodoList)
			}
			requireContentTextEq(t, readResp, "todo list loaded (2 items)")
		})
	}
}

func TestToolTodo_WriteWithRefSelectorResolvesBaseName(t *testing.T) {
	ctx := context.Background()
	s := newDaemonBackedServer(t)

	baseRespAny, rpcErr := s.toolSaveText(ctx, mustRawJSON(t, map[string]any{
		"name": "plan/task-by-ref",
		"text": "base",
	}))
	if rpcErr != nil {
		t.Fatalf("base save rpc error: %+v", rpcErr)
	}
	baseResp := requireToolOK(t, requireToolResult(t, baseRespAny))
	baseOut := requireSaveOut(t, baseResp.StructuredContent)

	for _, toolName := range todoToolNames {
		toolName := toolName
		t.Run(toolName, func(t *testing.T) {
			writeResp := requireToolOK(t, callToolsCall(t, s, ctx, toolName, map[string]any{
				"operation": "write",
				"artifact":  map[string]any{"ref": baseOut.Ref},
				"todoList": []map[string]any{
					{"id": 1, "title": "Linked task", "status": "completed"},
				},
			}))
			writeOut, ok := writeResp.StructuredContent.(todoOut)
			if !ok {
				t.Fatalf("%s expected todoOut structured content, got %T", toolName, writeResp.StructuredContent)
			}
			if writeOut.Name != "plan/task-by-ref/todo" {
				t.Fatalf("%s unexpected resolved todo name: %q", toolName, writeOut.Name)
			}
		})
	}
}

func TestToolTodo_StaleExpectedPrevRefReturnsConflictAndIsNonMutating(t *testing.T) {
	ctx := context.Background()

	for _, toolName := range todoToolNames {
		toolName := toolName
		t.Run(toolName, func(t *testing.T) {
			s := newDaemonBackedServer(t)
			firstResp := requireToolOK(t, callToolsCall(t, s, ctx, toolName, map[string]any{
				"operation": "write",
				"artifact":  map[string]any{"name": "plan/task-guard"},
				"todoList":  []map[string]any{{"id": 1, "title": "First", "status": "not-started"}},
			}))
			firstOut, ok := firstResp.StructuredContent.(todoOut)
			if !ok {
				t.Fatalf("%s expected todoOut structured content, got %T", toolName, firstResp.StructuredContent)
			}

			secondResp := requireToolErr(t, callToolsCall(t, s, ctx, toolName, map[string]any{
				"operation":       "write",
				"artifact":        map[string]any{"name": "plan/task-guard"},
				"todoList":        []map[string]any{{"id": 1, "title": "Second", "status": "in-progress"}},
				"expectedPrevRef": "20260216T101019Z-cccccccccccccccc",
			}))
			requireContentTextContains(t, secondResp, "conflict")

			readResp := requireToolOK(t, callToolsCall(t, s, ctx, toolName, map[string]any{
				"operation": "read",
				"artifact":  map[string]any{"name": "plan/task-guard"},
			}))
			readOut, ok := readResp.StructuredContent.(todoOut)
			if !ok {
				t.Fatalf("%s expected todoOut structured content, got %T", toolName, readResp.StructuredContent)
			}
			if readOut.Ref != firstOut.Ref {
				t.Fatalf("%s expected latest ref to remain %q, got %q", toolName, firstOut.Ref, readOut.Ref)
			}
		})
	}
}

func TestTodoTool_WriteWithoutTodoListReturnsInvalidArgumentsAndIsNonMutating(t *testing.T) {
	ctx := context.Background()
	s := newDaemonBackedServer(t)

	for _, toolName := range todoToolNames {
		toolName := toolName
		t.Run(toolName, func(t *testing.T) {
			writeResp := requireToolErr(t, callToolsCall(t, s, ctx, toolName, map[string]any{
				"operation": "write",
				"artifact":  map[string]any{"name": "plan/task-no-list"},
			}))
			requireContentTextEq(t, writeResp, todoInvalidArgumentsMessage)

			readResp := requireToolOK(t, callToolsCall(t, s, ctx, toolName, map[string]any{
				"operation": "read",
				"artifact":  map[string]any{"name": "plan/task-no-list"},
			}))
			readOut, ok := readResp.StructuredContent.(todoOut)
			if !ok {
				t.Fatalf("%s expected todoOut structured content, got %T", toolName, readResp.StructuredContent)
			}
			if readOut.Exists {
				t.Fatalf("%s expected todo artifact to remain absent after failed write, got %+v", toolName, readOut)
			}
		})
	}
}

func TestTodoTool_InvalidArguments_UnknownFields(t *testing.T) {
	ctx := context.Background()
	s := newDaemonBackedServer(t)

	testCases := []struct {
		name string
		args map[string]any
	}{
		{
			name: "unknown top-level field",
			args: map[string]any{
				"operation": "read",
				"artifact":  map[string]any{"name": "plan/task-unknown-top"},
				"unknown":   true,
			},
		},
		{
			name: "unknown artifact selector field",
			args: map[string]any{
				"operation": "read",
				"artifact": map[string]any{
					"name":    "plan/task-unknown-artifact",
					"unknown": "x",
				},
			},
		},
		{
			name: "unknown todo item field",
			args: map[string]any{
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
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		for _, toolName := range todoToolNames {
			toolName := toolName
			t.Run(tc.name+"/"+toolName, func(t *testing.T) {
				resp := requireToolErr(t, callToolsCall(t, s, ctx, toolName, tc.args))
				requireContentTextEq(t, resp, todoInvalidArgumentsMessage)
			})
		}
	}
}

func TestToolTodo_WriteRejectsOmittedRequiredTodoItemFields(t *testing.T) {
	ctx := context.Background()
	s := newDaemonBackedServer(t)

	testCases := []struct {
		name string
		item map[string]any
	}{
		{
			name: "missing id",
			item: map[string]any{"title": "Task", "status": "not-started"},
		},
		{
			name: "missing title",
			item: map[string]any{"id": 1, "status": "not-started"},
		},
		{
			name: "missing status",
			item: map[string]any{"id": 1, "title": "Task"},
		},
	}

	for _, tc := range testCases {
		tc := tc
		for _, toolName := range todoToolNames {
			toolName := toolName
			t.Run(tc.name+"/"+toolName, func(t *testing.T) {
				resp := requireToolErr(t, callToolsCall(t, s, ctx, toolName, map[string]any{
					"operation": "write",
					"artifact":  map[string]any{"name": "plan/task-missing-required-field"},
					"todoList":  []map[string]any{tc.item},
				}))
				requireContentTextEq(t, resp, todoInvalidArgumentsMessage)
			})
		}
	}
}

func TestToolTodo_WriteRejectsNullTodoItemPayload(t *testing.T) {
	ctx := context.Background()
	s := newDaemonBackedServer(t)

	for _, toolName := range todoToolNames {
		toolName := toolName
		t.Run(toolName, func(t *testing.T) {
			resp := requireToolErr(t, callToolsCall(t, s, ctx, toolName, map[string]any{
				"operation": "write",
				"artifact":  map[string]any{"name": "plan/task-null-item"},
				"todoList":  []any{nil},
			}))
			requireContentTextEq(t, resp, todoInvalidArgumentsMessage)
		})
	}
}

func TestToolTodo_ReadMalformedStoredTODOJSONReturnsError(t *testing.T) {
	ctx := context.Background()
	s := newDaemonBackedServer(t)

	saveRespAny, rpcErr := s.toolSaveBlob(ctx, mustRawJSON(t, map[string]any{
		"name":       "plan/task-malformed/todo",
		"mimeType":   "application/json; charset=utf-8",
		"dataBase64": base64.StdEncoding.EncodeToString([]byte("{")),
	}))
	if rpcErr != nil {
		t.Fatalf("save blob rpc error: %+v", rpcErr)
	}
	saveResp := requireToolResult(t, saveRespAny)
	if saveResp.IsError {
		t.Fatalf("save blob unexpectedly returned tool error: %+v", saveResp)
	}

	for _, toolName := range todoToolNames {
		toolName := toolName
		t.Run(toolName, func(t *testing.T) {
			readResp := requireToolErr(t, callToolsCall(t, s, ctx, toolName, map[string]any{
				"operation": "read",
				"artifact":  map[string]any{"name": "plan/task-malformed"},
			}))
			requireContentTextContains(t, readResp, "internal error: invalid stored todo artifact")
		})
	}
}

func TestToolTodo_ValidationErrors(t *testing.T) {
	ctx := context.Background()
	s := newDaemonBackedServer(t)

	cases := []struct {
		name                 string
		args                 map[string]any
		expectInvalidArgsErr bool
	}{
		{
			name: "empty title is rejected by handler validation",
			args: map[string]any{
				"operation": "write",
				"artifact":  map[string]any{"name": "plan/task-invalid"},
				"todoList":  []map[string]any{{"id": 1, "title": "", "status": "not-started"}},
			},
		},
		{
			name: "invalid status is rejected by schema validation",
			args: map[string]any{
				"operation": "write",
				"artifact":  map[string]any{"name": "plan/task-invalid"},
				"todoList":  []map[string]any{{"id": 1, "title": "A", "status": "done"}},
			},
			expectInvalidArgsErr: true,
		},
		{
			name: "duplicate IDs are rejected by handler validation",
			args: map[string]any{
				"operation": "write",
				"artifact":  map[string]any{"name": "plan/task-invalid"},
				"todoList": []map[string]any{
					{"id": 1, "title": "A", "status": "not-started"},
					{"id": 1, "title": "B", "status": "completed"},
				},
			},
		},
		{
			name: "name and ref conflict is rejected by handler validation",
			args: map[string]any{
				"operation": "write",
				"artifact":  map[string]any{"name": "plan/task-invalid", "ref": "20260216T101019Z-cccccccccccccccc"},
				"todoList":  []map[string]any{{"id": 1, "title": "A", "status": "not-started"}},
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		for _, toolName := range todoToolNames {
			toolName := toolName
			t.Run(tc.name+"/"+toolName, func(t *testing.T) {
				resp := requireToolErr(t, callToolsCall(t, s, ctx, toolName, tc.args))
				if tc.expectInvalidArgsErr {
					requireContentTextEq(t, resp, todoInvalidArgumentsMessage)
					return
				}
				requireContentTextContains(t, resp, "invalid input")
			})
		}
	}
}

func TestToolsList_ExposesTodoDefinitionWithStrictNestedSchemas(t *testing.T) {
	toolsResp, rpcErr := newDaemonBackedServer(t).handleToolsList(nil)
	if rpcErr != nil {
		t.Fatalf("tools/list error: %+v", rpcErr)
	}
	tools := requireToolDefs(t, requireMap(t, toolsResp, "tools/list response")["tools"])

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
	artifactProp := requireMap(t, requireMap(t, todo.InputSchema["properties"], "todo input properties")["artifact"], "artifact property")
	if artifactProp["additionalProperties"] != false {
		t.Fatalf("expected strict artifact selector schema, got %+v", artifactProp)
	}
	itemSchema := requireMap(t, requireMap(t, requireMap(t, todo.InputSchema["properties"], "todo input properties")["todoList"], "todoList property")["items"], "todo item schema")
	if itemSchema["additionalProperties"] != false {
		t.Fatalf("expected strict todo item schema, got %+v", itemSchema)
	}
}
