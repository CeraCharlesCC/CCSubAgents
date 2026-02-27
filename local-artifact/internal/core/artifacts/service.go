package artifacts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"
)

type Service struct {
	repo         Repository
	refGenerator func() (string, error)
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo, refGenerator: newRef}
}

type SaveTextInput struct {
	Name            string
	Text            string
	MimeType        string
	ExpectedPrevRef string
}

func (s *Service) SaveText(ctx context.Context, in SaveTextInput) (ArtifactVersion, error) {
	name, err := normalizeAndValidateName(in.Name)
	if err != nil {
		return ArtifactVersion{}, err
	}
	if strings.TrimSpace(in.Text) == "" {
		return ArtifactVersion{}, fmt.Errorf("%w: text is required", ErrInvalidInput)
	}
	mime := strings.TrimSpace(in.MimeType)
	if mime == "" {
		mime = "text/plain; charset=utf-8"
	}
	data := []byte(in.Text)
	return s.saveWithOptions(ctx, name, ArtifactKindText, mime, "", data, SaveOptions{ExpectedPrevRef: in.ExpectedPrevRef})
}

type SaveBlobInput struct {
	Name            string
	Data            []byte
	MimeType        string
	Filename        string
	ExpectedPrevRef string
}

func (s *Service) SaveBlob(ctx context.Context, in SaveBlobInput) (ArtifactVersion, error) {
	name, err := normalizeAndValidateName(in.Name)
	if err != nil {
		return ArtifactVersion{}, err
	}
	mime := strings.TrimSpace(in.MimeType)
	if mime == "" {
		return ArtifactVersion{}, fmt.Errorf("%w: mimeType is required", ErrInvalidInput)
	}
	kind := ArtifactKindFile
	if strings.HasPrefix(strings.ToLower(mime), "image/") {
		kind = ArtifactKindImage
	} else if strings.HasPrefix(strings.ToLower(mime), "text/") {
		kind = ArtifactKindText
	}
	return s.saveWithOptions(ctx, name, kind, mime, strings.TrimSpace(in.Filename), in.Data, SaveOptions{ExpectedPrevRef: in.ExpectedPrevRef})
}

func (s *Service) saveWithOptions(ctx context.Context, name string, kind ArtifactKind, mime string, filename string, data []byte, opts SaveOptions) (ArtifactVersion, error) {
	if data == nil {
		data = []byte{}
	}
	if strings.TrimSpace(opts.ExpectedPrevRef) != "" {
		normExpected, err := normalizeAndValidateRef(opts.ExpectedPrevRef)
		if err != nil {
			return ArtifactVersion{}, err
		}
		opts.ExpectedPrevRef = normExpected
	}

	ref, err := s.refGenerator()
	if err != nil {
		return ArtifactVersion{}, fmt.Errorf("%w: generate ref: %v", ErrInternal, err)
	}
	sum := sha256.Sum256(data)
	shaHex := hex.EncodeToString(sum[:])

	a := ArtifactVersion{
		Ref:       ref,
		Name:      name,
		Kind:      kind,
		MimeType:  mime,
		Filename:  filename,
		SizeBytes: int64(len(data)),
		SHA256:    shaHex,
		CreatedAt: nowUTCSecond(),
	}

	return s.repo.Save(ctx, a, data, opts)
}

func (s *Service) Resolve(ctx context.Context, name string) (string, error) {
	norm, err := normalizeAndValidateName(name)
	if err != nil {
		return "", err
	}
	return s.repo.Resolve(ctx, norm)
}

func (s *Service) Get(ctx context.Context, sel Selector) (ArtifactVersion, []byte, error) {
	normSel, err := normalizeSelector(sel)
	if err != nil {
		return ArtifactVersion{}, nil, err
	}
	return s.repo.Get(ctx, normSel)
}

func (s *Service) Delete(ctx context.Context, sel Selector) (ArtifactVersion, error) {
	normSel, err := normalizeSelector(sel)
	if err != nil {
		return ArtifactVersion{}, err
	}
	return s.repo.Delete(ctx, normSel)
}

func (s *Service) List(ctx context.Context, prefix string, limit int) ([]ArtifactVersion, error) {
	normPrefix, err := normalizePrefix(prefix)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		return nil, fmt.Errorf("%w: limit must be <= 1000", ErrInvalidInput)
	}
	return s.repo.List(ctx, normPrefix, limit)
}

func newRef() (string, error) {
	ts := nowUTCSecond().Format("20060102T150405Z")
	rnd := make([]byte, 8)
	if _, err := io.ReadFull(rand.Reader, rnd); err != nil {
		return "", err
	}
	return ts + "-" + hex.EncodeToString(rnd), nil
}

func nowUTCSecond() time.Time {
	return time.Now().UTC().Truncate(time.Second)
}
