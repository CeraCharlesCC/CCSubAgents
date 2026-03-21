package daemonclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	defaultDaemonAddr      = "127.0.0.1:19131"
	defaultMaxResponseSize = 12 << 20
	daemonTokenEnv         = "LOCAL_ARTIFACT_DAEMON_TOKEN"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
	failErr error
}

func NewUnavailableClient(cause error) *Client {
	if cause == nil {
		cause = errors.New("daemon client unavailable")
	}
	return &Client{failErr: cause}
}

func NewHTTPClient(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:   strings.TrimSpace(token),
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func NewUnixSocketClient(socketPath, token string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{
		baseURL: "http://daemon.local",
		token:   strings.TrimSpace(token),
		http: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
	}
}

func NewDefaultClient(stateDir string, getenv func(string) string) (*Client, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	token := ResolveDaemonToken(stateDir, getenv)
	if runtime.GOOS == "windows" {
		addr := strings.TrimSpace(getenv("LOCAL_ARTIFACT_DAEMON_ADDR"))
		if addr == "" {
			addr = defaultDaemonAddr
		}
		return NewHTTPClient("http://"+addr, token), nil
	}
	socket := strings.TrimSpace(getenv("LOCAL_ARTIFACT_DAEMON_SOCKET"))
	if socket == "" {
		socket = filepath.Join(stateDir, "daemon", "ccsubagentsd.sock")
	}
	return NewUnixSocketClient(socket, token), nil
}

func ResolveDaemonToken(stateDir string, getenv func(string) string) string {
	if getenv == nil {
		getenv = os.Getenv
	}
	token := strings.TrimSpace(getenv(daemonTokenEnv))
	if token != "" {
		return token
	}

	b, err := os.ReadFile(filepath.Join(stateDir, "daemon", "daemon.token"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func (c *Client) Health(ctx context.Context) error {
	var out HealthResponse
	return c.do(ctx, http.MethodGet, "/daemon/v1/health", nil, &out)
}

func (c *Client) Shutdown(ctx context.Context) (ShutdownResponse, error) {
	var out ShutdownResponse
	if err := c.do(ctx, http.MethodPost, "/daemon/v1/control/shutdown", map[string]any{}, &out); err != nil {
		return ShutdownResponse{}, err
	}
	return out, nil
}

func (c *Client) SaveText(ctx context.Context, req SaveTextRequest) (ArtifactVersion, error) {
	var out struct {
		Artifact ArtifactVersion `json:"artifact"`
	}
	if err := c.do(ctx, http.MethodPost, "/daemon/v1/artifacts/save_text", req, &out); err != nil {
		return ArtifactVersion{}, err
	}
	return out.Artifact, nil
}

func (c *Client) SaveBlob(ctx context.Context, req SaveBlobRequest) (ArtifactVersion, error) {
	var out struct {
		Artifact ArtifactVersion `json:"artifact"`
	}
	if err := c.do(ctx, http.MethodPost, "/daemon/v1/artifacts/save_blob", req, &out); err != nil {
		return ArtifactVersion{}, err
	}
	return out.Artifact, nil
}

func (c *Client) Resolve(ctx context.Context, req ResolveRequest) (ResolveResponse, error) {
	var out ResolveResponse
	if err := c.do(ctx, http.MethodPost, "/daemon/v1/artifacts/resolve", req, &out); err != nil {
		return ResolveResponse{}, err
	}
	return out, nil
}

func (c *Client) Get(ctx context.Context, req GetRequest) (GetResponse, error) {
	var out GetResponse
	if err := c.do(ctx, http.MethodPost, "/daemon/v1/artifacts/get", req, &out); err != nil {
		return GetResponse{}, err
	}
	return out, nil
}

func (c *Client) List(ctx context.Context, req ListRequest) (ListResponse, error) {
	var out ListResponse
	if err := c.do(ctx, http.MethodPost, "/daemon/v1/artifacts/list", req, &out); err != nil {
		return ListResponse{}, err
	}
	return out, nil
}

func (c *Client) Delete(ctx context.Context, req DeleteRequest) (DeleteResponse, error) {
	var out DeleteResponse
	if err := c.do(ctx, http.MethodPost, "/daemon/v1/artifacts/delete", req, &out); err != nil {
		return DeleteResponse{}, err
	}
	return out, nil
}

func (c *Client) do(ctx context.Context, method, path string, reqBody any, out any) error {
	if c == nil || c.failErr != nil {
		cause := errors.New("daemon client unavailable")
		if c != nil && c.failErr != nil {
			cause = c.failErr
		}
		return &RemoteError{Code: CodeServiceUnavailable, Message: cause.Error(), HTTPStatus: http.StatusServiceUnavailable}
	}
	if c.http == nil {
		return &RemoteError{Code: CodeServiceUnavailable, Message: "http client unavailable", HTTPStatus: http.StatusServiceUnavailable}
	}
	if strings.TrimSpace(c.baseURL) == "" {
		return &RemoteError{Code: CodeServiceUnavailable, Message: "daemon base URL is empty", HTTPStatus: http.StatusServiceUnavailable}
	}

	var body io.Reader
	if reqBody != nil {
		payload, err := json.Marshal(reqBody)
		if err != nil {
			return err
		}
		body = bytes.NewReader(payload)
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	if reqBody != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	if strings.TrimSpace(c.token) != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return &RemoteError{Code: CodeServiceUnavailable, Message: err.Error(), HTTPStatus: http.StatusServiceUnavailable}
	}
	defer closeResponseBody(resp)

	var env struct {
		OK    bool            `json:"ok"`
		Data  json.RawMessage `json:"data"`
		Error *EnvelopeError  `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, defaultMaxResponseSize)).Decode(&env); err != nil {
		return fmt.Errorf("decode daemon response: %w", err)
	}
	if !env.OK || env.Error != nil || resp.StatusCode >= 400 {
		if env.Error == nil {
			return &RemoteError{Code: CodeInternal, Message: "request failed", HTTPStatus: resp.StatusCode}
		}
		return &RemoteError{Code: env.Error.Code, Message: env.Error.Message, HTTPStatus: resp.StatusCode}
	}

	if out == nil || len(env.Data) == 0 || string(env.Data) == "null" {
		return nil
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("decode daemon data: %w", err)
	}
	return nil
}

func closeResponseBody(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	if err := resp.Body.Close(); err != nil {
		_ = err
	}
}
