package daemonclient

import "time"

type WorkspaceSelector struct {
	WorkspaceID string   `json:"workspaceID,omitempty"`
	Roots       []string `json:"roots,omitempty"`
}

type ArtifactVersion struct {
	Ref       string    `json:"ref"`
	Name      string    `json:"name,omitempty"`
	Kind      string    `json:"kind"`
	MimeType  string    `json:"mimeType"`
	Filename  string    `json:"filename,omitempty"`
	SizeBytes int64     `json:"sizeBytes"`
	SHA256    string    `json:"sha256,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	PrevRef   string    `json:"prevRef,omitempty"`
	Tombstone bool      `json:"tombstone,omitempty"`
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
	Artifact   ArtifactVersion `json:"artifact"`
	DataBase64 string          `json:"dataBase64"`
}

type ListRequest struct {
	Workspace WorkspaceSelector `json:"workspace"`
	Prefix    string            `json:"prefix,omitempty"`
	Limit     int               `json:"limit,omitempty"`
}

type ListResponse struct {
	Items []ArtifactVersion `json:"items"`
}

type DeleteRequest struct {
	Workspace WorkspaceSelector `json:"workspace"`
	Selector  Selector          `json:"selector"`
}

type DeleteResponse struct {
	Deleted  bool            `json:"deleted"`
	Artifact ArtifactVersion `json:"artifact"`
}

type HealthResponse struct {
	Status string `json:"status"`
}

type ShutdownResponse struct {
	Status string `json:"status"`
}
