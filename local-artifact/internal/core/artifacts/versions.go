package artifacts

import "context"

func (s *Service) ListVersions(ctx context.Context, name string, limit int) ([]ArtifactVersion, error) {
	norm, err := normalizeAndValidateName(name)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		return nil, ErrInvalidInput
	}
	return s.repo.ListVersions(ctx, norm, limit)
}
