package domain

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

type SaveTextInput struct {
	Name     string
	Text     string
	MimeType string // optional
}

func (s *Service) SaveText(ctx context.Context, in SaveTextInput) (Artifact, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return Artifact{}, ErrNameRequired
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
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return Artifact{}, ErrNameRequired
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
	ref := newRef()
	sum := sha256.Sum256(data)
	shaHex := hex.EncodeToString(sum[:])

	prevRef, _ := s.repo.Resolve(ctx, name) // ignore errors; empty prevRef if not found

	a := Artifact{
		Ref:       ref,
		Name:      name,
		Kind:      kind,
		MimeType:  mime,
		Filename:  filename,
		SizeBytes: int64(len(data)),
		SHA256:    shaHex,
		CreatedAt: time.Now().UTC(),
		PrevRef:   prevRef,
	}

	return s.repo.Save(ctx, a, data)
}

func (s *Service) Resolve(ctx context.Context, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ErrNameRequired
	}
	return s.repo.Resolve(ctx, name)
}

func (s *Service) Get(ctx context.Context, sel Selector) (Artifact, []byte, error) {
	if strings.TrimSpace(sel.Ref) == "" && strings.TrimSpace(sel.Name) == "" {
		return Artifact{}, nil, ErrRefOrName
	}
	return s.repo.Get(ctx, sel)
}

func (s *Service) List(ctx context.Context, prefix string, limit int) ([]Artifact, error) {
	if limit <= 0 {
		limit = 200
	}
	return s.repo.List(ctx, prefix, limit)
}

func newRef() string {
	// Timestamp + 8 bytes of randomness; safe as a filename.
	// Example: 20260214T083112.123Z-1a2b3c4d5e6f7788
	ts := time.Now().UTC().Format("20060102T150405.000Z")
	rnd := make([]byte, 8)
	_, _ = rand.Read(rnd)
	return ts + "-" + hex.EncodeToString(rnd)
}
