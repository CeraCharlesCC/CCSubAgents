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
	daemon "github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/presentation/daemonapi"
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

type todoUpdateTarget struct {
	Index *int `json:"index,omitempty"`
	ID    *int `json:"id,omitempty"`
}

type todoArgs struct {
	Operation       string               `json:"operation"`
	Artifact        todoArtifactSelector `json:"artifact"`
	TodoList        *[]todoItemInput     `json:"todoList,omitempty"`
	Target          *todoUpdateTarget    `json:"target,omitempty"`
	Status          string               `json:"status,omitempty"`
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

const todoInvalidArgumentsMessage = "Invalid arguments: expected {operation, artifact, todoList?, target?, status?, expectedPrevRef?}"

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
	if operation != "read" && operation != "write" && operation != "update" {
		return toolErrorFromErr(fmt.Errorf("%w: operation must be read, write, or update", artifacts.ErrInvalidInput)), nil
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
		items, got, err := loadStoredTodoItems(ctx, client, workspace, todoName)
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

	if operation == "write" {
		if args.TodoList == nil {
			return toolErrorFromErr(fmt.Errorf("%w: todoList is required for write", artifacts.ErrInvalidInput)), nil
		}

		items, err := normalizeAndValidateTodoInputItems(*args.TodoList)
		if err != nil {
			return toolErrorFromErr(err), nil
		}

		out, err := saveTodoItems(ctx, client, workspace, todoName, nameEsc, items, args.ExpectedPrevRef)
		if err != nil {
			return toolErrorFromErr(err), nil
		}
		return toolResult{Content: todoSuccessContent(operation, out), StructuredContent: out}, nil
	}

	if args.Target == nil {
		return toolErrorFromErr(fmt.Errorf("%w: target is required for update", artifacts.ErrInvalidInput)), nil
	}

	items, _, err := loadStoredTodoItems(ctx, client, workspace, todoName)
	if err != nil {
		return toolErrorFromErr(err), nil
	}

	updateIndex, err := findTodoUpdateIndex(items, *args.Target)
	if err != nil {
		return toolErrorFromErr(err), nil
	}

	status, err := normalizeTodoStatus(args.Status, "status")
	if err != nil {
		return toolErrorFromErr(err), nil
	}
	items[updateIndex].Status = status

	out, err := saveTodoItems(ctx, client, workspace, todoName, nameEsc, items, args.ExpectedPrevRef)
	if err != nil {
		return toolErrorFromErr(err), nil
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
	case "update":
		return []any{textContent("todo item status updated")}
	default:
		return []any{textContent("todo list ok")}
	}
}

func loadStoredTodoItems(ctx context.Context, client *daemon.Client, workspace daemon.WorkspaceSelector, todoName string) ([]todoItem, daemon.GetResponse, error) {
	got, err := client.Get(ctx, daemon.GetRequest{Workspace: workspace, Selector: daemon.Selector{Name: todoName}})
	if err != nil {
		return nil, daemon.GetResponse{}, err
	}

	data, decodeErr := base64.StdEncoding.DecodeString(got.DataBase64)
	if decodeErr != nil {
		return nil, daemon.GetResponse{}, fmt.Errorf("invalid daemon payload")
	}

	items, err := normalizeAndValidateTodoItemsFromStored(data)
	if err != nil {
		return nil, daemon.GetResponse{}, fmt.Errorf("invalid stored todo artifact")
	}

	return items, got, nil
}

func saveTodoItems(ctx context.Context, client *daemon.Client, workspace daemon.WorkspaceSelector, todoName, nameEsc string, items []todoItem, expectedPrevRef string) (todoOut, error) {
	payload, err := json.Marshal(items)
	if err != nil {
		return todoOut{}, fmt.Errorf("failed to marshal todoList")
	}

	a, err := client.SaveText(ctx, daemon.SaveTextRequest{
		Workspace:       workspace,
		Name:            todoName,
		Text:            string(payload),
		MimeType:        "application/json; charset=utf-8",
		ExpectedPrevRef: expectedPrevRef,
	})
	if err != nil {
		return todoOut{}, err
	}

	return todoOut{
		TodoList:  items,
		Exists:    true,
		Name:      a.Name,
		Ref:       a.Ref,
		PrevRef:   a.PrevRef,
		URIByName: artifacts.URIByName(nameEsc),
		URIByRef:  a.URIByRef(),
	}, nil
}

func findTodoUpdateIndex(items []todoItem, target todoUpdateTarget) (int, error) {
	hasIndex := target.Index != nil
	hasID := target.ID != nil
	if hasIndex == hasID {
		return 0, fmt.Errorf("%w: target must include exactly one of index or id", artifacts.ErrInvalidInput)
	}

	if hasIndex {
		if *target.Index < 0 || *target.Index >= len(items) {
			return 0, fmt.Errorf("%w: target.index %d out of range", artifacts.ErrInvalidInput, *target.Index)
		}
		return *target.Index, nil
	}

	for i, item := range items {
		if item.ID == *target.ID {
			return i, nil
		}
	}

	return 0, fmt.Errorf("%w: target.id %d not found", artifacts.ErrInvalidInput, *target.ID)
}

func normalizeTodoStatus(status, fieldPath string) (string, error) {
	normalized := strings.TrimSpace(status)
	switch normalized {
	case todoStatusNotStarted, todoStatusInProgress, todoStatusCompleted:
		return normalized, nil
	default:
		return "", fmt.Errorf("%w: %s must be one of %s|%s|%s", artifacts.ErrInvalidInput, fieldPath, todoStatusNotStarted, todoStatusInProgress, todoStatusCompleted)
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

		status, err := normalizeTodoStatus(item.Status, fmt.Sprintf("todoList[%d].status", i))
		if err != nil {
			return nil, err
		}

		if _, exists := seenIDs[item.ID]; exists {
			return nil, fmt.Errorf("%w: todoList[%d].id duplicates %d", artifacts.ErrInvalidInput, i, item.ID)
		}
		seenIDs[item.ID] = struct{}{}

		normalized[i] = todoItem{ID: item.ID, Title: title, Status: status}
	}
	return normalized, nil
}
