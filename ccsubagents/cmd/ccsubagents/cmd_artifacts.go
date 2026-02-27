package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/config"
	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/daemonclient"
	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/paths"
)

var artifactRefPattern = regexp.MustCompile(`^\d{8}T\d{6}(?:\.\d{3})?Z-[0-9a-f]{16}$`)

func runArtifacts(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "Usage: ccsubagents artifacts <ls|get|put|openwebui>")
		return 2
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	stateDir := paths.ResolveDaemonStateDir(home, os.Getenv)

	var client *daemonclient.Client
	getClient := func() (*daemonclient.Client, error) {
		if client != nil {
			return client, nil
		}
		resolved, err := daemonclient.NewDefaultClient(stateDir, os.Getenv)
		if err != nil {
			return nil, err
		}
		client = resolved
		return client, nil
	}

	sub := strings.TrimSpace(args[0])
	switch sub {
	case "openwebui":
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		settings, err := config.LoadMergedInstallSettings(home, cwd)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}

		addr := "127.0.0.1:19130"
		if envAddr := strings.TrimSpace(os.Getenv("LOCAL_ARTIFACT_WEB_UI_ADDR")); envAddr != "" {
			addr = envAddr
		} else if settings.WebUIPort != 0 {
			addr = fmt.Sprintf("127.0.0.1:%d", settings.WebUIPort)
		}

		if settings.NoAuth {
			fmt.Fprintf(stdout, "http://%s/\n", addr)
			return 0
		}

		token := strings.TrimSpace(os.Getenv("LOCAL_ARTIFACT_DAEMON_TOKEN"))
		if token == "" {
			tokenBytes, err := os.ReadFile(filepath.Join(stateDir, "daemon", "daemon.token"))
			if err != nil {
				if !os.IsNotExist(err) {
					fmt.Fprintln(stderr, err)
					return 1
				}
			} else {
				token = strings.TrimSpace(string(tokenBytes))
			}
		}

		if token == "" {
			fmt.Fprintf(stdout, "http://%s/\n", addr)
			return 0
		}
		fmt.Fprintf(stdout, "http://%s/?token=%s\n", addr, url.QueryEscape(token))
		return 0
	case "ls":
		client, err := getClient()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fs := flag.NewFlagSet("artifacts ls", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		prefix := fs.String("prefix", "", "name prefix")
		limit := fs.Int("limit", 100, "max results")
		workspaceID := fs.String("workspace-id", "global", "workspace id")
		if err := fs.Parse(args[1:]); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		res, err := client.List(context.Background(), daemonclient.ListRequest{
			Workspace: daemonclient.WorkspaceSelector{WorkspaceID: normalizeWorkspaceID(*workspaceID)},
			Prefix:    strings.TrimSpace(*prefix),
			Limit:     *limit,
		})
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		for _, item := range res.Items {
			fmt.Fprintf(stdout, "%s\t%s\n", item.Name, item.Ref)
		}
		return 0
	case "get":
		client, err := getClient()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fs := flag.NewFlagSet("artifacts get", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		outPath := fs.String("out", "-", "output path or - for stdout")
		workspaceID := fs.String("workspace-id", "global", "workspace id")
		if err := fs.Parse(args[1:]); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		if fs.NArg() != 1 {
			fmt.Fprintln(stderr, "Usage: ccsubagents artifacts get <name|ref> [--out PATH|-]")
			return 2
		}
		id := strings.TrimSpace(fs.Arg(0))
		sel := daemonclient.Selector{Name: id}
		if looksLikeRef(id) {
			sel = daemonclient.Selector{Ref: id}
		}
		res, err := client.Get(context.Background(), daemonclient.GetRequest{
			Workspace: daemonclient.WorkspaceSelector{WorkspaceID: normalizeWorkspaceID(*workspaceID)},
			Selector:  sel,
		})
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		payload, err := base64.StdEncoding.DecodeString(res.DataBase64)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if *outPath == "-" {
			_, _ = stdout.Write(payload)
			return 0
		}
		if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := os.WriteFile(*outPath, payload, 0o600); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "wrote %s\n", *outPath)
		return 0
	case "put":
		client, err := getClient()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fs := flag.NewFlagSet("artifacts put", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		mimeType := fs.String("mime-type", "", "content MIME type")
		filename := fs.String("filename", "", "optional filename metadata")
		workspaceID := fs.String("workspace-id", "global", "workspace id")
		expectedPrevRef := fs.String("expected-prev-ref", "", "optimistic concurrency ref")
		if err := fs.Parse(args[1:]); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		if fs.NArg() != 2 {
			fmt.Fprintln(stderr, "Usage: ccsubagents artifacts put <name> <path|-> [flags]")
			return 2
		}
		name := strings.TrimSpace(fs.Arg(0))
		path := strings.TrimSpace(fs.Arg(1))
		if name == "" {
			fmt.Fprintln(stderr, "artifact name is required")
			return 2
		}

		data, err := readPutData(stdin, path)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		typeHint := strings.TrimSpace(*mimeType)
		if typeHint == "" {
			if path == "-" {
				typeHint = "text/plain"
			} else {
				typeHint = "application/octet-stream"
			}
		}

		workspace := daemonclient.WorkspaceSelector{WorkspaceID: normalizeWorkspaceID(*workspaceID)}
		if strings.HasPrefix(typeHint, "text/") {
			saved, err := client.SaveText(context.Background(), daemonclient.SaveTextRequest{
				Workspace:       workspace,
				Name:            name,
				Text:            string(data),
				MimeType:        typeHint,
				ExpectedPrevRef: strings.TrimSpace(*expectedPrevRef),
			})
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			fmt.Fprintf(stdout, "%s\n", saved.Ref)
			return 0
		}
		saved, err := client.SaveBlob(context.Background(), daemonclient.SaveBlobRequest{
			Workspace:       workspace,
			Name:            name,
			DataBase64:      base64.StdEncoding.EncodeToString(data),
			MimeType:        typeHint,
			Filename:        strings.TrimSpace(*filename),
			ExpectedPrevRef: strings.TrimSpace(*expectedPrevRef),
		})
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "%s\n", saved.Ref)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown artifacts subcommand %q\n", sub)
		return 2
	}
}

func normalizeWorkspaceID(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "global"
	}
	return strings.ToLower(trimmed)
}

func looksLikeRef(value string) bool {
	value = strings.TrimSpace(value)
	return artifactRefPattern.MatchString(value)
}

func readPutData(stdin io.Reader, path string) ([]byte, error) {
	if strings.TrimSpace(path) == "-" {
		if stdin == nil {
			return nil, errors.New("stdin is unavailable")
		}
		return io.ReadAll(stdin)
	}
	return os.ReadFile(path)
}
