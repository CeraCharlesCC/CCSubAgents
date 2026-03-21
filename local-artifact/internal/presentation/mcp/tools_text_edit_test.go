package mcp

import (
	"context"
	"encoding/base64"
	"reflect"
	"strings"
	"testing"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/artifacts"
	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/presentation/daemon"
)

var textEditToolNames = []string{toolArtifactEditText, "artifact.edit_text"}

func TestTextEditTool_AppendSupportsCanonicalAndAlias(t *testing.T) {
	ctx := context.Background()

	for idx, toolName := range textEditToolNames {
		toolName := toolName
		t.Run(toolName, func(t *testing.T) {
			s := newDaemonBackedServer(t)
			artifactName := "plan/edit-append-" + strings.ReplaceAll(toolName, ".", "-") + "-" + string(rune('a'+idx))

			baseResp := requireToolOK(t, callToolsCall(t, s, ctx, toolArtifactSaveText, map[string]any{
				"name": artifactName,
				"text": "hello",
			}))
			baseOut := requireSaveOut(t, baseResp.StructuredContent)

			editResp := requireToolOK(t, callToolsCall(t, s, ctx, toolName, map[string]any{
				"operation": "append",
				"artifact":  map[string]any{"name": artifactName},
				"text":      " world",
			}))
			editOut := requireSaveOut(t, editResp.StructuredContent)
			if editOut.Ref == baseOut.Ref {
				t.Fatalf("expected append to create a new ref, got %+v", editOut)
			}
			if editOut.PrevRef != baseOut.Ref {
				t.Fatalf("expected prevRef=%q, got %q", baseOut.Ref, editOut.PrevRef)
			}
			requireContentTextEq(t, editResp, "appended")

			got, err := s.daemon().Get(ctx, daemon.GetRequest{Workspace: s.currentWorkspace(ctx), Selector: daemon.Selector{Name: artifactName}})
			if err != nil {
				t.Fatalf("daemon get after append: %v", err)
			}
			payload, err := base64.StdEncoding.DecodeString(got.DataBase64)
			if err != nil {
				t.Fatalf("decode appended payload: %v", err)
			}
			if string(payload) != "hello world" {
				t.Fatalf("unexpected appended payload: %q", string(payload))
			}
		})
	}
}

func TestTextEditTool_PatchSupportsCanonicalAndAlias(t *testing.T) {
	ctx := context.Background()
	patch := strings.Join([]string{
		"--- a/sample.txt",
		"+++ b/sample.txt",
		"@@ -1,2 +1,2 @@",
		" hello",
		"-world",
		"+gophers",
		"",
	}, "\n")

	for _, toolName := range textEditToolNames {
		toolName := toolName
		t.Run(toolName, func(t *testing.T) {
			s := newDaemonBackedServer(t)
			artifactName := "plan/edit-patch-" + strings.ReplaceAll(toolName, ".", "-")

			baseResp := requireToolOK(t, callToolsCall(t, s, ctx, toolArtifactSaveText, map[string]any{
				"name": artifactName,
				"text": "hello\nworld\n",
			}))
			baseOut := requireSaveOut(t, baseResp.StructuredContent)

			editResp := requireToolOK(t, callToolsCall(t, s, ctx, toolName, map[string]any{
				"operation": "patch",
				"artifact":  map[string]any{"name": artifactName},
				"patch":     patch,
			}))
			editOut := requireSaveOut(t, editResp.StructuredContent)
			if editOut.PrevRef != baseOut.Ref {
				t.Fatalf("expected prevRef=%q, got %q", baseOut.Ref, editOut.PrevRef)
			}
			requireContentTextEq(t, editResp, "patched")

			got, err := s.daemon().Get(ctx, daemon.GetRequest{Workspace: s.currentWorkspace(ctx), Selector: daemon.Selector{Name: artifactName}})
			if err != nil {
				t.Fatalf("daemon get after patch: %v", err)
			}
			payload, err := base64.StdEncoding.DecodeString(got.DataBase64)
			if err != nil {
				t.Fatalf("decode patched payload: %v", err)
			}
			if string(payload) != "hello\ngophers\n" {
				t.Fatalf("unexpected patched payload: %q", string(payload))
			}
		})
	}
}

