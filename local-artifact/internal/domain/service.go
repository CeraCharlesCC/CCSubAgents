package domain

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	Name     string
	Text     string
	MimeType string // optional
}

func (s *Service) SaveText(ctx context.Context, in SaveTextInput) (Artifact, error) {
	name, err := normalizeAndValidateName(in.Name)
	if err != nil {
		return Artifact{}, err
	}
	mime := strings.TrimSpace(in.MimeType)
	if mime == "" {
		mime = "text/plain; charset=utf-8"
	}
	data := []byte(in.Text)
	return s.save(ctx, name, ArtifactKindText, mime, "", data)
}

type SaveBlobInput struct {
	Name     string
	Data     []byte
	MimeType string
	Filename string
}

func (s *Service) SaveBlob(ctx context.Context, in SaveBlobInput) (Artifact, error) {
	name, err := normalizeAndValidateName(in.Name)
	if err != nil {
		return Artifact{}, err
	}
	mime := strings.TrimSpace(in.MimeType)
	if mime == "" {
		return Artifact{}, fmt.Errorf("%w: mimeType is required", ErrInvalidInput)
	}
	kind := ArtifactKindFile
	if strings.HasPrefix(strings.ToLower(mime), "image/") {
		kind = ArtifactKindImage
	} else if strings.HasPrefix(strings.ToLower(mime), "text/") {
		kind = ArtifactKindText
	}
	return s.save(ctx, name, kind, mime, strings.TrimSpace(in.Filename), in.Data)
}

func (s *Service) save(ctx context.Context, name string, kind ArtifactKind, mime string, filename string, data []byte) (Artifact, error) {
	if data == nil {
		data = []byte{}
	}

	prevRef := ""
	if resolvedRef, err := s.repo.Resolve(ctx, name); err == nil {
		prevRef = resolvedRef
	} else if err != nil && !errors.Is(err, ErrNotFound) {
		return Artifact{}, err
	}

	ref, err := s.refGenerator()
	if err != nil {
		return Artifact{}, fmt.Errorf("%w: generate ref: %v", ErrInternal, err)
	}
	sum := sha256.Sum256(data)
	shaHex := hex.EncodeToString(sum[:])

	a := Artifact{
		Ref:       ref,
		Name:      name,
		Kind:      kind,
		MimeType:  mime,
		Filename:  filename,
		SizeBytes: int64(len(data)),
		SHA256:    shaHex,
		CreatedAt: nowUTCSecond(),
		PrevRef:   prevRef,
	}

	return s.repo.Save(ctx, a, data)
}

func (s *Service) Resolve(ctx context.Context, name string) (string, error) {
	norm, err := normalizeAndValidateName(name)
	if err != nil {
		return "", err
	}
	return s.repo.Resolve(ctx, norm)
}

func (s *Service) Get(ctx context.Context, sel Selector) (Artifact, []byte, error) {
	normSel, err := normalizeSelector(sel)
	if err != nil {
		return Artifact{}, nil, err
	}
	return s.repo.Get(ctx, normSel)
}

func (s *Service) Delete(ctx context.Context, sel Selector) (Artifact, error) {
	normSel, err := normalizeSelector(sel)
	if err != nil {
		return Artifact{}, err
	}
	return s.repo.Delete(ctx, normSel)
}

func (s *Service) List(ctx context.Context, prefix string, limit int) ([]Artifact, error) {
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
	// Timestamp + 8 bytes of randomness; safe as a filename.
	// Example: 20260214T083112Z-1a2b3c4d5e6f7788
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
