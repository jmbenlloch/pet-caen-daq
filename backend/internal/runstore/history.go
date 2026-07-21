package runstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

var ErrRunNotFound = errors.New("run not found")
var ErrArtifactNotFound = errors.New("artifact not found")

// ListManifests inspects finalized and incomplete runs without changing their
// contents. Non-run entries and symlinks are ignored; corrupt run manifests are
// reported instead of being silently omitted.
func ListManifests(parent string, limit int) ([]Manifest, error) {
	entries, err := os.ReadDir(parent)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list run storage: %w", err)
	}
	manifests := make([]Manifest, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || len(entry.Name()) <= 4 || entry.Name()[:4] != "run-" {
			continue
		}
		runID := entry.Name()[4:]
		manifest, err := ReadManifest(filepath.Join(parent, entry.Name()), runID)
		if err != nil {
			return nil, fmt.Errorf("inspect run %q: %w", runID, err)
		}
		manifests = append(manifests, manifest)
	}
	sort.Slice(manifests, func(i, j int) bool {
		left, right := manifests[i], manifests[j]
		leftTime, rightTime := left.CompletedAt, right.CompletedAt
		if leftTime == "" {
			leftTime = left.StartedAt
		}
		if rightTime == "" {
			rightTime = right.StartedAt
		}
		if leftTime != rightTime {
			return leftTime > rightTime
		}
		return left.RunID > right.RunID
	})
	if limit > 0 && len(manifests) > limit {
		manifests = manifests[:limit]
	}
	return manifests, nil
}

func ReadManifest(directory, expectedRunID string) (Manifest, error) {
	file, err := openRegular(filepath.Join(directory, "manifest.json"))
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{}, ErrRunNotFound
	}
	if err != nil {
		return Manifest{}, fmt.Errorf("open manifest: %w", err)
	}
	defer file.Close()
	limited := io.LimitReader(file, MaxManifestSize+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return Manifest{}, fmt.Errorf("read manifest: %w", err)
	}
	if len(data) > MaxManifestSize {
		return Manifest{}, fmt.Errorf("manifest exceeds %d bytes", MaxManifestSize)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	if manifest.SchemaVersion != SchemaVersion {
		return Manifest{}, fmt.Errorf("unsupported manifest schema version %d", manifest.SchemaVersion)
	}
	if manifest.RunID != expectedRunID {
		return Manifest{}, fmt.Errorf("manifest run ID %q does not match directory run ID %q", manifest.RunID, expectedRunID)
	}
	return manifest, nil
}

// OpenArtifact opens only a regular file explicitly recorded in the run
// manifest. Callers own the returned file.
func OpenArtifact(parent, runID, name string) (*os.File, Artifact, error) {
	directory := filepath.Join(parent, "run-"+runID)
	manifest, err := ReadManifest(directory, runID)
	if err != nil {
		return nil, Artifact{}, err
	}
	var artifact Artifact
	found := false
	for _, candidate := range manifest.Artifacts {
		if candidate.Name == name {
			artifact, found = candidate, true
			break
		}
	}
	if !found || filepath.Base(name) != name || name == "." {
		return nil, Artifact{}, ErrArtifactNotFound
	}
	file, err := openRegular(filepath.Join(directory, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, Artifact{}, ErrArtifactNotFound
	}
	if err != nil {
		return nil, Artifact{}, fmt.Errorf("open artifact: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, Artifact{}, fmt.Errorf("inspect artifact: %w", err)
	}
	if uint64(info.Size()) != artifact.SizeBytes {
		file.Close()
		return nil, Artifact{}, fmt.Errorf("artifact size is %d, manifest records %d", info.Size(), artifact.SizeBytes)
	}
	return file, artifact, nil
}

func openRegular(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s is not a regular file", filepath.Base(path))
	}
	return os.Open(path)
}
