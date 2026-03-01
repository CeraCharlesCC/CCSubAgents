package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
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

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/artifacts"
	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/workspaces"
	artsqlite "github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/infrastructure/sqlite"
	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/presentation/jsonbody"
)

var subspaceHashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var csrfReadRandom = rand.Read

const globalSubspaceSelector = "global"
const mebibyte int64 = 1 << 20
const maxInsertUploadBytes int64 = 10 * mebibyte
const maxInsertUploadOverheadBytes int64 = 1 * mebibyte
const maxInsertJSONBodyBytes int64 = ((maxInsertUploadBytes + 2) / 3 * 4) + maxInsertUploadOverheadBytes
const csrfCookieName = "local-artifact-csrf"
const csrfFieldName = "csrf_token"
const csrfTokenBytes = 32

type Server struct {
	baseStoreRoot       string
	registry            workspaces.Registry
	mu                  sync.Mutex
	serviceByKey        map[string]*artifacts.Service
	closeByKey          map[string]func() error
	resolver            ServiceResolver
	apiMaxJSONBodyBytes int64
}

type ServiceResolver func(selector string) (*artifacts.Service, error)

func New(baseStoreRoot string) *Server {
	return NewWithServiceResolver(baseStoreRoot, nil)
}

func NewWithServiceResolver(baseStoreRoot string, resolver ServiceResolver) *Server {
	var registry workspaces.Registry
	sqliteRegistry, err := artsqlite.NewWorkspaceRegistry(baseStoreRoot)
	if err != nil {
		registry = nil
	} else {
		registry = sqliteRegistry
	}
	return &Server{
		baseStoreRoot:       baseStoreRoot,
		registry:            registry,
		serviceByKey:        make(map[string]*artifacts.Service),
		closeByKey:          make(map[string]func() error),
		resolver:            resolver,
		apiMaxJSONBodyBytes: maxInsertJSONBodyBytes,
	}
}

