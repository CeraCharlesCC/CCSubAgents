package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/artifacts"
	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/workspaces"
	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/presentation/web"
)

type RunConfig struct {
	StoreRoot   string
	StateDir    string
	LogDir      string
	APISocket   string
	APIAddr     string
	WebAddr     string
	Token       string
	DisableAuth bool
	Stderr      io.Writer
}

type apiAlreadyListeningError struct {
	socket string
}

func (e *apiAlreadyListeningError) Error() string {
	if e == nil {
		return "daemon already listening"
	}
	return fmt.Sprintf("daemon already listening on %s", e.socket)
}

func (c *RunConfig) normalize() {
	c.StoreRoot = strings.TrimSpace(c.StoreRoot)
	c.StateDir = strings.TrimSpace(c.StateDir)
	c.LogDir = strings.TrimSpace(c.LogDir)
	if c.StateDir == "" {
		c.StateDir = c.StoreRoot
	}
	if c.LogDir == "" {
		c.LogDir = filepath.Join(c.StateDir, "daemon")
	}
	if c.Stderr == nil {
		c.Stderr = os.Stderr
	}
	if runtime.GOOS == "windows" {
		if strings.TrimSpace(c.APIAddr) == "" {
			c.APIAddr = defaultDaemonTCPAddr
		}
	} else {
		if strings.TrimSpace(c.APISocket) == "" {
			c.APISocket = filepath.Join(c.StateDir, "daemon", "ccsubagentsd.sock")
		}
	}
}

