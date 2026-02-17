package web

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/domain"
	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/infrastructure/filestore"
)

var subspaceHashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

const globalSubspaceSelector = "global"
const maxInsertUploadBytes int64 = 10 << 20

type Server struct {
	baseStoreRoot string
	mu            sync.Mutex
	serviceByKey  map[string]*domain.Service
}

func New(baseStoreRoot string) *Server {
	return &Server{
		baseStoreRoot: baseStoreRoot,
		serviceByKey:  make(map[string]*domain.Service),
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
	mux.HandleFunc("/insert", s.handleInsert)
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

	subspace, prefix, limitRaw := formRedirectContext(r.Form)
	redirectBase := indexRedirectBase(subspace, prefix, limitRaw)

	svc, err := s.serviceFromSelectedSubspace(subspace)
	if err != nil {
		http.Redirect(w, r, redirectBase+"&err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	parsedSelectors, err := parseDeleteSelectors(r.Form)
	if err != nil {
		http.Redirect(w, r, redirectBase+"&err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if err := prevalidateDeleteSelectors(r.Context(), svc, parsedSelectors.selectors); err != nil {
		http.Redirect(w, r, redirectBase+"&err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	if parsedSelectors.single {
		if _, err := svc.Delete(r.Context(), parsedSelectors.selectors[0]); err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				http.Redirect(w, r, redirectBase+"&err="+url.QueryEscape(domain.ErrNotFound.Error()), http.StatusSeeOther)
				return
			}
			http.Redirect(w, r, redirectBase+"&err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, redirectBase+"&msg="+url.QueryEscape("deleted 1 artifact"), http.StatusSeeOther)
		return
	}

	deletedCount := 0
	notFoundCount := 0
	for _, sel := range parsedSelectors.selectors {
		if _, err := svc.Delete(r.Context(), sel); err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				notFoundCount++
				continue
			}
			http.Redirect(w, r, redirectBase+"&err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
			return
		}
		deletedCount++
	}

	if deletedCount == 0 {
		errMsg := "selected artifacts not found"
		if notFoundCount == 0 {
			errMsg = domain.ErrRefOrName.Error()
		}
		http.Redirect(w, r, redirectBase+"&err="+url.QueryEscape(errMsg), http.StatusSeeOther)
		return
	}

	msg := fmt.Sprintf("deleted %d artifact", deletedCount)
	if deletedCount != 1 {
		msg += "s"
	}
	if notFoundCount > 0 {
		msg = fmt.Sprintf("%s (%d not found)", msg, notFoundCount)
	}
	http.Redirect(w, r, redirectBase+"&msg="+url.QueryEscape(msg), http.StatusSeeOther)
}

func (s *Server) handleInsert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxInsertUploadBytes+(1<<20))
	if err := r.ParseMultipartForm(maxInsertUploadBytes); err != nil {
		errMsg := "invalid form data"
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			errMsg = "upload too large (max 10 MiB)"
		}
		http.Redirect(w, r, indexRedirectBase("", "", "")+"&err="+url.QueryEscape(errMsg), http.StatusSeeOther)
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}

	subspace, prefix, limitRaw := formRedirectContext(r.Form)
	redirectBase := indexRedirectBase(subspace, prefix, limitRaw)

	svc, err := s.serviceFromSelectedSubspace(subspace)
	if err != nil {
		http.Redirect(w, r, redirectBase+"&err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	mimeType := strings.TrimSpace(r.FormValue("mimeType"))
	text := r.FormValue("text")
	hasText := strings.TrimSpace(text) != ""

	file, fileHeader, err := r.FormFile("file")
	hasFile := false
	if err == nil {
		hasFile = true
		defer file.Close()
	} else if !errors.Is(err, http.ErrMissingFile) {
		http.Redirect(w, r, redirectBase+"&err="+url.QueryEscape("invalid upload file"), http.StatusSeeOther)
		return
	}

	if hasText == hasFile {
		http.Redirect(w, r, redirectBase+"&err="+url.QueryEscape("choose exactly one content source: text or file"), http.StatusSeeOther)
		return
	}

	var saved domain.Artifact
	if hasText {
		saved, err = svc.SaveText(r.Context(), domain.SaveTextInput{
			Name:     name,
			Text:     text,
			MimeType: mimeType,
		})
	} else {
		data, readErr := io.ReadAll(file)
		if readErr != nil {
			http.Redirect(w, r, redirectBase+"&err="+url.QueryEscape("unable to read upload"), http.StatusSeeOther)
			return
		}
		uploadMimeType := mimeType
		if uploadMimeType == "" && fileHeader != nil {
			uploadMimeType = strings.TrimSpace(fileHeader.Header.Get("Content-Type"))
		}
		if uploadMimeType == "" {
			uploadMimeType = "application/octet-stream"
		}
		filename := ""
		if fileHeader != nil {
			filename = fileHeader.Filename
		}
		saved, err = svc.SaveBlob(r.Context(), domain.SaveBlobInput{
			Name:     name,
			Data:     data,
			MimeType: uploadMimeType,
			Filename: filename,
		})
	}
	if err != nil {
		http.Redirect(w, r, redirectBase+"&err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, redirectBase+"&msg="+url.QueryEscape(fmt.Sprintf("saved %q", saved.Name)), http.StatusSeeOther)
}

func (s *Server) handleAPIArtifacts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleAPIList(w, r)
	case http.MethodPost:
		s.handleAPISave(w, r)
	case http.MethodDelete:
		s.handleAPIDelete(w, r)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost, http.MethodDelete)
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

	parsedSelectors, err := parseDeleteSelectors(r.URL.Query())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := prevalidateDeleteSelectors(r.Context(), svc, parsedSelectors.selectors); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	if parsedSelectors.single {
		deletedItem, deleteErr := svc.Delete(r.Context(), parsedSelectors.selectors[0])
		if deleteErr != nil {
			if errors.Is(deleteErr, domain.ErrNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]any{"error": domain.ErrNotFound.Error()})
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": deleteErr.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"deleted":  true,
			"artifact": deletedItem,
		})
		return
	}

	deleted := make([]domain.Artifact, 0, len(parsedSelectors.selectors))
	notFoundCount := 0
	for _, sel := range parsedSelectors.selectors {
		item, deleteErr := svc.Delete(r.Context(), sel)
		if deleteErr != nil {
			if errors.Is(deleteErr, domain.ErrNotFound) {
				notFoundCount++
				continue
			}
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": deleteErr.Error()})
			return
		}
		deleted = append(deleted, item)
	}

	if len(deleted) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error":        "artifacts not found",
			"deleted":      false,
			"deletedCount": 0,
			"artifacts":    []domain.Artifact{},
		})
		return
	}

	payload := map[string]any{
		"deleted":       true,
		"deletedCount":  len(deleted),
		"artifacts":     deleted,
		"notFoundCount": notFoundCount,
	}
	if len(deleted) == 1 {
		payload["artifact"] = deleted[0]
	}
	writeJSON(w, http.StatusOK, payload)
}

