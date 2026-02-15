package domain

import "context"

type Selector struct {
	Ref  string `json:"ref,omitempty"`
	Name string `json:"name,omitempty"`
}

// Repository persists artifacts.
//
// Layering note: Domain depends on this interface; Infrastructure implements it.
type Repository interface {
	Save(ctx context.Context, a Artifact, data []byte) (Artifact, error)
	Resolve(ctx context.Context, name string) (ref string, err error)
	Get(ctx context.Context, sel Selector) (Artifact, []byte, error)
	List(ctx context.Context, prefix string, limit int) ([]Artifact, error)
	Delete(ctx context.Context, sel Selector) (Artifact, error)
}
