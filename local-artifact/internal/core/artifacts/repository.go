package artifacts

import "context"

type Selector struct {
	Ref  string `json:"ref,omitempty"`
	Name string `json:"name,omitempty"`
}

type SaveOptions struct {
	ExpectedPrevRef string
}

// Repository persists immutable versions and mutable name pointers.
type Repository interface {
	Save(ctx context.Context, a ArtifactVersion, data []byte, opts SaveOptions) (ArtifactVersion, error)
	Resolve(ctx context.Context, name string) (ref string, err error)
	Get(ctx context.Context, sel Selector) (ArtifactVersion, []byte, error)
	List(ctx context.Context, prefix string, limit int) ([]ArtifactVersion, error)
	ListVersions(ctx context.Context, name string, limit int) ([]ArtifactVersion, error)
	Delete(ctx context.Context, sel Selector) (ArtifactVersion, error)
}