func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var firstErr error
	for _, closeFn := range s.closeByKey {
		if closeFn == nil {
			continue
		}
		if err := closeFn(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if closer, ok := s.registry.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	s.serviceByKey = make(map[string]*artifacts.Service)
	s.closeByKey = make(map[string]func() error)
	return firstErr
}

func (s *Server) Serve(ctx context.Context, addr string) error {
	httpServer := &http.Server{
		Addr:    addr,
		Handler: s.Handler(),
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

func (s *Server) Handler() http.Handler {
	return s.routes()
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/insert", s.handleInsert)
	mux.HandleFunc("/delete", s.handleDelete)
	mux.HandleFunc("/api/artifacts", s.handleAPIArtifacts)
	mux.HandleFunc("/api/artifact-content", s.handleAPIContent)
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
	Sort        string
	Limit       int
	CSRFToken   string
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
		renderIndex(w, r, pageData{
			Sort:        listSortNameAsc,
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
	sortMode := normalizeListSort(r.URL.Query().Get("sort"))
	limit := 200
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil {
			renderIndex(w, r, pageData{
				Subspaces:   subspaces,
				Subspace:    subspace,
				Prefix:      prefix,
				Sort:        sortMode,
				Limit:       limit,
				Error:       "limit must be a valid integer",
				GeneratedAt: time.Now().UTC().Format(time.RFC3339),
			})
			return
		}
		limit = parsed
	}

	if subspace != "" && !isValidSubspaceSelector(subspace) {
		renderIndex(w, r, pageData{
			Subspaces:   subspaces,
			Subspace:    subspace,
			Prefix:      prefix,
			Sort:        sortMode,
			Limit:       limit,
			Error:       "subspace must be 64 lowercase hex or global",
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		})
		return
	}
	if subspace != "" && !containsString(subspaces, subspace) {
		renderIndex(w, r, pageData{
			Subspaces:   subspaces,
			Subspace:    subspace,
			Prefix:      prefix,
			Sort:        sortMode,
			Limit:       limit,
			Error:       "selected subspace not found",
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		})
		return
	}

	var items []pageItem
	if subspace != "" {
		svc, svcErr := s.serviceForSubspace(subspace)
		if svcErr != nil {
			renderIndex(w, r, pageData{
				Subspaces:   subspaces,
				Subspace:    subspace,
				Prefix:      prefix,
				Sort:        sortMode,
				Limit:       limit,
				Error:       svcErr.Error(),
				GeneratedAt: time.Now().UTC().Format(time.RFC3339),
			})
			return
		}
		arts, listErr := listArtifacts(r.Context(), svc, prefix, sortMode, limit)
		if listErr != nil {
			renderIndex(w, r, pageData{
				Subspaces:   subspaces,
				Subspace:    subspace,
				Prefix:      prefix,
				Sort:        sortMode,
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

	renderIndex(w, r, pageData{
		Subspaces:   subspaces,
		Subspace:    subspace,
		Prefix:      prefix,
		Sort:        sortMode,
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

	subspace, prefix, sortMode, limitRaw := formRedirectContext(r.Form)
	redirectBase := indexRedirectBase(subspace, prefix, sortMode, limitRaw)
	if err := validateCSRFToken(r); err != nil {
		http.Redirect(w, r, redirectBase+"&err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

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
			if errors.Is(err, artifacts.ErrNotFound) {
				http.Redirect(w, r, redirectBase+"&err="+url.QueryEscape(artifacts.ErrNotFound.Error()), http.StatusSeeOther)
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
	var opErr error
	for _, sel := range parsedSelectors.selectors {
		if _, err := svc.Delete(r.Context(), sel); err != nil {
			if errors.Is(err, artifacts.ErrNotFound) {
				notFoundCount++
				continue
			}
			opErr = err
			break
		}
		deletedCount++
	}
	if opErr != nil {
		baseMsg := fmt.Sprintf("error after deleting %d artifact", deletedCount)
		if deletedCount != 1 {
			baseMsg += "s"
		}
		if notFoundCount > 0 {
			baseMsg = fmt.Sprintf("%s (%d not found)", baseMsg, notFoundCount)
		}
		errMsg := fmt.Sprintf("%s: %v", baseMsg, opErr)
		http.Redirect(w, r, redirectBase+"&err="+url.QueryEscape(errMsg), http.StatusSeeOther)
		return
	}

	if deletedCount == 0 {
		http.Redirect(w, r, redirectBase+"&err="+url.QueryEscape("selected artifacts not found"), http.StatusSeeOther)
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

	r.Body = http.MaxBytesReader(w, r.Body, maxInsertUploadBytes+maxInsertUploadOverheadBytes)
	if err := r.ParseMultipartForm(maxInsertUploadBytes); err != nil {
		errMsg := "invalid form data"
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			errMsg = "upload too large (max 10 MiB)"
		}
		http.Redirect(w, r, indexRedirectBase("", "", listSortNameAsc, "")+"&err="+url.QueryEscape(errMsg), http.StatusSeeOther)
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}

	subspace, prefix, sortMode, limitRaw := formRedirectContext(r.Form)
	redirectBase := indexRedirectBase(subspace, prefix, sortMode, limitRaw)
	if err := validateCSRFToken(r); err != nil {
		http.Redirect(w, r, redirectBase+"&err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

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

	var saved artifacts.Artifact
	if hasText {
		saved, err = svc.SaveText(r.Context(), artifacts.SaveTextInput{
			Name:     name,
			Text:     text,
			MimeType: mimeType,
		})
	} else {
		data, readErr := io.ReadAll(file)
		if readErr != nil {
			var maxErr *http.MaxBytesError
			if errors.As(readErr, &maxErr) {
				http.Redirect(w, r, redirectBase+"&err="+url.QueryEscape("upload too large (max 10 MiB)"), http.StatusSeeOther)
				return
			}
			http.Redirect(w, r, redirectBase+"&err="+url.QueryEscape("unable to read upload"), http.StatusSeeOther)
			return
		}
		uploadMimeType := mimeType
		if uploadMimeType == "" {
			if fileHeader != nil {
				uploadMimeType = strings.TrimSpace(fileHeader.Header.Get("Content-Type"))
			}
			if uploadMimeType == "" || strings.EqualFold(uploadMimeType, "application/octet-stream") {
				if len(data) > 0 {
					uploadMimeType = http.DetectContentType(data)
				}
			}
		}
		if uploadMimeType == "" {
			uploadMimeType = "application/octet-stream"
		}
		uploadFilename := ""
		if fileHeader != nil {
			uploadFilename = sanitizeFilename(fileHeader.Filename)
		}
		saved, err = svc.SaveBlob(r.Context(), artifacts.SaveBlobInput{
			Name:     name,
			Data:     data,
			MimeType: uploadMimeType,
			Filename: uploadFilename,
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

func (s *Server) handleAPIContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}

	svc, err := s.serviceFromQuerySubspace(r.URL.Query().Get("subspace"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	selector, err := parseSingleSelector(r.URL.Query())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	artifact, payload, err := svc.Get(r.Context(), selector)
	if err != nil {
		if errors.Is(err, artifacts.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": artifacts.ErrNotFound.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", attachmentDisposition(artifactDownloadFilename(artifact)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Artifact-Ref", artifact.Ref)
	w.Header().Set("X-Artifact-Name", artifact.Name)
	w.Header().Set("X-Artifact-MimeType", artifact.MimeType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

func (s *Server) handleAPIList(w http.ResponseWriter, r *http.Request) {
	svc, err := s.serviceFromQuerySubspace(r.URL.Query().Get("subspace"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	prefix := strings.TrimSpace(r.URL.Query().Get("prefix"))
	sortMode := normalizeListSort(r.URL.Query().Get("sort"))
	limit := 200
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "limit must be a valid integer"})
			return
		}
		limit = parsed
	}

	arts, err := listArtifacts(r.Context(), svc, prefix, sortMode, limit)
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
			if errors.Is(deleteErr, artifacts.ErrNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]any{"error": artifacts.ErrNotFound.Error()})
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

	deleted := make([]artifacts.Artifact, 0, len(parsedSelectors.selectors))
	notFoundCount := 0
	for idx, sel := range parsedSelectors.selectors {
		item, deleteErr := svc.Delete(r.Context(), sel)
		if deleteErr != nil {
			if errors.Is(deleteErr, artifacts.ErrNotFound) {
				notFoundCount++
				continue
			}
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":          deleteErr.Error(),
				"deleted":        false,
				"deletedCount":   len(deleted),
				"artifacts":      deleted,
				"notFoundCount":  notFoundCount,
				"failedIndex":    idx,
				"failedSelector": sel,
			})
			return
		}
		deleted = append(deleted, item)
	}

	if len(deleted) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error":        "artifacts not found",
			"deleted":      false,
			"deletedCount": 0,
			"artifacts":    []artifacts.Artifact{},
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
	DataBase64 *string `json:"dataBase64"`
}

func (s *Server) handleAPISave(w http.ResponseWriter, r *http.Request) {
	svc, err := s.serviceFromQuerySubspace(r.URL.Query().Get("subspace"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	var req apiSaveRequest
	if err := jsonbody.DecodeStrictJSON(r, s.apiMaxJSONBodyBytes, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON body"})
		return
	}

	hasText := req.Text != nil
	hasBlob := req.DataBase64 != nil
	if hasText == hasBlob {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "provide exactly one of text or dataBase64"})
		return
	}

	var saved artifacts.Artifact
	if hasText {
		saved, err = svc.SaveText(r.Context(), artifacts.SaveTextInput{
			Name:     req.Name,
			Text:     *req.Text,
			MimeType: req.MimeType,
		})
	} else {
		data, decodeErr := base64.StdEncoding.DecodeString(strings.TrimSpace(*req.DataBase64))
		if decodeErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid dataBase64"})
			return
		}
		if int64(len(data)) > maxInsertUploadBytes {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "upload too large (max 10 MiB)"})
			return
		}
		mimeType := strings.TrimSpace(req.MimeType)
		if mimeType == "" {
			if len(data) > 0 {
				mimeType = http.DetectContentType(data)
			} else {
				mimeType = "application/octet-stream"
			}
		}
		saved, err = svc.SaveBlob(r.Context(), artifacts.SaveBlobInput{
			Name:     req.Name,
			Data:     data,
			MimeType: mimeType,
			Filename: sanitizeFilename(req.Filename),
		})
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"artifact": saved})
}

func formRedirectContext(values url.Values) (subspace string, prefix string, sortMode string, limitRaw string) {
	subspace = strings.TrimSpace(values.Get("subspace"))
	prefix = strings.TrimSpace(values.Get("prefix"))
	sortMode = normalizeListSort(values.Get("sort"))
	limitRaw = strings.TrimSpace(values.Get("limit"))
	if limitRaw == "" {
		limitRaw = "200"
	}
	return
}

func indexRedirectBase(subspace string, prefix string, sortMode string, limitRaw string) string {
	if strings.TrimSpace(limitRaw) == "" {
		limitRaw = "200"
	}
	return "/?subspace=" + url.QueryEscape(subspace) +
		"&prefix=" + url.QueryEscape(prefix) +
		"&sort=" + url.QueryEscape(normalizeListSort(sortMode)) +
		"&limit=" + url.QueryEscape(limitRaw)
}

const (
	listSortNameAsc  = "name_asc"
	listSortTimeAsc  = "time_asc"
	listSortTimeDesc = "time_desc"
	maxListLimit     = 1000
	defaultListLimit = 200
)

func normalizeListSort(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case listSortNameAsc:
		return listSortNameAsc
	case listSortTimeAsc:
		return listSortTimeAsc
	case listSortTimeDesc:
		return listSortTimeDesc
	default:
		return listSortNameAsc
	}
}

func splitPrefixFilters(raw string) []string {
	parts := strings.Split(raw, ",")
	prefixes := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		prefix := strings.TrimSpace(part)
		if prefix == "" {
			continue
		}
		if _, ok := seen[prefix]; ok {
			continue
		}
		seen[prefix] = struct{}{}
		prefixes = append(prefixes, prefix)
	}
	return prefixes
}

func normalizedEffectiveLimit(limit int) (int, error) {
	effectiveLimit := limit
	if effectiveLimit <= 0 {
		effectiveLimit = defaultListLimit
	}
	if effectiveLimit > maxListLimit {
		return 0, fmt.Errorf("%w: limit must be <= %d", artifacts.ErrInvalidInput, maxListLimit)
	}
	return effectiveLimit, nil
}

func listArtifacts(ctx context.Context, svc *artifacts.Service, rawPrefix string, sortMode string, limit int) ([]artifacts.ArtifactVersion, error) {
	effectiveLimit, err := normalizedEffectiveLimit(limit)
	if err != nil {
		return nil, err
	}

	normalizedSortMode := normalizeListSort(sortMode)
	prefixFilters := splitPrefixFilters(rawPrefix)

	// For non-default sorting or multi-prefix OR filters, fetch a wider window and
	// apply sorting/limit in-process so time sort and OR behavior are meaningful.
	fetchLimit := effectiveLimit
	if normalizedSortMode != listSortNameAsc || len(prefixFilters) > 1 {
		fetchLimit = maxListLimit
	}

	if len(prefixFilters) == 0 {
		items, err := svc.List(ctx, "", fetchLimit)
		if err != nil {
			return nil, err
		}
		sortArtifactVersions(items, normalizedSortMode)
		if len(items) > effectiveLimit {
			items = items[:effectiveLimit]
		}
		return items, nil
	}

	combined := make([]artifacts.ArtifactVersion, 0, fetchLimit)
	seenByRef := make(map[string]struct{}, fetchLimit)
	for _, prefix := range prefixFilters {
		items, err := svc.List(ctx, prefix, fetchLimit)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if _, exists := seenByRef[item.Ref]; exists {
				continue
			}
			seenByRef[item.Ref] = struct{}{}
			combined = append(combined, item)
		}
	}

	sortArtifactVersions(combined, normalizedSortMode)
	if len(combined) > effectiveLimit {
		combined = combined[:effectiveLimit]
	}
	return combined, nil
}

func sortArtifactVersions(items []artifacts.ArtifactVersion, mode string) {
	normalizedMode := normalizeListSort(mode)
	switch normalizedMode {
	case listSortTimeDesc:
		sort.Slice(items, func(i, j int) bool {
			if items[i].CreatedAt.Equal(items[j].CreatedAt) {
				return items[i].Name < items[j].Name
			}
			return items[i].CreatedAt.After(items[j].CreatedAt)
		})
	case listSortTimeAsc:
		sort.Slice(items, func(i, j int) bool {
			if items[i].CreatedAt.Equal(items[j].CreatedAt) {
				return items[i].Name < items[j].Name
			}
			return items[i].CreatedAt.Before(items[j].CreatedAt)
		})
	default:
		sort.Slice(items, func(i, j int) bool {
			return items[i].Name < items[j].Name
		})
	}
}

type deleteSelectorRequest struct {
	selectors []artifacts.Selector
	single    bool
}

func parseDeleteSelectors(values url.Values) (deleteSelectorRequest, error) {
	names := trimUniqueNonEmpty(values["name"])
	refs := trimUniqueNonEmpty(values["ref"])

	if len(names) > 0 && len(refs) > 0 {
		return deleteSelectorRequest{}, artifacts.ErrRefAndNameMutuallyExclusive
	}

	selectors := make([]artifacts.Selector, 0, len(names)+len(refs))
	for _, name := range names {
		selectors = append(selectors, artifacts.Selector{Name: name})
	}
	for _, ref := range refs {
		selectors = append(selectors, artifacts.Selector{Ref: ref})
	}

	if len(selectors) == 0 {
		return deleteSelectorRequest{}, artifacts.ErrRefOrName
	}

	return deleteSelectorRequest{selectors: selectors, single: len(selectors) == 1}, nil
}

func parseSingleSelector(values url.Values) (artifacts.Selector, error) {
	names := trimUniqueNonEmpty(values["name"])
	refs := trimUniqueNonEmpty(values["ref"])

	if len(names) > 0 && len(refs) > 0 {
		return artifacts.Selector{}, artifacts.ErrRefAndNameMutuallyExclusive
	}
	if len(names)+len(refs) == 0 {
		return artifacts.Selector{}, artifacts.ErrRefOrName
	}
	if len(names)+len(refs) > 1 {
		return artifacts.Selector{}, errors.New("provide exactly one ref or name")
	}
	if len(names) == 1 {
		return artifacts.Selector{Name: names[0]}, nil
	}
	return artifacts.Selector{Ref: refs[0]}, nil
}

func sanitizeFilename(raw string) string {
	name := strings.TrimSpace(raw)
	if name == "" {
		return ""
	}
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)
	if name == "." || name == ".." || name == "/" {
		return ""
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	return strings.TrimSpace(name)
}

func artifactDownloadFilename(artifact artifacts.Artifact) string {
	if name := sanitizeFilename(artifact.Filename); name != "" {
		return name
	}
	fallback := strings.ReplaceAll(strings.TrimSpace(artifact.Name), "/", "_")
	if name := sanitizeFilename(fallback); name != "" {
		return name
	}
	return "artifact.bin"
}

func attachmentDisposition(filename string) string {
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": filename})
	if strings.TrimSpace(disposition) == "" {
		return `attachment; filename="artifact.bin"`
	}
	return disposition
}

func prevalidateDeleteSelectors(ctx context.Context, svc *artifacts.Service, selectors []artifacts.Selector) error {
	for _, selector := range selectors {
		if err := artifacts.ValidateSelector(selector); err != nil {
			return err
		}
		if selector.Ref != "" {
			// Skip existence checks for ref selectors to avoid unnecessary blob reads.
			// Delete will report not-found selectors in the mutation pass.
			continue
		}
		var err error
		if selector.Name != "" {
			_, err = svc.Resolve(ctx, selector.Name)
		}
		if err == nil || errors.Is(err, artifacts.ErrNotFound) {
			continue
		}
		return err
	}
	return nil
}

func trimUniqueNonEmpty(values []string) []string {
	// Preserve first occurrence order while dropping duplicates and empty values.
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

func issueCSRFToken() (string, error) {
	buf := make([]byte, csrfTokenBytes)
	if _, err := csrfReadRandom(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func isValidCSRFToken(token string) bool {
	if len(token) != csrfTokenBytes*2 {
		return false
	}
	_, err := hex.DecodeString(token)
	return err == nil
}

func csrfTokenFromRequest(r *http.Request) string {
	cookie, err := r.Cookie(csrfCookieName)
	if err != nil {
		return ""
	}
	token := strings.TrimSpace(cookie.Value)
	if !isValidCSRFToken(token) {
		return ""
	}
	return token
}

func ensureCSRFToken(w http.ResponseWriter, r *http.Request) (string, error) {
	if token := csrfTokenFromRequest(r); token != "" {
		return token, nil
	}
	token, err := issueCSRFToken()
	if err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	})
	return token, nil
}

func validateCSRFToken(r *http.Request) error {
	cookieToken := csrfTokenFromRequest(r)
	if cookieToken == "" {
		return errors.New("invalid csrf token")
	}
	formToken := strings.TrimSpace(r.FormValue(csrfFieldName))
	if !isValidCSRFToken(formToken) {
		return errors.New("invalid csrf token")
	}
	if subtle.ConstantTimeCompare([]byte(cookieToken), []byte(formToken)) != 1 {
		return errors.New("invalid csrf token")
	}
	return nil
}

func (s *Server) serviceFromSelectedSubspace(rawSubspace string) (*artifacts.Service, error) {
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
	return s.serviceForSubspace(subspace)
}

func (s *Server) serviceFromQuerySubspace(rawSubspace string) (*artifacts.Service, error) {
	subspace := normalizeSubspaceSelector(rawSubspace)
	if subspace == "" {
		subspace = globalSubspaceSelector
	}
	return s.serviceFromSelectedSubspace(subspace)
}

func (s *Server) serviceForSubspace(selector string) (*artifacts.Service, error) {
	selector = normalizeSubspaceSelector(selector)
	if selector == "" {
		selector = globalSubspaceSelector
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing := s.serviceByKey[selector]; existing != nil {
		return existing, nil
	}
	if s.resolver != nil {
		svc, err := s.resolver(selector)
		if err != nil {
			return nil, err
		}
		if svc == nil {
			return nil, errors.New("workspace service unavailable")
		}
		s.serviceByKey[selector] = svc
		s.closeByKey[selector] = nil
		return svc, nil
	}

	storeRoot := s.baseStoreRoot
	if selector != globalSubspaceSelector {
		storeRoot = filepath.Join(s.baseStoreRoot, selector)
	}
	repo, err := artsqlite.NewArtifactRepository(storeRoot)
	if err != nil {
		return nil, err
	}
	svc := artifacts.NewService(repo)
	if s.registry != nil {
		roots := []string{}
		workspaceID := selector
		if workspaceID == globalSubspaceSelector {
			workspaceID = workspaces.GlobalWorkspaceID
		}
		_ = s.registry.EnsureWorkspace(context.Background(), workspaceID, roots, "web")
	}
	s.serviceByKey[selector] = svc
	s.closeByKey[selector] = repo.Close
	return svc, nil
}

func (s *Server) discoverSubspaces() ([]string, error) {
	info, err := os.Stat(s.baseStoreRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{globalSubspaceSelector}, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("store root is not a directory: %s", s.baseStoreRoot)
	}

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

func renderIndex(w http.ResponseWriter, r *http.Request, data pageData) {
	token, err := ensureCSRFToken(w, r)
	if err != nil {
		http.Error(w, "csrf setup error", http.StatusInternalServerError)
		return
	}
	data.CSRFToken = token
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
