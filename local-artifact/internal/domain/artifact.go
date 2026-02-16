package domain

import "time"

type ArtifactKind string

const (
	ArtifactKindText  ArtifactKind = "text"
	ArtifactKindFile  ArtifactKind = "file"
	ArtifactKindImage ArtifactKind = "image"
)

type Artifact struct {
	Ref       string       `json:"ref"`
	Name      string       `json:"name,omitempty"`
	Kind      ArtifactKind `json:"kind"`
	MimeType  string       `json:"mimeType"`
	Filename  string       `json:"filename,omitempty"`
	SizeBytes int64        `json:"sizeBytes"`
	SHA256    string       `json:"sha256"`
	CreatedAt time.Time    `json:"createdAt"`
	PrevRef   string       `json:"prevRef,omitempty"`
}

func (a Artifact) URIByRef() string {
	// URI format: artifact://ref/<ref>
	return "artifact://ref/" + a.Ref
}

func URIByName(nameEscaped string) string {
	// URI format: artifact://name/<url.PathEscape(name)>
	return "artifact://name/" + nameEscaped
}