func TestTextEditTool_MissingTargetReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	s := newDaemonBackedServer(t)

	cases := []map[string]any{
		{"operation": "append", "artifact": map[string]any{"name": "plan/missing-append"}, "text": "hi"},
		{"operation": "patch", "artifact": map[string]any{"name": "plan/missing-patch"}, "patch": "@@ -0,0 +1 @@\n+hi\n"},
	}

	for _, tc := range cases {
		for _, toolName := range textEditToolNames {
			toolName := toolName
			t.Run(toolName+"/"+tc["operation"].(string), func(t *testing.T) {
				resp := requireToolErr(t, callToolsCall(t, s, ctx, toolName, tc))
				requireContentTextEq(t, resp, "not found")
			})
		}
	}
}

func TestTextEditTool_StaleRefConflictsAndIsNonMutating(t *testing.T) {
	ctx := context.Background()

	for _, toolName := range textEditToolNames {
		toolName := toolName
		t.Run(toolName, func(t *testing.T) {
			s := newDaemonBackedServer(t)
			firstResp := requireToolOK(t, callToolsCall(t, s, ctx, toolArtifactSaveText, map[string]any{
				"name": "plan/edit-conflict",
				"text": "first",
			}))
			firstOut := requireSaveOut(t, firstResp.StructuredContent)
			secondResp := requireToolOK(t, callToolsCall(t, s, ctx, toolArtifactSaveText, map[string]any{
				"name": "plan/edit-conflict",
				"text": "second",
			}))
			secondOut := requireSaveOut(t, secondResp.StructuredContent)

			editResp := requireToolErr(t, callToolsCall(t, s, ctx, toolName, map[string]any{
				"operation": "append",
				"artifact":  map[string]any{"ref": firstOut.Ref},
				"text":      "!",
			}))
			requireContentTextContains(t, editResp, "conflict")

			got, err := s.daemon().Get(ctx, daemon.GetRequest{Workspace: s.currentWorkspace(ctx), Selector: daemon.Selector{Name: "plan/edit-conflict"}})
			if err != nil {
				t.Fatalf("daemon get after conflict: %v", err)
			}
			if got.Artifact.Ref != secondOut.Ref {
				t.Fatalf("expected latest ref to remain %q, got %q", secondOut.Ref, got.Artifact.Ref)
			}
		})
	}
}

func TestTextEditTool_RejectsNonTextArtifacts(t *testing.T) {
	ctx := context.Background()
	s := newDaemonBackedServer(t)

	requireToolOK(t, callToolsCall(t, s, ctx, toolArtifactSaveBlob, map[string]any{
		"name":       "plan/edit-binary",
		"dataBase64": base64.StdEncoding.EncodeToString([]byte{0x00, 0x01, 0x02}),
		"mimeType":   "application/octet-stream",
	}))

	for _, toolName := range textEditToolNames {
		toolName := toolName
		t.Run(toolName, func(t *testing.T) {
			resp := requireToolErr(t, callToolsCall(t, s, ctx, toolName, map[string]any{
				"operation": "append",
				"artifact":  map[string]any{"name": "plan/edit-binary"},
				"text":      "oops",
			}))
			requireContentTextContains(t, resp, "invalid input")
		})
	}
}

func TestTextEditTool_RejectsInvalidUTF8Payload(t *testing.T) {
	ctx := context.Background()
	s := newDaemonBackedServer(t)

	_, err := s.daemon().SaveArtifact(ctx, daemon.SaveArtifactRequest{
		Workspace:  s.currentWorkspace(ctx),
		Name:       "plan/edit-invalid-utf8",
		DataBase64: base64.StdEncoding.EncodeToString([]byte{0xff, 0xfe}),
		Kind:       artifacts.ArtifactKindText,
		MimeType:   "application/json; charset=utf-8",
	})
	if err != nil {
		t.Fatalf("seed invalid UTF-8 artifact: %v", err)
	}

	for _, toolName := range textEditToolNames {
		toolName := toolName
		t.Run(toolName, func(t *testing.T) {
			resp := requireToolErr(t, callToolsCall(t, s, ctx, toolName, map[string]any{
				"operation": "append",
				"artifact":  map[string]any{"name": "plan/edit-invalid-utf8"},
				"text":      "x",
			}))
			requireContentTextContains(t, resp, "utf-8")
		})
	}
}

