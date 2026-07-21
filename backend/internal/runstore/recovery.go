package runstore

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const MaxManifestSize = 4 << 20

// IncompleteRun is read-only restart evidence. Problem is populated when the
// marker exists but its manifest cannot be trusted; discovery never repairs,
// finalizes, renames, or removes run artifacts.
type IncompleteRun struct {
	RunID     string
	Directory string
	Manifest  *Manifest
	Problem   string
}

func FindIncomplete(parent string) ([]IncompleteRun, error) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil, fmt.Errorf("read run storage directory: %w", err)
	}
	var runs []IncompleteRun
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "run-") {
			continue
		}
		directory := filepath.Join(parent, entry.Name())
		marker, err := os.Lstat(filepath.Join(directory, "incomplete"))
		if os.IsNotExist(err) {
			continue
		}
		candidate := IncompleteRun{RunID: strings.TrimPrefix(entry.Name(), "run-"), Directory: directory}
		if err != nil {
			candidate.Problem = fmt.Sprintf("inspect incomplete marker: %v", err)
			runs = append(runs, candidate)
			continue
		}
		if !marker.Mode().IsRegular() {
			candidate.Problem = "incomplete marker is not a regular file"
			runs = append(runs, candidate)
			continue
		}
		manifest, problem := readRecoveryManifest(filepath.Join(directory, "manifest.json"), candidate.RunID)
		candidate.Manifest, candidate.Problem = manifest, problem
		runs = append(runs, candidate)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].RunID < runs[j].RunID })
	return runs, nil
}

func readRecoveryManifest(path, directoryRunID string) (*Manifest, string) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Sprintf("open manifest: %v", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxManifestSize+1))
	if err != nil {
		return nil, fmt.Sprintf("read manifest: %v", err)
	}
	if len(data) > MaxManifestSize {
		return nil, fmt.Sprintf("manifest exceeds %d bytes", MaxManifestSize)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Sprintf("decode manifest: %v", err)
	}
	if manifest.SchemaVersion != SchemaVersion {
		return nil, fmt.Sprintf("unsupported manifest schema version %d", manifest.SchemaVersion)
	}
	if manifest.RunID != directoryRunID {
		return nil, fmt.Sprintf("manifest run ID %q does not match directory run ID %q", manifest.RunID, directoryRunID)
	}
	if manifest.CompletedAt != "" {
		return &manifest, "incomplete marker remains beside a completed manifest"
	}
	return &manifest, ""
}
