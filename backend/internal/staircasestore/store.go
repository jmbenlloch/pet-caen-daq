package staircasestore

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/staircase"
)

const SchemaVersion = 1

type Manifest struct {
	SchemaVersion     int                `json:"schema_version"`
	ScanID            string             `json:"scan_id"`
	Board             uint32             `json:"board"`
	RequestedBy       string             `json:"requested_by"`
	StartedAt         string             `json:"started_at"`
	CompletedAt       string             `json:"completed_at,omitempty"`
	State             string             `json:"state"`
	TerminationReason string             `json:"termination_reason,omitempty"`
	Request           staircase.Request  `json:"request"`
	CompletedPoints   uint32             `json:"completed_points"`
	Restored          bool               `json:"restored"`
	Artifact          *runstore.Artifact `json:"artifact,omitempty"`
}

type Writer struct {
	dir      string
	manifest Manifest
	file     *os.File
	encoder  *json.Encoder
	points   []staircase.Point
	closed   bool
}

func Create(parent string, manifest Manifest) (*Writer, error) {
	if strings.TrimSpace(manifest.ScanID) == "" {
		return nil, errors.New("scan ID is required")
	}
	manifest.SchemaVersion = SchemaVersion
	manifest.State = "running"
	dir := filepath.Join(parent, "scan-"+manifest.ScanID)
	if err := os.Mkdir(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create scan directory: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(dir, "points.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return nil, err
	}
	writer := &Writer{dir: dir, manifest: manifest, file: file, encoder: json.NewEncoder(file)}
	if err := writer.writeManifest(); err != nil {
		file.Close()
		return nil, err
	}
	return writer, nil
}

func (w *Writer) Append(point staircase.Point) error {
	if w.closed {
		return errors.New("scan writer is closed")
	}
	if err := w.encoder.Encode(point); err != nil {
		return fmt.Errorf("append scan point: %w", err)
	}
	if err := w.file.Sync(); err != nil {
		return fmt.Errorf("sync scan point: %w", err)
	}
	w.points = append(w.points, point)
	w.manifest.CompletedPoints = uint32(len(w.points))
	return w.writeManifest()
}

func (w *Writer) Finalize(completedAt, state, reason string, restored bool) error {
	if w.closed {
		return errors.New("scan writer is closed")
	}
	w.closed = true
	if err := w.file.Close(); err != nil {
		return err
	}
	w.manifest.CompletedAt = completedAt
	w.manifest.State = state
	w.manifest.TerminationReason = reason
	w.manifest.Restored = restored
	artifactPath, kind, err := buildArtifact(w.dir, w.manifest, w.points)
	if err != nil {
		return err
	}
	info, err := os.Stat(artifactPath)
	if err != nil {
		return err
	}
	sum, err := fileSHA256(artifactPath)
	if err != nil {
		return err
	}
	w.manifest.Artifact = &runstore.Artifact{Kind: kind, Name: filepath.Base(artifactPath), SizeBytes: uint64(info.Size()), SHA256: sum}
	return w.writeManifest()
}

func (w *Writer) writeManifest() error {
	data, err := json.MarshalIndent(w.manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := filepath.Join(w.dir, "manifest.json.tmp")
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(w.dir, "manifest.json"))
}

func Read(parent, scanID string) (Manifest, []staircase.Point, error) {
	manifest, dir, err := readManifest(parent, scanID)
	if err != nil {
		return Manifest{}, nil, err
	}
	file, err := os.Open(filepath.Join(dir, "points.jsonl"))
	if err != nil {
		return Manifest{}, nil, err
	}
	defer file.Close()
	var points []staircase.Point
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var point staircase.Point
		if err := json.Unmarshal(scanner.Bytes(), &point); err != nil {
			return Manifest{}, nil, err
		}
		points = append(points, point)
	}
	return manifest, points, scanner.Err()
}

func ReadManifest(parent, scanID string) (Manifest, error) {
	manifest, _, err := readManifest(parent, scanID)
	return manifest, err
}

func readManifest(parent, scanID string) (Manifest, string, error) {
	if strings.ContainsAny(scanID, `/\`) || scanID == "" {
		return Manifest{}, "", errors.New("invalid scan ID")
	}
	dir := filepath.Join(parent, "scan-"+scanID)
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return Manifest{}, "", err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, "", err
	}
	return manifest, dir, nil
}

func List(parent string, limit int) ([]Manifest, error) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil, err
	}
	var manifests []Manifest
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "scan-") {
			continue
		}
		manifest, err := ReadManifest(parent, strings.TrimPrefix(entry.Name(), "scan-"))
		if err == nil && manifest.CompletedAt != "" {
			manifests = append(manifests, manifest)
		}
	}
	sort.Slice(manifests, func(i, j int) bool { return manifests[i].StartedAt > manifests[j].StartedAt })
	if limit > 0 && len(manifests) > limit {
		manifests = manifests[:limit]
	}
	return manifests, nil
}

func OpenArtifact(parent, scanID, name string) (*os.File, error) {
	if strings.ContainsAny(scanID, `/\`) || scanID == "" || filepath.Base(name) != name {
		return nil, errors.New("invalid scan artifact identity")
	}
	manifest, err := ReadManifest(parent, scanID)
	if err != nil {
		return nil, err
	}
	if manifest.Artifact == nil || manifest.Artifact.Name != name {
		return nil, os.ErrNotExist
	}
	return os.Open(filepath.Join(parent, "scan-"+scanID, name))
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := file.WriteTo(hash); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func NewManifest(scanID string, board uint32, actor string, request staircase.Request, now time.Time) Manifest {
	return Manifest{ScanID: scanID, Board: board, RequestedBy: actor, StartedAt: now.UTC().Format(time.RFC3339Nano), Request: request}
}
