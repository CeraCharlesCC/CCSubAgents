package mcp

import (
	"context"
	"fmt"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/artifacts"
)

type unavailableRepository struct {
	cause error
}

func newUnavailableService(cause error) *artifacts.Service {
	return artifacts.NewService(unavailableRepository{cause: cause})
}

func (r unavailableRepository) fail() error {
	if r.cause == nil {
		return fmt.Errorf("%w: workspace service unavailable", artifacts.ErrInternal)
	}
	return fmt.Errorf("%w: workspace service unavailable: %v", artifacts.ErrInternal, r.cause)
}

func (r unavailableRepository) Save(_ context.Context, _ artifacts.ArtifactVersion, _ []byte, _ artifacts.SaveOptions) (artifacts.ArtifactVersion, error) {
	return artifacts.ArtifactVersion{}, r.fail()
}

func (r unavailableRepository) Resolve(_ context.Context, _ string) (string, error) {
	return "", r.fail()
}

func (r unavailableRepository) Get(_ context.Context, _ artifacts.Selector) (artifacts.ArtifactVersion, []byte, error) {
	return artifacts.ArtifactVersion{}, nil, r.fail()
}

func (r unavailableRepository) List(_ context.Context, _ string, _ int) ([]artifacts.ArtifactVersion, error) {
	return nil, r.fail()
}

func (r unavailableRepository) ListVersions(_ context.Context, _ string, _ int) ([]artifacts.ArtifactVersion, error) {
	return nil, r.fail()
}

func (r unavailableRepository) Delete(_ context.Context, _ artifacts.Selector) (artifacts.ArtifactVersion, error) {
	return artifacts.ArtifactVersion{}, r.fail()
}