type apiSaveRequest struct {
	Name       string  `json:"name"`
	Text       *string `json:"text"`
	MimeType   string  `json:"mimeType"`
	Filename   string  `json:"filename"`
	DataBase64 string  `json:"dataBase64"`
}

func (s *Server) handleAPISave(w http.ResponseWriter, r *http.Request) {
	svc, err := s.serviceFromQuerySubspace(r.URL.Query().Get("subspace"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	decoder := json.NewDecoder(io.LimitReader(r.Body, maxInsertUploadBytes+(1<<20)))
	decoder.DisallowUnknownFields()
	var req apiSaveRequest
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON body"})
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON body"})
		return
	}

	hasText := req.Text != nil
	hasBlob := strings.TrimSpace(req.DataBase64) != ""
	if hasText == hasBlob {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "provide exactly one of text or dataBase64"})
		return
	}

	var saved domain.Artifact
	if hasText {
		saved, err = svc.SaveText(r.Context(), domain.SaveTextInput{
			Name:     req.Name,
			Text:     *req.Text,
			MimeType: req.MimeType,
		})
	} else {
		data, decodeErr := base64.StdEncoding.DecodeString(strings.TrimSpace(req.DataBase64))
		if decodeErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid dataBase64"})
			return
		}
		mimeType := strings.TrimSpace(req.MimeType)
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		saved, err = svc.SaveBlob(r.Context(), domain.SaveBlobInput{
			Name:     req.Name,
			Data:     data,
			MimeType: mimeType,
			Filename: req.Filename,
		})
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"artifact": saved})
}