func Run(ctx context.Context, cfg RunConfig) error {
	cfg.normalize()
	if cfg.StoreRoot == "" {
		return errors.New("store root is required")
	}

	token := ""
	if cfg.DisableAuth {
		if err := clearTokenFile(cfg.StateDir); err != nil {
			return err
		}
	} else {
		resolvedToken, err := ResolveOrCreateToken(cfg.StateDir, cfg.Token)
		if err != nil {
			return err
		}
		token = resolvedToken
	}
	engine, err := NewEngine(cfg.StoreRoot)
	if err != nil {
		return err
	}
	defer engine.Close()

	daemonServer := NewServer(engine, "daemon")
	apiHandler := AuthMiddleware(token, daemonServer.Routes(), AuthOptions{SkipPathPrefix: "/daemon/v1/health"})

	apiListener, apiAddress, err := listenAPI(cfg)
	if err != nil {
		var alreadyErr *apiAlreadyListeningError
		if strings.TrimSpace(cfg.WebAddr) == "" || !errors.As(err, &alreadyErr) {
			return err
		}
		fmt.Fprintf(cfg.Stderr, "ccsubagentsd api already active on unix://%s; starting web-only mode\n", alreadyErr.socket)
	}
	var apiHTTPServer *http.Server
	var apiErrCh chan error
	shutdownErrCh := make(chan error, 1)
	var shutdownOnce sync.Once
	if apiListener != nil {
		defer apiListener.Close()
		apiHTTPServer = &http.Server{Handler: apiHandler}
		apiErrCh = make(chan error, 1)
		go func() {
			apiErrCh <- apiHTTPServer.Serve(apiListener)
		}()
		fmt.Fprintf(cfg.Stderr, "ccsubagentsd api listening on %s\n", apiAddress)
	}

	var webHTTPServer *http.Server
	var webErrCh chan error
	shutdownServers := func() {
		shutdownOnce.Do(func() {
			go func() {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				var err error
				if apiHTTPServer != nil {
					err = apiHTTPServer.Shutdown(shutdownCtx)
					if err == nil || errors.Is(err, http.ErrServerClosed) {
						err = nil
					}
				}
				if webHTTPServer != nil {
					if webErr := webHTTPServer.Shutdown(shutdownCtx); webErr != nil && !errors.Is(webErr, http.ErrServerClosed) && err == nil {
						err = webErr
					}
				}
				shutdownErrCh <- err
			}()
		})
	}
	if strings.TrimSpace(cfg.WebAddr) != "" {
		if !isLoopbackHostPort(cfg.WebAddr) {
			return fmt.Errorf("web address must bind localhost only: %s", cfg.WebAddr)
		}
		webServer := web.NewWithServiceResolver(cfg.StoreRoot, daemonWebServiceResolver(engine))
		defer webServer.Close()
		webMux := http.NewServeMux()
		webMux.Handle("/daemon/v1/", daemonServer.Routes())
		webMux.Handle("/", webServer.Handler())
		webHandler := AuthMiddleware(token, webMux, AuthOptions{AllowQueryBootstrap: true, SkipPathPrefix: "/daemon/v1/health"})

		webHTTPServer = &http.Server{Addr: cfg.WebAddr, Handler: webHandler}
		webErrCh = make(chan error, 1)
		go func() {
			webErrCh <- webHTTPServer.ListenAndServe()
		}()
		fmt.Fprintf(cfg.Stderr, "ccsubagentsd web listening on http://%s\n", cfg.WebAddr)
		if token == "" {
			fmt.Fprintf(cfg.Stderr, "ccsubagentsd web bootstrap: open http://%s/\n", cfg.WebAddr)
		} else {
			fmt.Fprintln(cfg.Stderr, webBootstrapHint(cfg.WebAddr, cfg.StateDir))
		}
	}
	daemonServer.SetShutdownFunc(shutdownServers)

	select {
	case <-ctx.Done():
		shutdownServers()
		return ctx.Err()
	case err := <-shutdownErrCh:
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case err := <-apiErrCh:
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case err := <-webErrCh:
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func clearTokenFile(stateDir string) error {
	tokenPath := tokenFilePath(stateDir)
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(tokenPath, []byte(""), 0o600)
}

func listenAPI(cfg RunConfig) (net.Listener, string, error) {
	if runtime.GOOS == "windows" || strings.TrimSpace(cfg.APISocket) == "" {
		addr, err := resolveLoopbackTCPAddr(cfg.APIAddr)
		if err != nil {
			return nil, "", err
		}
		ln, err := net.Listen("tcp", addr)
		return ln, "tcp://" + addr, err
	}

	socket := strings.TrimSpace(cfg.APISocket)
	if socket == "" {
		return nil, "", errors.New("api socket is required")
	}
	if err := os.MkdirAll(filepath.Dir(socket), 0o755); err != nil {
		return nil, "", err
	}
	if err := cleanupStaleSocket(socket); err != nil {
		return nil, "", err
	}
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return nil, "", err
	}
	if err := os.Chmod(socket, 0o600); err != nil {
		_ = ln.Close()
		return nil, "", err
	}
	return ln, "unix://" + socket, nil
}

func cleanupStaleSocket(socket string) error {
	info, err := os.Lstat(socket)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to remove non-socket path: %s", socket)
	}

	conn, err := net.DialTimeout("unix", socket, 200*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return &apiAlreadyListeningError{socket: socket}
	}
	if !errors.Is(err, syscall.ECONNREFUSED) {
		return err
	}
	if err := os.Remove(socket); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func daemonWebServiceResolver(engine *Engine) web.ServiceResolver {
	return func(selector string) (*artifacts.Service, error) {
		workspaceID := strings.ToLower(strings.TrimSpace(selector))
		if workspaceID == "" || workspaceID == "global" {
			workspaceID = workspaces.GlobalWorkspaceID
		}
		_, svc, err := engine.resolveWorkspace(context.Background(), WorkspaceSelector{WorkspaceID: workspaceID}, "daemon")
		return svc, err
	}
}

func resolveLoopbackTCPAddr(rawAddr string) (string, error) {
	addr := strings.TrimSpace(rawAddr)
	if addr == "" {
		addr = defaultDaemonTCPAddr
	}
	if !isLoopbackHostPort(addr) {
		return "", fmt.Errorf("api address must bind localhost only: %s", addr)
	}
	return addr, nil
}

func webBootstrapHint(webAddr, stateDir string) string {
	return fmt.Sprintf("ccsubagentsd web bootstrap: open http://%s/ then authenticate with token from %s", webAddr, tokenFilePath(stateDir))
}

func isLoopbackHostPort(addr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return false
	}
	host = strings.TrimSpace(host)
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	if parsed := net.ParseIP(host); parsed != nil {
		return parsed.IsLoopback()
	}
	return false
}
