package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/core/artifacts"
	artsqlite "github.com/CeraCharlesCC/CCSubAgents/local-artifact/internal/infrastructure/sqlite"
)

type MigrationReport struct {
	Migrated         bool
	ImportedVersions int
}

type legacyIndex struct {
	Names map[string]string `json:"names"`
}

type legacyVersion struct {
	meta artifacts.ArtifactVersion
	data []byte
}

const stagedWorkspaceDirName = ".migration-stage"

func MigrateWorkspaceIfNeeded(ctx context.Context, workspaceDir string) (MigrationReport, error) {
	legacyPresent, err := hasLegacyData(workspaceDir)
	if err != nil {
		return MigrationReport{}, err
	}
	if !legacyPresent {
		if err := cleanupStagedWorkspace(workspaceDir); err != nil {
			return MigrationReport{}, err
		}
		return MigrationReport{Migrated: false}, nil
	}

	chains, err := loadLegacyChains(workspaceDir)
	if err != nil {
		return MigrationReport{}, err
	}
	if len(chains) == 0 {
		return MigrationReport{}, ErrNoImportableLegacyChains
	}

	stagedRoot := stagedWorkspaceRoot(workspaceDir)
	if err := os.RemoveAll(stagedRoot); err != nil {
		return MigrationReport{}, err
	}
	if err := os.MkdirAll(stagedRoot, 0o755); err != nil {
		return MigrationReport{}, err
	}
	defer func() {
		_ = os.RemoveAll(stagedRoot)
	}()

	repo, err := artsqlite.NewArtifactRepository(stagedRoot)
	if err != nil {
		return MigrationReport{}, err
	}
	closed := false
	defer func() {
		if !closed {
			_ = repo.Close()
		}
	}()

	imported := 0
	for _, chain := range chains {
		for _, item := range chain {
			if _, err := repo.Save(ctx, item.meta, item.data, artifacts.SaveOptions{}); err != nil {
				return MigrationReport{}, fmt.Errorf("import %s/%s: %w", item.meta.Name, item.meta.Ref, err)
			}
			imported++
		}
	}
	if imported == 0 {
		return MigrationReport{}, ErrNoImportableLegacyChains
	}
	if err := repo.Close(); err != nil {
		return MigrationReport{}, err
	}
	closed = true

	if err := installStagedWorkspace(stagedRoot, workspaceDir); err != nil {
		return MigrationReport{}, err
	}
	if err := moveLegacyAside(workspaceDir); err != nil {
		return MigrationReport{}, err
	}

	return MigrationReport{Migrated: true, ImportedVersions: imported}, nil
}

func hasLegacyData(workspaceDir string) (bool, error) {
	if _, err := os.Stat(filepath.Join(workspaceDir, "names.json")); err == nil {
		return true, nil
	} else if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if info, err := os.Stat(filepath.Join(workspaceDir, "meta")); err == nil && info.IsDir() {
		return true, nil
	} else if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if info, err := os.Stat(filepath.Join(workspaceDir, "objects")); err == nil && info.IsDir() {
		return true, nil
	} else if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return false, nil
}

func loadLegacyChains(workspaceDir string) ([][]legacyVersion, error) {
	idxNames, err := loadLegacyIndexNames(workspaceDir)
	if err != nil {
		return nil, err
	}
	versionsByRef, err := loadLegacyVersions(workspaceDir)
	if err != nil {
		return nil, err
	}
	if len(versionsByRef) == 0 {
		return nil, nil
	}

	chains := make([][]legacyVersion, 0, len(idxNames))
	seen := map[string]struct{}{}

	names := make([]string, 0, len(idxNames))
	for name := range idxNames {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		latest := strings.TrimSpace(idxNames[name])
		if latest == "" {
			continue
		}
		chain := walkLegacyChainFromMap(name, latest, versionsByRef, seen)
		if len(chain) == 0 {
			continue
		}
		reverse(chain)
		chains = append(chains, chain)
	}

	for _, tipRef := range findUnseenChainTips(versionsByRef, seen) {
		chain := walkLegacyUnnamedChain(tipRef, versionsByRef, seen)
		if len(chain) == 0 {
			continue
		}
		chainName := chooseLegacyChainName(chain)
		if chainName == "" {
			continue
		}
		for i := range chain {
			chain[i].meta.Name = chainName
		}
		reverse(chain)
		chains = append(chains, chain)
	}

	return chains, nil
}

