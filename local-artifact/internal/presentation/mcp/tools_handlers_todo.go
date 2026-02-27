package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/artifacts"
	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/presentation/daemon"
)

type todoArtifactSelector struct {
	Name string `json:"name,omitempty"`
	Ref  string `json:"ref,omitempty"`
}

type todoItem struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type todoItemInput struct {
	ID     *int   `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type todoArgs struct {
	Operation       string               `json:"operation"`
	Artifact        todoArtifactSelector `json:"artifact"`
	TodoList        *[]todoItemInput     `json:"todoList,omitempty"`
	ExpectedPrevRef string               `json:"expectedPrevRef,omitempty"`
}

type todoOut struct {
	TodoList  []todoItem `json:"todoList"`
	Exists    bool       `json:"exists"`
	Name      string     `json:"name,omitempty"`
	Ref       string     `json:"ref,omitempty"`
	PrevRef   string     `json:"prevRef,omitempty"`
	URIByName string     `json:"uriByName,omitempty"`
	URIByRef  string     `json:"uriByRef,omitempty"`
}

const todoInvalidArgumentsMessage = "Invalid arguments: expected {operation, artifact, todoList?, expectedPrevRef?}"

func (s *Server) toolTodo(ctx context.Context, argsRaw json.RawMessage) (any, *jsonRPCError) {
	var args todoArgs
	decoder := json.NewDecoder(bytes.NewReader(argsRaw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&args); err != nil {
		return toolError(todoInvalidArgumentsMessage), nil
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return toolError(todoInvalidArgumentsMessage), nil
	}

	operation := strings.TrimSpace(args.Operation)
	if operation != "read" && operation != "write" {
		return toolErrorFromErr(fmt.Errorf("%w: operation must be read or write", artifacts.ErrInvalidInput)), nil
	}

	workspace := s.currentWorkspace(ctx)
	client := s.daemon()
	baseName, err := resolveTodoBaseName(ctx, client, workspace, args.Artifact)
	if err != nil {
		return toolErrorFromErr(err), nil
	}
	todoName := baseName + "/todo"
	nameEsc := url.PathEscape(todoName)

	if operation == "read" {
		got, err := client.Get(ctx, daemon.GetRequest{Workspace: workspace, Selector: daemon.Selector{Name: todoName}})
		if err != nil {
			var remoteErr *daemon.RemoteError
			if errors.Is(err, artifacts.ErrNotFound) || (errors.As(err, &remoteErr) && remoteErr.Code == daemon.CodeNotFound) {
				out := todoOut{
					TodoList:  []todoItem{},
					Exists:    false,
					Name:      todoName,
					URIByName: artifacts.URIByName(nameEsc),
				}
				return toolResult{Content: todoSuccessContent(operation, out), StructuredContent: out}, nil
			}
			return toolErrorFromErr(err), nil
		}
		data, decodeErr := base64.StdEncoding.DecodeString(got.DataBase64)
		if decodeErr != nil {
			return toolError("internal error: invalid daemon payload"), nil
		}

		items, err := normalizeAndValidateTodoItemsFromStored(data)
		if err != nil {
			return toolError("internal error: invalid stored todo artifact"), nil
		}
		a := got.Artifact

		out := todoOut{
			TodoList:  items,
			Exists:    true,
			Name:      a.Name,
			Ref:       a.Ref,
			PrevRef:   a.PrevRef,
			URIByName: artifacts.URIByName(nameEsc),
			URIByRef:  a.URIByRef(),
		}
		return toolResult{Content: todoSuccessContent(operation, out), StructuredContent: out}, nil
	}

	if args.TodoList == nil {
		return toolErrorFromErr(fmt.Errorf("%w: todoList is required for write", artifacts.ErrInvalidInput)), nil
	}

	items, err := normalizeAndValidateTodoInputItems(*args.TodoList)
	if err != nil {
		return toolErrorFromErr(err), nil
	}
	payload, err := json.Marshal(items)
	if err != nil {
		return toolError("internal error: failed to marshal todoList"), nil
	}

	a, err := client.SaveText(ctx, daemon.SaveTextRequest{
		Workspace:       workspace,
		Name:            todoName,
		Text:            string(payload),
		MimeType:        "application/json; charset=utf-8",
		ExpectedPrevRef: args.ExpectedPrevRef,
	})
	if err != nil {
		return toolErrorFromErr(err), nil
	}

	out := todoOut{
		TodoList:  items,
		Exists:    true,
		Name:      a.Name,
		Ref:       a.Ref,
		PrevRef:   a.PrevRef,
		URIByName: artifacts.URIByName(nameEsc),
		URIByRef:  a.URIByRef(),
	}
	return toolResult{Content: todoSuccessContent(operation, out), StructuredContent: out}, nil
}

func todoSuccessContent(operation string, out todoOut) []any {
	switch operation {
	case "read":
		if out.Exists {
			return []any{textContent(fmt.Sprintf("todo list loaded (%d items)", len(out.TodoList)))}
		}
		return []any{textContent("todo list not found; returning empty list")}
	case "write":
		return []any{textContent(fmt.Sprintf("todo list saved (%d items)", len(out.TodoList)))}
	default:
		return []any{textContent("todo list ok")}
	}
}

func resolveTodoBaseName(ctx context.Context, client *daemon.Client, workspace daemon.WorkspaceSelector, sel todoArtifactSelector) (string, error) {
	hasName := strings.TrimSpace(sel.Name) != ""
	hasRef := strings.TrimSpace(sel.Ref) != ""

	if !hasName && !hasRef {
		return "", artifacts.ErrRefOrName
	}
	if hasName && hasRef {
		return "", artifacts.ErrRefAndNameMutuallyExclusive
	}
	if hasName {
		return strings.TrimSpace(sel.Name), nil
	}

	got, err := client.Get(ctx, daemon.GetRequest{Workspace: workspace, Selector: daemon.Selector{Ref: strings.TrimSpace(sel.Ref)}})
	if err != nil {
		return "", err
	}
	a := got.Artifact
	if strings.TrimSpace(a.Name) == "" {
		return "", fmt.Errorf("%w: artifact referenced by ref has no name", artifacts.ErrInvalidInput)
	}
	return strings.TrimSpace(a.Name), nil
}

func normalizeAndValidateTodoItemsFromStored(data []byte) ([]todoItem, error) {
	var items []todoItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}
	return normalizeAndValidateTodoItems(items)
}

func normalizeAndValidateTodoInputItems(items []todoItemInput) ([]todoItem, error) {
	normalized := make([]todoItem, len(items))
	for i, item := range items {
		if item.ID == nil {
			return nil, fmt.Errorf("%w: todoList[%d].id is required", artifacts.ErrInvalidInput, i)
		}
		normalized[i] = todoItem{
			ID:     *item.ID,
			Title:  item.Title,
			Status: item.Status,
		}
	}
	return normalizeAndValidateTodoItems(normalized)
}

func normalizeAndValidateTodoItems(items []todoItem) ([]todoItem, error) {
	seenIDs := make(map[int]struct{}, len(items))
	normalized := make([]todoItem, len(items))
	for i, item := range items {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			return nil, fmt.Errorf("%w: todoList[%d].title is required", artifacts.ErrInvalidInput, i)
		}

		status := strings.TrimSpace(item.Status)
		switch status {
		case "not-started", "in-progress", "completed":
		default:
			return nil, fmt.Errorf("%w: todoList[%d].status must be one of not-started|in-progress|completed", artifacts.ErrInvalidInput, i)
		}

		if _, exists := seenIDs[item.ID]; exists {
			return nil, fmt.Errorf("%w: todoList[%d].id duplicates %d", artifacts.ErrInvalidInput, i, item.ID)
		}
		seenIDs[item.ID] = struct{}{}

		normalized[i] = todoItem{ID: item.ID, Title: title, Status: status}
	}
	return normalized, nil
}
