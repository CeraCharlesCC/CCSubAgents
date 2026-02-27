package daemon

import "github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/artifacts"

const (
	DefaultMaxRequestBytes int64 = 12 << 20 // 12 MiB
)

type WorkspaceSelector struct {
	WorkspaceID string   `json:"workspaceID,omitempty"`
	Roots       []string `json:"roots,omitempty"`
}

type SaveTextRequest struct {
	Workspace       WorkspaceSelector `json:"workspace"`
	Name            string            `json:"name"`
	Text            string            `json:"text"`
	MimeType        string            `json:"mimeType,omitempty"`
	ExpectedPrevRef string            `json:"expectedPrevRef,omitempty"`
}

type SaveBlobRequest struct {
	Workspace       WorkspaceSelector `json:"workspace"`
	Name            string            `json:"name"`
	DataBase64      string            `json:"dataBase64"`
	MimeType        string            `json:"mimeType"`
	Filename        string            `json:"filename,omitempty"`
	ExpectedPrevRef string            `json:"expectedPrevRef,omitempty"`
}

type ResolveRequest struct {
	Workspace WorkspaceSelector `json:"workspace"`
	Name      string            `json:"name"`
}

type ResolveResponse struct {
	Name string `json:"name"`
	Ref  string `json:"ref"`
}

type Selector struct {
	Ref  string `json:"ref,omitempty"`
	Name string `json:"name,omitempty"`
}

type GetRequest struct {
	Workspace WorkspaceSelector `json:"workspace"`
	Selector  Selector          `json:"selector"`
}

type GetResponse struct {
	Artifact   artifacts.ArtifactVersion `json:"artifact"`
	DataBase64 string                    `json:"dataBase64"`
}

type ListRequest struct {
	Workspace WorkspaceSelector `json:"workspace"`
	Prefix    string            `json:"prefix,omitempty"`
	Limit     int               `json:"limit,omitempty"`
}

type ListResponse struct {
	Items []artifacts.ArtifactVersion `json:"items"`
}

type DeleteRequest struct {
	Workspace WorkspaceSelector `json:"workspace"`
	Selector  Selector          `json:"selector"`
}

type DeleteResponse struct {
	Deleted  bool                      `json:"deleted"`
	Artifact artifacts.ArtifactVersion `json:"artifact"`
}

type HealthResponse struct {
	Status string `json:"status"`
}

type ShutdownResponse struct {
	Status string `json:"status"`
}