func formRedirectContext(values url.Values) (subspace string, prefix string, limitRaw string) {
	subspace = strings.TrimSpace(values.Get("subspace"))
	prefix = strings.TrimSpace(values.Get("prefix"))
	limitRaw = strings.TrimSpace(values.Get("limit"))
	if limitRaw == "" {
		limitRaw = "200"
	}
	return
}

func indexRedirectBase(subspace string, prefix string, limitRaw string) string {
	if strings.TrimSpace(limitRaw) == "" {
		limitRaw = "200"
	}
	return "/?subspace=" + url.QueryEscape(subspace) + "&prefix=" + url.QueryEscape(prefix) + "&limit=" + url.QueryEscape(limitRaw)
}

type deleteSelectorRequest struct {
	selectors []domain.Selector
	single    bool
}

func parseDeleteSelectors(values url.Values) (deleteSelectorRequest, error) {
	names := trimUniqueNonEmpty(values["name"])
	refs := trimUniqueNonEmpty(values["ref"])

	if len(names) > 0 && len(refs) > 0 {
		return deleteSelectorRequest{}, domain.ErrRefAndNameMutuallyExclusive
	}

	selectors := make([]domain.Selector, 0, len(names)+len(refs))
	for _, name := range names {
		selectors = append(selectors, domain.Selector{Name: name})
	}
	for _, ref := range refs {
		selectors = append(selectors, domain.Selector{Ref: ref})
	}

	if len(selectors) == 0 {
		return deleteSelectorRequest{}, domain.ErrRefOrName
	}

	return deleteSelectorRequest{selectors: selectors, single: len(selectors) == 1}, nil
}

func prevalidateDeleteSelectors(ctx context.Context, svc *domain.Service, selectors []domain.Selector) error {
	for _, selector := range selectors {
		_, _, err := svc.Get(ctx, selector)
		if err == nil || errors.Is(err, domain.ErrNotFound) {
			continue
		}
		return err
	}
	return nil
}

func trimUniqueNonEmpty(values []string) []string {
	trimmedValues := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		trimmedValues = append(trimmedValues, trimmed)
	}
	return trimmedValues
}

func (s *Server) serviceFromSelectedSubspace(rawSubspace string) (*domain.Service, error) {
	subspace := normalizeSubspaceSelector(rawSubspace)
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

func (s *Server) serviceFromQuerySubspace(rawSubspace string) (*domain.Service, error) {
	subspace := normalizeSubspaceSelector(rawSubspace)
	if subspace == "" {
		subspace = globalSubspaceSelector
	}
	return s.serviceFromSelectedSubspace(subspace)
}

func (s *Server) serviceForSubspace(selector string) *domain.Service {
	selector = normalizeSubspaceSelector(selector)
	if selector == "" {
		selector = globalSubspaceSelector
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing := s.serviceByKey[selector]; existing != nil {
		return existing
	}

	storeRoot := s.baseStoreRoot
	if selector != globalSubspaceSelector {
		storeRoot = filepath.Join(s.baseStoreRoot, selector)
	}
	repo := filestore.New(storeRoot)
	svc := domain.NewService(repo)
	s.serviceByKey[selector] = svc
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
	return strings.ToLower(strings.TrimSpace(subspace))
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
