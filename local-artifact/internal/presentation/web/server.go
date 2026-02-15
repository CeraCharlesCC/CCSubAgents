package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"local-artifact-mcp/internal/domain"
	"local-artifact-mcp/internal/infrastructure/filestore"
)

var subspaceHashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

const globalSubspaceSelector = "global"

type Server struct {
	baseStoreRoot string

	mu sync.Mutex
	// Intentionally unbounded for this developer/operator tool.
	// Operators are responsible for controlling subspace churn over process lifetime.
	serviceBySelector map[string]*domain.Service
}

func New(baseStoreRoot string) *Server {
	return &Server{
		baseStoreRoot:     baseStoreRoot,
		serviceBySelector: map[string]*domain.Service{},
	}
}

func (s *Server) Serve(ctx context.Context, addr string) error {
	httpServer := &http.Server{
		Addr:    addr,
		Handler: s.routes(),
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
		return ctx.Err()
	case err := <-errCh:
		if err == nil || err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/delete", s.handleDelete)
	mux.HandleFunc("/api/artifacts", s.handleAPIArtifacts)
	mux.HandleFunc("/api/subspaces", s.handleAPISubspaces)
	return mux
}

type pageItem struct {
	Name      string
	Ref       string
	Kind      string
	MimeType  string
	SizeBytes int64
	SHA256    string
	CreatedAt string
}

type pageData struct {
	Subspaces   []string
	Subspace    string
	Prefix      string
	Limit       int
	Message     string
	Error       string
	Items       []pageItem
	GeneratedAt string
}

var indexTemplate = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Local Artifact Store</title>
  <style>
    :root {
      --bg: #f5f3ef;
      --card: #fffaf3;
      --ink: #1d1b17;
      --muted: #6a6153;
      --accent: #0b6e4f;
      --danger: #a12727;
      --line: #ded6c9;
      --mono: "IBM Plex Mono", "SFMono-Regular", Menlo, Consolas, monospace;
      --sans: "IBM Plex Sans", "Segoe UI", system-ui, sans-serif;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      background:
        radial-gradient(circle at 10% 10%, #ece5d7 0, transparent 45%),
        radial-gradient(circle at 90% 0%, #ddd2bc 0, transparent 40%),
        var(--bg);
      color: var(--ink);
      font-family: var(--sans);
    }
    main {
      max-width: 1100px;
      margin: 2rem auto;
      padding: 0 1rem 2rem;
    }
    h1 {
      margin: 0 0 0.35rem;
      font-size: clamp(1.4rem, 2.5vw, 2rem);
      letter-spacing: 0.02em;
    }
    .sub {
      margin-bottom: 1.25rem;
      color: var(--muted);
    }
    .card {
      background: var(--card);
      border: 1px solid var(--line);
      border-radius: 14px;
      padding: 0.9rem;
      box-shadow: 0 6px 24px rgba(43, 40, 34, 0.07);
    }
    form.filters {
      display: flex;
      gap: 0.65rem;
      align-items: end;
      flex-wrap: wrap;
      margin-bottom: 0.75rem;
    }
    label {
      display: grid;
      gap: 0.3rem;
      font-size: 0.85rem;
      color: var(--muted);
    }
    input, select {
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 0.52rem 0.6rem;
      font-family: var(--mono);
      min-width: 180px;
      background: #fff;
    }
    button {
      border: 0;
      border-radius: 9px;
      padding: 0.58rem 0.85rem;
      font-family: var(--sans);
      font-weight: 650;
      cursor: pointer;
      background: var(--accent);
      color: #fff;
    }
    button.delete {
      background: var(--danger);
      font-size: 0.85rem;
      padding: 0.4rem 0.65rem;
    }
    .msg {
      margin: 0.4rem 0 0.75rem;
      padding: 0.55rem 0.7rem;
      border-radius: 8px;
      font-size: 0.9rem;
    }
    .msg.ok {
      background: #e9f8f2;
      color: #115a42;
      border: 1px solid #b8e4d4;
    }
    .msg.err {
      background: #fbeceb;
      color: #7e2020;
      border: 1px solid #efc4c4;
    }
    .table-wrap {
      overflow-x: auto;
    }
    table {
      width: 100%;
      border-collapse: collapse;
      font-size: 0.9rem;
    }
    th, td {
      text-align: left;
      border-bottom: 1px solid var(--line);
      padding: 0.58rem 0.45rem;
      vertical-align: top;
    }
    th { color: var(--muted); font-weight: 650; }
    td code {
      font-family: var(--mono);
      font-size: 0.8rem;
      overflow-wrap: anywhere;
    }
    .foot {
      margin-top: 0.75rem;
      font-size: 0.82rem;
      color: var(--muted);
    }
    @media (max-width: 700px) {
      th:nth-child(4), td:nth-child(4), th:nth-child(6), td:nth-child(6) {
        display: none;
      }
    }
  </style>
</head>
<body>
  <main>
    <h1>Local Artifact Store</h1>
    <div class="sub">Track current aliases and delete artifacts quickly.</div>

    <section class="card">
      <form class="filters" method="get" action="/">
        <label>Subspace
          <select name="subspace">
            <option value="">-- select --</option>
            {{range .Subspaces}}
              <option value="{{.}}" {{if eq $.Subspace .}}selected{{end}}>{{if eq . "global"}}global (fallback){{else}}{{.}}{{end}}</option>
            {{end}}
          </select>
        </label>
        <label>Prefix
          <input type="text" name="prefix" value="{{.Prefix}}" placeholder="plan/ or image/">
        </label>
        <label>Limit
          <input type="number" name="limit" min="1" max="1000" value="{{.Limit}}">
        </label>
        <button type="submit">Refresh</button>
      </form>

      {{if .Message}}<div class="msg ok">{{.Message}}</div>{{end}}
      {{if .Error}}<div class="msg err">{{.Error}}</div>{{end}}

      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>Ref</th>
              <th>Kind</th>
              <th>MIME</th>
              <th>Size</th>
              <th>Created</th>
              <th>Delete</th>
            </tr>
          </thead>
          <tbody>
          {{range .Items}}
            <tr>
              <td><code>{{.Name}}</code></td>
              <td><code>{{.Ref}}</code></td>
              <td>{{.Kind}}</td>
              <td><code>{{.MimeType}}</code></td>
              <td>{{.SizeBytes}}</td>
              <td><code>{{.CreatedAt}}</code></td>
              <td>
                <form method="post" action="/delete" onsubmit="return confirm('Delete artifact {{.Name}}?');">
                  <input type="hidden" name="subspace" value="{{$.Subspace}}">
                  <input type="hidden" name="prefix" value="{{$.Prefix}}">
                  <input type="hidden" name="limit" value="{{$.Limit}}">
                  <input type="hidden" name="name" value="{{.Name}}">
                  <button class="delete" type="submit">Delete</button>
                </form>
              </td>
            </tr>
          {{else}}
            <tr><td colspan="7">No artifacts found for this filter.</td></tr>
          {{end}}
          </tbody>
        </table>
      </div>

      <div class="foot">Generated at {{.GeneratedAt}} | JSON endpoints: <code>/api/subspaces</code>, <code>/api/artifacts?subspace=&lt;global|hash&gt;</code></div>
    </section>
  </main>
</body>
</html>`))

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}

	subspaces, err := s.discoverSubspaces()
	if err != nil {
		renderIndex(w, pageData{
			Limit:       200,
			Error:       err.Error(),
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		})
		return
	}

	subspace := strings.TrimSpace(r.URL.Query().Get("subspace"))
	if subspace == "" && len(subspaces) > 0 {
		subspace = subspaces[0]
	}

	prefix := strings.TrimSpace(r.URL.Query().Get("prefix"))
	limit := 200
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil {
			renderIndex(w, pageData{
				Subspaces:   subspaces,
				Subspace:    subspace,
				Prefix:      prefix,
				Limit:       limit,
				Error:       "limit must be a valid integer",
				GeneratedAt: time.Now().UTC().Format(time.RFC3339),
			})
			return
		}
		limit = parsed
	}

	if subspace != "" && !isValidSubspaceSelector(subspace) {
		renderIndex(w, pageData{
			Subspaces:   subspaces,
			Subspace:    subspace,
			Prefix:      prefix,
			Limit:       limit,
			Error:       "subspace must be 64 lowercase hex or global",
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		})
		return
	}
	if subspace != "" && !containsString(subspaces, subspace) {
		renderIndex(w, pageData{
			Subspaces:   subspaces,
			Subspace:    subspace,
			Prefix:      prefix,
			Limit:       limit,
			Error:       "selected subspace not found",
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		})
		return
	}

	var items []pageItem
	if subspace != "" {
		svc := s.serviceForSubspace(subspace)
		arts, listErr := svc.List(r.Context(), prefix, limit)
		if listErr != nil {
			renderIndex(w, pageData{
				Subspaces:   subspaces,
				Subspace:    subspace,
				Prefix:      prefix,
				Limit:       limit,
				Error:       listErr.Error(),
				GeneratedAt: time.Now().UTC().Format(time.RFC3339),
			})
			return
		}

		items = make([]pageItem, 0, len(arts))
		for _, a := range arts {
			items = append(items, pageItem{
				Name:      a.Name,
				Ref:       a.Ref,
				Kind:      string(a.Kind),
				MimeType:  a.MimeType,
				SizeBytes: a.SizeBytes,
				SHA256:    a.SHA256,
				CreatedAt: a.CreatedAt.Format(time.RFC3339),
			})
		}
	}

	message := strings.TrimSpace(r.URL.Query().Get("msg"))
	errorMsg := strings.TrimSpace(r.URL.Query().Get("err"))
	if subspace == "" && len(subspaces) == 0 && message == "" && errorMsg == "" {
		message = "No subspaces discovered yet."
	}

	renderIndex(w, pageData{
		Subspaces:   subspaces,
		Subspace:    subspace,
		Prefix:      prefix,
		Limit:       limit,
		Items:       items,
		Message:     message,
		Error:       errorMsg,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	subspace := strings.TrimSpace(r.FormValue("subspace"))
	prefix := strings.TrimSpace(r.FormValue("prefix"))
	limitRaw := strings.TrimSpace(r.FormValue("limit"))
	if limitRaw == "" {
		limitRaw = "200"
	}

	redirectBase := "/?subspace=" + url.QueryEscape(subspace) + "&prefix=" + url.QueryEscape(prefix) + "&limit=" + url.QueryEscape(limitRaw)

	if !isValidSubspaceSelector(subspace) {
		http.Redirect(w, r, redirectBase+"&err="+url.QueryEscape("subspace must be 64 lowercase hex or global"), http.StatusSeeOther)
		return
	}
	if ok, err := s.subspaceExists(subspace); err != nil {
		http.Redirect(w, r, redirectBase+"&err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	} else if !ok {
		http.Redirect(w, r, redirectBase+"&err="+url.QueryEscape("selected subspace not found"), http.StatusSeeOther)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	ref := strings.TrimSpace(r.FormValue("ref"))
	svc := s.serviceForSubspace(subspace)
	a, err := svc.Delete(r.Context(), domain.Selector{Name: name, Ref: ref})
	if err != nil {
		http.Redirect(w, r, redirectBase+"&err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	msg := "deleted artifact"
	if strings.TrimSpace(a.Name) != "" {
		msg = fmt.Sprintf("deleted %q", a.Name)
	}
	http.Redirect(w, r, redirectBase+"&msg="+url.QueryEscape(msg), http.StatusSeeOther)
}

func (s *Server) handleAPIArtifacts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleAPIList(w, r)
	case http.MethodDelete:
		s.handleAPIDelete(w, r)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodDelete)
	}
}

func (s *Server) handleAPISubspaces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	subspaces, err := s.discoverSubspaces()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": subspaces})
}

func (s *Server) handleAPIList(w http.ResponseWriter, r *http.Request) {
	svc, err := s.serviceFromQuerySubspace(r.URL.Query().Get("subspace"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	prefix := strings.TrimSpace(r.URL.Query().Get("prefix"))
	limit := 200
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "limit must be a valid integer"})
			return
		}
		limit = parsed
	}

	arts, err := svc.List(r.Context(), prefix, limit)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": arts})
}

func (s *Server) handleAPIDelete(w http.ResponseWriter, r *http.Request) {
	svc, err := s.serviceFromQuerySubspace(r.URL.Query().Get("subspace"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	name := strings.TrimSpace(r.URL.Query().Get("name"))
	ref := strings.TrimSpace(r.URL.Query().Get("ref"))

	deleted, err := svc.Delete(r.Context(), domain.Selector{Name: name, Ref: ref})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, domain.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "artifact": deleted})
}

func (s *Server) serviceFromQuerySubspace(rawSubspace string) (*domain.Service, error) {
	subspace := normalizeSubspaceSelector(rawSubspace)
	if subspace == "" {
		subspace = globalSubspaceSelector
	}
	if !isValidSubspaceSelector(subspace) {
		return nil, errors.New("subspace must be 64 lowercase hex or global")
	}
	ok, err := s.subspaceExists(subspace)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("selected subspace not found")
	}
	return s.serviceForSubspace(subspace), nil
}

func (s *Server) serviceForSubspace(selector string) *domain.Service {
	selector = normalizeSubspaceSelector(selector)
	if selector == "" {
		selector = globalSubspaceSelector
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	existing := s.serviceBySelector[selector]
	if existing != nil {
		return existing
	}
	storeRoot := s.baseStoreRoot
	if selector != globalSubspaceSelector {
		storeRoot = filepath.Join(s.baseStoreRoot, selector)
	}
	repo := filestore.New(storeRoot)
	svc := domain.NewService(repo)
	s.serviceBySelector[selector] = svc
	return svc
}

func (s *Server) discoverSubspaces() ([]string, error) {
	hashes := make([]string, 0)
	entries, err := os.ReadDir(s.baseStoreRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{globalSubspaceSelector}, nil
		}
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		if isValidSubspaceHash(name) {
			hashes = append(hashes, name)
		}
	}
	sort.Strings(hashes)
	items := make([]string, 0, len(hashes)+1)
	items = append(items, globalSubspaceSelector)
	items = append(items, hashes...)
	return items, nil
}

func (s *Server) subspaceExists(selector string) (bool, error) {
	selector = normalizeSubspaceSelector(selector)
	if selector == globalSubspaceSelector {
		info, err := os.Stat(s.baseStoreRoot)
		if err != nil {
			if os.IsNotExist(err) {
				return true, nil
			}
			return false, err
		}
		return info.IsDir(), nil
	}

	info, err := os.Stat(filepath.Join(s.baseStoreRoot, selector))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return info.IsDir(), nil
}

func isValidSubspaceHash(hash string) bool {
	return subspaceHashPattern.MatchString(strings.TrimSpace(hash))
}

func isValidSubspaceSelector(subspace string) bool {
	subspace = normalizeSubspaceSelector(subspace)
	if subspace == globalSubspaceSelector {
		return true
	}
	return isValidSubspaceHash(subspace)
}

func normalizeSubspaceSelector(subspace string) string {
	return strings.TrimSpace(subspace)
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func renderIndex(w http.ResponseWriter, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := indexTemplate.Execute(w, data); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

func writeJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeMethodNotAllowed(w http.ResponseWriter, allowed ...string) {
	if len(allowed) > 0 {
		w.Header().Set("Allow", strings.Join(allowed, ", "))
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}