func loadLegacyIndexNames(workspaceDir string) (map[string]string, error) {
	idx := legacyIndex{Names: map[string]string{}}
	for _, path := range legacyComponentCandidates(workspaceDir, "names.json") {
		b, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if unmarshalErr := json.Unmarshal(b, &idx); unmarshalErr != nil {
			// Best-effort recovery: continue from meta/*.json chain topology.
			idx.Names = map[string]string{}
		}
		break
	}
	if idx.Names == nil {
		idx.Names = map[string]string{}
	}
	return idx.Names, nil
}

func loadLegacyVersions(workspaceDir string) (map[string]legacyVersion, error) {
	refs, err := listLegacyRefs(workspaceDir)
	if err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return map[string]legacyVersion{}, nil
	}

	out := map[string]legacyVersion{}
	for _, ref := range refs {
		meta, err := readLegacyMeta(workspaceDir, ref)
		if err != nil {
			return nil, err
		}
		data, err := readLegacyObject(workspaceDir, ref)
		if err != nil {
			return nil, err
		}

		sum := sha256.Sum256(data)
		digest := hex.EncodeToString(sum[:])
		if strings.TrimSpace(meta.SHA256) != "" && !strings.EqualFold(strings.TrimSpace(meta.SHA256), digest) {
			return nil, fmt.Errorf("sha256 mismatch for ref %s", ref)
		}
		meta.SHA256 = digest
		meta.SizeBytes = int64(len(data))

		out[ref] = legacyVersion{meta: meta, data: data}
	}
	return out, nil
}

func listLegacyRefs(workspaceDir string) ([]string, error) {
	refs := map[string]struct{}{}
	for _, metaDir := range legacyComponentCandidates(workspaceDir, "meta") {
		entries, err := os.ReadDir(metaDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(name, ".json") {
				continue
			}
			ref := strings.TrimSpace(strings.TrimSuffix(name, ".json"))
			if ref == "" {
				continue
			}
			refs[ref] = struct{}{}
		}
	}

	out := make([]string, 0, len(refs))
	for ref := range refs {
		out = append(out, ref)
	}
	sort.Strings(out)
	return out, nil
}

func walkLegacyChainFromMap(name, startRef string, versionsByRef map[string]legacyVersion, seen map[string]struct{}) []legacyVersion {
	out := make([]legacyVersion, 0)
	normalizedName := strings.TrimSpace(name)
	cur := strings.TrimSpace(startRef)
	for cur != "" {
		if _, ok := seen[cur]; ok {
			break
		}
		item, ok := versionsByRef[cur]
		if !ok {
			break
		}
		seen[cur] = struct{}{}
		if normalizedName != "" {
			item.meta.Name = normalizedName
		}
		out = append(out, item)
		cur = strings.TrimSpace(item.meta.PrevRef)
	}
	return out
}

func walkLegacyUnnamedChain(startRef string, versionsByRef map[string]legacyVersion, seen map[string]struct{}) []legacyVersion {
	out := make([]legacyVersion, 0)
	cur := strings.TrimSpace(startRef)
	for cur != "" {
		if _, ok := seen[cur]; ok {
			break
		}
		item, ok := versionsByRef[cur]
		if !ok {
			break
		}
		seen[cur] = struct{}{}
		out = append(out, item)
		cur = strings.TrimSpace(item.meta.PrevRef)
	}
	return out
}

func chooseLegacyChainName(chain []legacyVersion) string {
	for _, item := range chain {
		if name := strings.TrimSpace(item.meta.Name); name != "" {
			return name
		}
	}
	return ""
}

func findUnseenChainTips(versionsByRef map[string]legacyVersion, seen map[string]struct{}) []string {
	hasChild := map[string]struct{}{}
	for _, item := range versionsByRef {
		parent := strings.TrimSpace(item.meta.PrevRef)
		if parent == "" {
			continue
		}
		if _, ok := versionsByRef[parent]; ok {
			hasChild[parent] = struct{}{}
		}
	}

	tips := make([]string, 0)
	for ref := range versionsByRef {
		if _, alreadyImported := seen[ref]; alreadyImported {
			continue
		}
		if _, hasAnyChild := hasChild[ref]; hasAnyChild {
			continue
		}
		tips = append(tips, ref)
	}
	sort.Strings(tips)
	if len(tips) > 0 {
		return tips
	}

	// Degenerate graph (for example, cycles): still attempt deterministic traversal.
	fallback := make([]string, 0)
	for ref := range versionsByRef {
		if _, alreadyImported := seen[ref]; alreadyImported {
			continue
		}
		fallback = append(fallback, ref)
	}
	sort.Strings(fallback)
	return fallback
}

