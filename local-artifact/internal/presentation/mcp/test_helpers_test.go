package mcp

import (
	"context"
	"strings"
	"testing"
)

func callToolsCall(t *testing.T, s *Server, ctx context.Context, toolName string, args map[string]any) toolResult {
	t.Helper()
	respAny, rpcErr := s.handleToolsCall(ctx, mustRawJSON(t, map[string]any{
		"name":      toolName,
		"arguments": args,
	}))
	if rpcErr != nil {
		t.Fatalf("tools/call %q rpc error: %+v", toolName, rpcErr)
	}

	resp, ok := respAny.(toolResult)
	if !ok {
		t.Fatalf("tools/call %q expected toolResult, got %T", toolName, respAny)
	}
	return resp
}

func requireToolOK(t *testing.T, resp toolResult) toolResult {
	t.Helper()
	if resp.IsError {
		t.Fatalf("expected tool success, got tool error: %+v", resp)
	}
	return resp
}

func requireToolErr(t *testing.T, resp toolResult) toolResult {
	t.Helper()
	if !resp.IsError {
		t.Fatalf("expected tool error, got success: %+v", resp)
	}
	return resp
}

func requireContentTextEq(t *testing.T, resp toolResult, want string) {
	t.Helper()
	if got := firstContentText(resp); got != want {
		t.Fatalf("unexpected content text\nwant: %q\n got: %q", want, got)
	}
}

func requireContentTextContains(t *testing.T, resp toolResult, needle string) {
	t.Helper()
	if !contentContains(resp, needle) {
		t.Fatalf("expected content to contain %q, got %+v", needle, resp.Content)
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
