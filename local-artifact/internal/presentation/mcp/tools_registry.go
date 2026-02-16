package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

type callToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type toolResult struct {
	Content           []any `json:"content"`
	StructuredContent any   `json:"structuredContent,omitempty"`
	IsError           bool  `json:"isError,omitempty"`
}

type toolHandlerFunc func(*Server, context.Context, json.RawMessage) (any, *jsonRPCError)

type toolRegistryMetadata struct {
	CanonicalName string
	Aliases       []string
}

type toolRegistryEntry struct {
	Metadata toolRegistryMetadata
	Handler  toolHandlerFunc
}

var (
	toolRegistryOnce sync.Once
	toolRegistryByID map[string]toolRegistryEntry
)

func initializeToolRegistry() {
	entries := []toolRegistryEntry{
		{
			Metadata: toolRegistryMetadata{CanonicalName: toolArtifactSaveText, Aliases: []string{"artifact.save_text"}},
			Handler: func(s *Server, ctx context.Context, args json.RawMessage) (any, *jsonRPCError) {
				return s.toolSaveText(ctx, args)
			},
		},
		{
			Metadata: toolRegistryMetadata{CanonicalName: toolArtifactSaveBlob, Aliases: []string{"artifact.save_blob"}},
			Handler: func(s *Server, ctx context.Context, args json.RawMessage) (any, *jsonRPCError) {
				return s.toolSaveBlob(ctx, args)
			},
		},
		{
			Metadata: toolRegistryMetadata{CanonicalName: toolArtifactResolve, Aliases: []string{"artifact.resolve"}},
			Handler: func(s *Server, ctx context.Context, args json.RawMessage) (any, *jsonRPCError) {
				return s.toolResolve(ctx, args)
			},
		},
		{
			Metadata: toolRegistryMetadata{CanonicalName: toolArtifactGet, Aliases: []string{"artifact.get"}},
			Handler: func(s *Server, ctx context.Context, args json.RawMessage) (any, *jsonRPCError) {
				return s.toolGet(ctx, args)
			},
		},
		{
			Metadata: toolRegistryMetadata{CanonicalName: toolArtifactList, Aliases: []string{"artifact.list"}},
			Handler: func(s *Server, ctx context.Context, args json.RawMessage) (any, *jsonRPCError) {
				return s.toolList(ctx, args)
			},
		},
		{
			Metadata: toolRegistryMetadata{CanonicalName: toolArtifactDelete, Aliases: []string{"artifact.delete", "deleteArtifact"}},
			Handler: func(s *Server, ctx context.Context, args json.RawMessage) (any, *jsonRPCError) {
				return s.toolDelete(ctx, args)
			},
		},
	}

	toolRegistryByID = make(map[string]toolRegistryEntry, len(entries)*2)
	for _, entry := range entries {
		toolRegistryByID[entry.Metadata.CanonicalName] = entry
		for _, alias := range entry.Metadata.Aliases {
			toolRegistryByID[alias] = entry
		}
	}
}

func lookupToolRegistryEntry(name string) (toolRegistryEntry, bool) {
	toolRegistryOnce.Do(initializeToolRegistry)
	entry, ok := toolRegistryByID[name]
	return entry, ok
}

func (s *Server) handleToolsList(_ json.RawMessage) (any, *jsonRPCError) {
	return map[string]any{"tools": toolDefinitions()}, nil
}

func (s *Server) handleToolsCall(ctx context.Context, params json.RawMessage) (any, *jsonRPCError) {
	if !s.isInitialized() {
		// Be lenient: some clients call tools before notifications/initialized.
	}

	var p callToolParams
	if err := json.Unmarshal(params, &p); err != nil {
		return toolError("invalid params"), nil
	}

	entry, ok := lookupToolRegistryEntry(p.Name)
	if !ok {
		return toolError(fmt.Sprintf("unknown tool: %s", p.Name)), nil
	}

	return entry.Handler(s, ctx, p.Arguments)
}