func readLegacyMeta(workspaceDir, ref string) (artifacts.ArtifactVersion, error) {
	for _, path := range legacyComponentCandidates(workspaceDir, filepath.Join("meta", ref+".json")) {
		b, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return artifacts.ArtifactVersion{}, err
		}
		var meta artifacts.ArtifactVersion
		if err := json.Unmarshal(b, &meta); err != nil {
			return artifacts.ArtifactVersion{}, err
		}
		if strings.TrimSpace(meta.Ref) == "" {
			meta.Ref = ref
		}
		return meta, nil
	}
	return artifacts.ArtifactVersion{}, fmt.Errorf("legacy meta missing for ref %s: %w", ref, os.ErrNotExist)
}

func readLegacyObject(workspaceDir, ref string) ([]byte, error) {
	for _, path := range legacyComponentCandidates(workspaceDir, filepath.Join("objects", ref)) {
		b, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		return b, nil
	}
	return nil, fmt.Errorf("legacy object missing for ref %s: %w", ref, os.ErrNotExist)
}

func legacyComponentCandidates(workspaceDir, relative string) []string {
	return []string{
		filepath.Join(workspaceDir, relative),
		filepath.Join(workspaceDir, "legacy", relative),
	}
}

func reverse(items []legacyVersion) {
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
}

func stagedWorkspaceRoot(workspaceDir string) string {
	return filepath.Join(workspaceDir, stagedWorkspaceDirName)
}

func cleanupStagedWorkspace(workspaceDir string) error {
	return os.RemoveAll(stagedWorkspaceRoot(workspaceDir))
}

func installStagedWorkspace(stagedRoot, workspaceDir string) error {
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		return err
	}

	stagedMeta := filepath.Join(stagedRoot, "meta.sqlite")
	if _, err := os.Stat(stagedMeta); err != nil {
		return fmt.Errorf("staged metadata db missing: %w", err)
	}

	for _, suffix := range []string{"", "-wal", "-shm"} {
		finalMeta := filepath.Join(workspaceDir, "meta.sqlite"+suffix)
		if err := os.Remove(finalMeta); err != nil && !os.IsNotExist(err) {
			return err
		}

		stagedPath := filepath.Join(stagedRoot, "meta.sqlite"+suffix)
		if _, err := os.Stat(stagedPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if err := os.Rename(stagedPath, finalMeta); err != nil {
			return err
		}
	}

	stagedBlobs := filepath.Join(stagedRoot, "blobs")
	if _, err := os.Stat(stagedBlobs); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	finalBlobs := filepath.Join(workspaceDir, "blobs")
	if err := os.RemoveAll(finalBlobs); err != nil {
		return err
	}
	if err := os.Rename(stagedBlobs, finalBlobs); err != nil {
		return err
	}

	return nil
}

func moveLegacyAside(workspaceDir string) error {
	legacyDir := filepath.Join(workspaceDir, "legacy")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		return err
	}
	for _, name := range []string{"names.json", "meta", "objects"} {
		src := filepath.Join(workspaceDir, name)
		if _, err := os.Stat(src); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		dst := filepath.Join(legacyDir, name)
		if _, err := os.Stat(dst); err == nil {
			if err := os.RemoveAll(dst); err != nil {
				return err
			}
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.Rename(src, dst); err != nil {
			return err
		}
	}
	return nil
}

func MigrateAllDetectedWorkspaces(ctx context.Context, baseRoot string) error {
	entries, err := os.ReadDir(baseRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if _, err := MigrateWorkspaceIfNeeded(ctx, baseRoot); err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		if len(name) != 64 {
			continue
		}
		if _, err := hex.DecodeString(name); err != nil {
			continue
		}
		if _, err := MigrateWorkspaceIfNeeded(ctx, filepath.Join(baseRoot, name)); err != nil {
			return err
		}
	}
	return nil
}

var ErrNoLegacyData = errors.New("no legacy data")
var ErrNoImportableLegacyChains = errors.New("legacy data present but no importable chains found")