func TestTextEditTool_RejectsMalformedAndNonApplicablePatches(t *testing.T) {
	ctx := context.Background()

	t.Run("malformed", func(t *testing.T) {
		s := newDaemonBackedServer(t)
		requireToolOK(t, callToolsCall(t, s, ctx, toolArtifactSaveText, map[string]any{
			"name": "plan/edit-malformed-patch",
			"text": "hello\n",
		}))

		for _, toolName := range textEditToolNames {
			toolName := toolName
			t.Run(toolName, func(t *testing.T) {
				resp := requireToolErr(t, callToolsCall(t, s, ctx, toolName, map[string]any{
					"operation": "patch",
					"artifact":  map[string]any{"name": "plan/edit-malformed-patch"},
					"patch":     "not a patch",
				}))
				requireContentTextContains(t, resp, "invalid input")
			})
		}
	})

	t.Run("non-applicable", func(t *testing.T) {
		s := newDaemonBackedServer(t)
		baseResp := requireToolOK(t, callToolsCall(t, s, ctx, toolArtifactSaveText, map[string]any{
			"name": "plan/edit-non-applicable",
			"text": "alpha\nbeta\n",
		}))
		baseOut := requireSaveOut(t, baseResp.StructuredContent)
		patch := "@@ -1,2 +1,2 @@\n-wrong\n+right\n beta\n"

		for _, toolName := range textEditToolNames {
			toolName := toolName
			t.Run(toolName, func(t *testing.T) {
				resp := requireToolErr(t, callToolsCall(t, s, ctx, toolName, map[string]any{
					"operation": "patch",
					"artifact":  map[string]any{"name": "plan/edit-non-applicable"},
					"patch":     patch,
				}))
				requireContentTextContains(t, resp, "invalid input")

				got, err := s.daemon().Get(ctx, daemon.GetRequest{Workspace: s.currentWorkspace(ctx), Selector: daemon.Selector{Name: "plan/edit-non-applicable"}})
				if err != nil {
					t.Fatalf("daemon get after failed patch: %v", err)
				}
				if got.Artifact.Ref != baseOut.Ref {
					t.Fatalf("expected latest ref to remain %q, got %q", baseOut.Ref, got.Artifact.Ref)
				}
			})
		}
	})
}

func TestTextEditTool_PreservesFilenameAndTextKindMetadata(t *testing.T) {
	ctx := context.Background()
	s := newDaemonBackedServer(t)

	baseResp := requireToolOK(t, callToolsCall(t, s, ctx, toolArtifactSaveBlob, map[string]any{
		"name":       "plan/edit-preserve-filename",
		"dataBase64": base64.StdEncoding.EncodeToString([]byte("# Notes\n")),
		"mimeType":   "text/markdown; charset=utf-8",
		"filename":   "notes.md",
	}))
	baseOut := requireSaveOut(t, baseResp.StructuredContent)
	if baseOut.Kind != string(artifacts.ArtifactKindText) {
		t.Fatalf("expected blob save with text/* MIME to be kind=text, got %+v", baseOut)
	}

	editResp := requireToolOK(t, callToolsCall(t, s, ctx, toolArtifactEditText, map[string]any{
		"operation": "append",
		"artifact":  map[string]any{"name": "plan/edit-preserve-filename"},
		"text":      "more\n",
	}))
	editOut := requireSaveOut(t, editResp.StructuredContent)
	if editOut.Filename != "notes.md" {
		t.Fatalf("expected filename to be preserved, got %+v", editOut)
	}
	if editOut.Kind != string(artifacts.ArtifactKindText) {
		t.Fatalf("expected kind=text to be preserved, got %+v", editOut)
	}
	if editOut.PrevRef != baseOut.Ref {
		t.Fatalf("expected prevRef=%q, got %q", baseOut.Ref, editOut.PrevRef)
	}
}

func TestTextEditTool_PatchCanProduceEmptyStringAndPreserveMimeAndKind(t *testing.T) {
	ctx := context.Background()
	s := newDaemonBackedServer(t)

	baseResp := requireToolOK(t, callToolsCall(t, s, ctx, toolArtifactSaveText, map[string]any{
		"name":     "plan/edit-empty-json",
		"text":     "value\n",
		"mimeType": "application/json; charset=utf-8",
	}))
	baseOut := requireSaveOut(t, baseResp.StructuredContent)
	if baseOut.Kind != string(artifacts.ArtifactKindText) {
		t.Fatalf("expected save_artifact_text to produce kind=text, got %+v", baseOut)
	}

	patch := "@@ -1 +0,0 @@\n-value\n"
	editResp := requireToolOK(t, callToolsCall(t, s, ctx, toolArtifactEditText, map[string]any{
		"operation": "patch",
		"artifact":  map[string]any{"name": "plan/edit-empty-json"},
		"patch":     patch,
	}))
	editOut := requireSaveOut(t, editResp.StructuredContent)
	if editOut.MimeType != "application/json; charset=utf-8" {
		t.Fatalf("expected MIME type to be preserved, got %+v", editOut)
	}
	if editOut.Kind != string(artifacts.ArtifactKindText) {
		t.Fatalf("expected kind=text to be preserved, got %+v", editOut)
	}

	got, err := s.daemon().Get(ctx, daemon.GetRequest{Workspace: s.currentWorkspace(ctx), Selector: daemon.Selector{Name: "plan/edit-empty-json"}})
	if err != nil {
		t.Fatalf("daemon get after empty patch: %v", err)
	}
	payload, err := base64.StdEncoding.DecodeString(got.DataBase64)
	if err != nil {
		t.Fatalf("decode empty payload: %v", err)
	}
	if string(payload) != "" {
		t.Fatalf("expected empty payload, got %q", string(payload))
	}
}

