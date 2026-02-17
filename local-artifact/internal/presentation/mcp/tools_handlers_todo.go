package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/domain"
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
		return toolErrorFromErr(fmt.Errorf("%w: operation must be read or write", domain.ErrInvalidInput)), nil
	}

	svc := s.service(ctx)
	baseName, err := resolveTodoBaseName(ctx, svc, args.Artifact)
	if err != nil {
		return toolErrorFromErr(err), nil
	}
	todoName := baseName + "/todo"
	nameEsc := url.PathEscape(todoName)

	if operation == "read" {
		a, data, err := svc.Get(ctx, domain.Selector{Name: todoName})
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				out := todoOut{
					TodoList:  []todoItem{},
					Exists:    false,
					Name:      todoName,
					URIByName: domain.URIByName(nameEsc),
				}
				jsonStr, err := json.Marshal(out)
				if err != nil {
					return toolError("internal error: failed to marshal output"), nil
				}
				return toolResult{Content: []any{textContent(string(jsonStr))}, StructuredContent: out}, nil
			}
			return toolErrorFromErr(err), nil
		}

		items, err := normalizeAndValidateTodoItemsFromStored(data)
		if err != nil {
			return toolError("internal error: invalid stored todo artifact"), nil
		}

		out := todoOut{
			TodoList:  items,
			Exists:    true,
			Name:      a.Name,
			Ref:       a.Ref,
			PrevRef:   a.PrevRef,
			URIByName: domain.URIByName(nameEsc),
			URIByRef:  a.URIByRef(),
		}
		jsonStr, err := json.Marshal(out)
		if err != nil {
			return toolError("internal error: failed to marshal output"), nil
		}
		return toolResult{Content: []any{textContent(string(jsonStr))}, StructuredContent: out}, nil
	}

	if args.TodoList == nil {
		return toolErrorFromErr(fmt.Errorf("%w: todoList is required for write", domain.ErrInvalidInput)), nil
	}

	items, err := normalizeAndValidateTodoInputItems(*args.TodoList)
	if err != nil {
		return toolErrorFromErr(err), nil
	}
	payload, err := json.Marshal(items)
	if err != nil {
		return toolError("internal error: failed to marshal todoList"), nil
	}

	a, err := svc.SaveText(ctx, domain.SaveTextInput{
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
		URIByName: domain.URIByName(nameEsc),
		URIByRef:  a.URIByRef(),
	}
	jsonStr, err := json.Marshal(out)
	if err != nil {
		return toolError("internal error: failed to marshal output"), nil
	}
	return toolResult{Content: []any{textContent(string(jsonStr))}, StructuredContent: out}, nil
}

func resolveTodoBaseName(ctx context.Context, svc *domain.Service, sel todoArtifactSelector) (string, error) {
	hasName := strings.TrimSpace(sel.Name) != ""
	hasRef := strings.TrimSpace(sel.Ref) != ""

	if !hasName && !hasRef {
		return "", domain.ErrRefOrName
	}
	if hasName && hasRef {
		return "", domain.ErrRefAndNameMutuallyExclusive
	}
	if hasName {
		return strings.TrimSpace(sel.Name), nil
	}

	a, _, err := svc.Get(ctx, domain.Selector{Ref: strings.TrimSpace(sel.Ref)})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(a.Name) == "" {
		return "", fmt.Errorf("%w: artifact referenced by ref has no name", domain.ErrInvalidInput)
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
			return nil, fmt.Errorf("%w: todoList[%d].id is required", domain.ErrInvalidInput, i)
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
			return nil, fmt.Errorf("%w: todoList[%d].title is required", domain.ErrInvalidInput, i)
		}

		status := strings.TrimSpace(item.Status)
		switch status {
		case "not-started", "in-progress", "completed":
		default:
			return nil, fmt.Errorf("%w: todoList[%d].status must be one of not-started|in-progress|completed", domain.ErrInvalidInput, i)
		}

		if _, exists := seenIDs[item.ID]; exists {
			return nil, fmt.Errorf("%w: todoList[%d].id duplicates %d", domain.ErrInvalidInput, i, item.ID)
		}
		seenIDs[item.ID] = struct{}{}

		normalized[i] = todoItem{ID: item.ID, Title: title, Status: status}
	}
	return normalized, nil
}
