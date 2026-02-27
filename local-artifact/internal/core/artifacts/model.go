package artifacts

import "time"

type ArtifactKind string

const (
	ArtifactKindText  ArtifactKind = "text"
	ArtifactKindFile  ArtifactKind = "file"
	ArtifactKindImage ArtifactKind = "image"
)

type ArtifactVersion struct {
	Ref       string       `json:"ref"`
	Name      string       `json:"name,omitempty"`
	Kind      ArtifactKind `json:"kind"`
	MimeType  string       `json:"mimeType"`
	Filename  string       `json:"filename,omitempty"`
	SizeBytes int64        `json:"sizeBytes"`
	SHA256    string       `json:"sha256,omitempty"`
	CreatedAt time.Time    `json:"createdAt"`
	PrevRef   string       `json:"prevRef,omitempty"`
	Tombstone bool         `json:"tombstone,omitempty"`
}

// Artifact remains as an alias for compatibility with existing call sites.
type Artifact = ArtifactVersion

func (a ArtifactVersion) URIByRef() string {
	return "artifact://ref/" + a.Ref
}

func URIByName(nameEscaped string) string {
	return "artifact://name/" + nameEscaped
}