func TestTextEditTool_SchemaValidationRejectsUnknownAndOperationMismatchedFields(t *testing.T) {
	s := newDaemonBackedServer(t)
	s.setInitialized(true)
	ctx := context.Background()

	testCases := []struct {
		name string
		args map[string]any
	}{
		{
			name: "unknown top-level field",
			args: map[string]any{"operation": "append", "artifact": map[string]any{"name": "plan/x"}, "text": "ok", "extra": true},
		},
		{
			name: "unknown nested selector field",
			args: map[string]any{"operation": "append", "artifact": map[string]any{"name": "plan/x", "extra": true}, "text": "ok"},
		},
		{
			name: "append missing text",
			args: map[string]any{"operation": "append", "artifact": map[string]any{"name": "plan/x"}},
		},
		{
			name: "append forbids patch",
			args: map[string]any{"operation": "append", "artifact": map[string]any{"name": "plan/x"}, "text": "ok", "patch": "@@ -0,0 +1 @@\n+ok\n"},
		},
		{
			name: "patch missing patch",
			args: map[string]any{"operation": "patch", "artifact": map[string]any{"name": "plan/x"}},
		},
		{
			name: "patch forbids text",
			args: map[string]any{"operation": "patch", "artifact": map[string]any{"name": "plan/x"}, "text": "ok", "patch": "@@ -1 +1 @@\n-ok\n+ok\n"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			respAny, rpcErr := s.handleToolsCall(ctx, mustRawJSON(t, map[string]any{
				"name":      toolArtifactEditText,
				"arguments": tc.args,
			}))
			if rpcErr != nil {
				t.Fatalf("tools/call rpc error: %+v", rpcErr)
			}
			resp := requireToolResult(t, respAny)
			if !resp.IsError {
				t.Fatalf("expected schema validation error, got %+v", resp)
			}
			if got := firstContentText(resp); !strings.HasPrefix(got, "Invalid arguments:") {
				t.Fatalf("unexpected schema validation message: %q", got)
			}
		})
	}
}

func TestToolsList_ExposesTextEditDefinitionWithStrictNestedSchemas(t *testing.T) {
	toolsResp, rpcErr := newDaemonBackedServer(t).handleToolsList(nil)
	if rpcErr != nil {
		t.Fatalf("tools/list error: %+v", rpcErr)
	}
	tools := requireToolDefs(t, requireMap(t, toolsResp, "tools/list response")["tools"])

	var edit toolDef
	found := false
	for _, tool := range tools {
		if tool.Name == toolArtifactEditText {
			edit = tool
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected tools/list to include %q", toolArtifactEditText)
	}

	if edit.InputSchema["additionalProperties"] != false {
		t.Fatalf("expected strict top-level input schema, got %+v", edit.InputSchema)
	}
	artifactProp := requireMap(t, requireMap(t, edit.InputSchema["properties"], "edit input properties")["artifact"], "artifact property")
	if artifactProp["additionalProperties"] != false {
		t.Fatalf("expected strict artifact selector schema, got %+v", artifactProp)
	}
	operationProp := requireMap(t, requireMap(t, edit.InputSchema["properties"], "edit input properties")["operation"], "operation property")
	if !reflect.DeepEqual(operationProp["enum"], []string{"append", "patch"}) {
		t.Fatalf("expected operation enum to be append|patch, got %+v", operationProp["enum"])
	}
	textProp := requireMap(t, requireMap(t, edit.InputSchema["properties"], "edit input properties")["text"], "text property")
	if textProp["minLength"] != 1 {
		t.Fatalf("expected text minLength=1, got %+v", textProp)
	}
	patchProp := requireMap(t, requireMap(t, edit.InputSchema["properties"], "edit input properties")["patch"], "patch property")
	if patchProp["minLength"] != 1 {
		t.Fatalf("expected patch minLength=1, got %+v", patchProp)
	}
}
