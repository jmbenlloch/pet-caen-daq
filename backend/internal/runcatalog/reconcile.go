package runcatalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
)

type ReconcileProblem struct{ RunID, Error string }

type ReconcileReport struct {
	Indexed, Unchanged, MarkedUnavailable int
	Problems                              []ReconcileProblem
}

// Reconcile makes the catalog agree with the run manifests under parent.
// Invalid manifests are reported, while missing runs are retained but marked
// unavailable so disappearance of acquisition evidence is never concealed.
func (c *Catalog) Reconcile(ctx context.Context, parent string) (ReconcileReport, error) {
	var report ReconcileReport
	entries, err := os.ReadDir(parent)
	if errors.Is(err, os.ErrNotExist) {
		entries = nil
	} else if err != nil {
		return report, fmt.Errorf("list run storage for reconciliation: %w", err)
	}
	incomplete, err := runstore.FindIncomplete(parent)
	if errors.Is(err, os.ErrNotExist) {
		incomplete = nil
	} else if err != nil {
		return report, fmt.Errorf("inspect incomplete runs for reconciliation: %w", err)
	}
	incompleteByID := make(map[string]runstore.IncompleteRun, len(incomplete))
	for _, run := range incomplete {
		incompleteByID[run.RunID] = run
		if run.Problem != "" {
			report.Problems = append(report.Problems, ReconcileProblem{run.RunID, run.Problem})
		}
	}
	known, err := c.catalogHashes(ctx)
	if err != nil {
		return report, err
	}
	seen := make(map[string]struct{})
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasPrefix(entry.Name(), "run-") || len(entry.Name()) == 4 {
			continue
		}
		runID := strings.TrimPrefix(entry.Name(), "run-")
		directory := filepath.Join(parent, entry.Name())
		manifest, err := runstore.ReadManifest(directory, runID)
		if err != nil {
			report.Problems = append(report.Problems, ReconcileProblem{runID, err.Error()})
			continue
		}
		hash, err := hashRegularManifest(filepath.Join(directory, "manifest.json"))
		if err != nil {
			report.Problems = append(report.Problems, ReconcileProblem{runID, err.Error()})
			continue
		}
		seen[runID] = struct{}{}
		current, exists := known[runID]
		if exists && current.hash == hash && current.available {
			report.Unchanged++
			continue
		}
		configuration, err := NormalizeConfiguration(manifest.RequestedConfiguration, manifest.ConfigurationAudit)
		if err != nil {
			report.Problems = append(report.Problems, ReconcileProblem{runID, err.Error()})
			continue
		}
		configurationDigest := sha256.Sum256([]byte(manifest.RequestedConfiguration))
		_, isIncomplete := incompleteByID[runID]
		if err := c.IndexManifest(ctx, IndexRequest{
			Manifest: manifest, ManifestPath: filepath.Join(directory, "manifest.json"),
			ManifestSHA256: hash, ConfigurationSHA256: hex.EncodeToString(configurationDigest[:]),
			Incomplete: isIncomplete, Configuration: configuration,
		}); err != nil {
			return report, fmt.Errorf("reconcile run %q: %w", runID, err)
		}
		report.Indexed++
	}
	for runID := range known {
		if _, ok := seen[runID]; ok {
			continue
		}
		result, err := c.db.ExecContext(ctx, `UPDATE runs SET available = 0 WHERE run_id = ? AND available = 1`, runID)
		if err != nil {
			return report, fmt.Errorf("mark run %q unavailable: %w", runID, err)
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return report, fmt.Errorf("count unavailable run %q: %w", runID, err)
		}
		report.MarkedUnavailable += int(changed)
	}
	sort.Slice(report.Problems, func(i, j int) bool { return report.Problems[i].RunID < report.Problems[j].RunID })
	return report, nil
}

type catalogHash struct {
	hash      string
	available bool
}

func (c *Catalog) catalogHashes(ctx context.Context) (map[string]catalogHash, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT run_id, manifest_sha256, available FROM runs`)
	if err != nil {
		return nil, fmt.Errorf("read catalog reconciliation state: %w", err)
	}
	defer rows.Close()
	result := make(map[string]catalogHash)
	for rows.Next() {
		var runID, hash string
		var available bool
		if err := rows.Scan(&runID, &hash, &available); err != nil {
			return nil, fmt.Errorf("scan catalog reconciliation state: %w", err)
		}
		result[runID] = catalogHash{hash, available}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate catalog reconciliation state: %w", err)
	}
	return result, nil
}

func hashRegularManifest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open manifest for hashing: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("inspect manifest for hashing: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("manifest is not a regular file")
	}
	if info.Size() > runstore.MaxManifestSize {
		return "", fmt.Errorf("manifest exceeds %d bytes", runstore.MaxManifestSize)
	}
	digest := sha256.New()
	read, err := io.Copy(digest, io.LimitReader(file, runstore.MaxManifestSize+1))
	if err != nil {
		return "", fmt.Errorf("hash manifest: %w", err)
	}
	if read > runstore.MaxManifestSize {
		return "", fmt.Errorf("manifest exceeds %d bytes", runstore.MaxManifestSize)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
