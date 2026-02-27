package workspaces

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"path"
	"sort"
	"strings"
)

const GlobalWorkspaceID = "global"

func ComputeWorkspaceID(normalizedSortedRoots []string) string {
	if len(normalizedSortedRoots) == 0 {
		return GlobalWorkspaceID
	}
	joined := strings.Join(normalizedSortedRoots, "\n")
	sum := sha256.Sum256([]byte(joined))
	return hex.EncodeToString(sum[:])
}

func NormalizeRootURIs(roots []string) ([]string, error) {
	set := map[string]struct{}{}
	for _, raw := range roots {
		normalized, err := NormalizeRootURI(raw)
		if err != nil {
			continue
		}
		set[normalized] = struct{}{}
	}

	if len(set) == 0 {
		return nil, errors.New("no valid file roots")
	}

	out := make([]string, 0, len(set))
	for uri := range set {
		out = append(out, uri)
	}
	sort.Strings(out)
	return out, nil
}

func NormalizeRootURI(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("empty root uri")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(u.Scheme, "file") {
		return "", errors.New("root uri must use file scheme")
	}
	if strings.TrimSpace(u.Path) == "" {
		return "", errors.New("root uri path is required")
	}

	u.Scheme = "file"
	host := strings.ToLower(strings.TrimSpace(u.Host))
	if host == "localhost" {
		host = ""
	}
	u.Host = host
	u.RawQuery = ""
	u.Fragment = ""
	u.Path = path.Clean(u.Path)
	if !strings.HasPrefix(u.Path, "/") {
		u.Path = "/" + u.Path
	}
	u.RawPath = ""

	return u.String(), nil
}
