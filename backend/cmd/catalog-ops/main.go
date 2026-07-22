package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runcatalog"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: catalog-ops <check|rebuild|backup> [options]")
	}
	switch args[0] {
	case "check":
		flags := flag.NewFlagSet("check", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		runs := flags.String("runs", "./runs", "parent directory containing run-* directories")
		catalog := flags.String("catalog", "./runs/catalog.sqlite3", "SQLite catalog path")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		return check(ctx, *runs, *catalog, output)
	case "rebuild":
		flags := flag.NewFlagSet("rebuild", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		runs := flags.String("runs", "./runs", "parent directory containing run-* directories")
		catalog := flags.String("catalog", "./runs/catalog.sqlite3", "SQLite catalog path")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		return rebuild(ctx, *runs, *catalog, output)
	case "backup":
		flags := flag.NewFlagSet("backup", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		catalog := flags.String("catalog", "./runs/catalog.sqlite3", "SQLite catalog path")
		destination := flags.String("destination", "", "new backup file")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *destination == "" {
			return errors.New("backup requires -destination")
		}
		return backup(*catalog, *destination, output)
	default:
		return fmt.Errorf("unknown catalog operation %q", args[0])
	}
}

type manifestState struct {
	hash     string
	problems []string
}

func inspectManifests(ctx context.Context, parent string) (map[string]manifestState, error) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil, fmt.Errorf("list run storage: %w", err)
	}
	states := make(map[string]manifestState)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasPrefix(entry.Name(), "run-") || len(entry.Name()) == 4 {
			continue
		}
		runID := strings.TrimPrefix(entry.Name(), "run-")
		directory := filepath.Join(parent, entry.Name())
		if _, err := runstore.ReadManifest(directory, runID); err != nil {
			states[runID] = manifestState{problems: []string{err.Error()}}
			continue
		}
		hash, err := hashManifest(filepath.Join(directory, "manifest.json"))
		if err != nil {
			states[runID] = manifestState{problems: []string{err.Error()}}
			continue
		}
		states[runID] = manifestState{hash: hash}
	}
	return states, nil
}

func check(ctx context.Context, runsPath, catalogPath string, output io.Writer) error {
	manifests, err := inspectManifests(ctx, runsPath)
	if err != nil {
		return err
	}
	catalog, err := runcatalog.Open(catalogPath)
	if err != nil {
		return fmt.Errorf("open catalog: %w", err)
	}
	defer catalog.Close()
	runs, err := catalog.List(ctx, runcatalog.Query{IncludeUnavailable: true})
	if err != nil {
		return err
	}
	catalogRuns := make(map[string]runcatalog.Run, len(runs))
	for _, run := range runs {
		catalogRuns[run.RunID] = run
	}
	var problems []string
	for runID, state := range manifests {
		for _, problem := range state.problems {
			problems = append(problems, fmt.Sprintf("run %s: invalid manifest: %s", runID, problem))
		}
		if len(state.problems) != 0 {
			continue
		}
		indexed, ok := catalogRuns[runID]
		if !ok {
			problems = append(problems, fmt.Sprintf("run %s: missing from catalog", runID))
		} else if indexed.ManifestSHA256 != state.hash {
			problems = append(problems, fmt.Sprintf("run %s: manifest hash differs", runID))
		} else if !indexed.Available {
			problems = append(problems, fmt.Sprintf("run %s: incorrectly marked unavailable", runID))
		}
	}
	for runID, indexed := range catalogRuns {
		if _, ok := manifests[runID]; !ok && indexed.Available {
			problems = append(problems, fmt.Sprintf("run %s: catalog says available but run directory is absent", runID))
		}
	}
	sort.Strings(problems)
	for _, problem := range problems {
		fmt.Fprintln(output, problem)
	}
	if len(problems) != 0 {
		return fmt.Errorf("catalog check failed with %d problem(s)", len(problems))
	}
	fmt.Fprintf(output, "catalog check passed: %d manifest(s), %d catalog row(s)\n", len(manifests), len(runs))
	return nil
}

func rebuild(ctx context.Context, runsPath, catalogPath string, output io.Writer) (err error) {
	if err := os.MkdirAll(filepath.Dir(catalogPath), 0o750); err != nil {
		return fmt.Errorf("create catalog directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(catalogPath), ".catalog-rebuild-*.sqlite3")
	if err != nil {
		return fmt.Errorf("create rebuild catalog: %w", err)
	}
	temporaryPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close rebuild placeholder: %w", err)
	}
	defer func() { _ = os.Remove(temporaryPath) }()
	catalog, err := runcatalog.Open(temporaryPath)
	if err != nil {
		return err
	}
	report, reconcileErr := catalog.Reconcile(ctx, runsPath)
	closeErr := catalog.Close()
	if reconcileErr != nil {
		return reconcileErr
	}
	if closeErr != nil {
		return fmt.Errorf("close rebuilt catalog: %w", closeErr)
	}
	if len(report.Problems) != 0 {
		for _, problem := range report.Problems {
			fmt.Fprintf(output, "run %s: %s\n", problem.RunID, problem.Error)
		}
		return fmt.Errorf("refusing to replace catalog: %d manifest problem(s)", len(report.Problems))
	}
	validated, err := runcatalog.Open(temporaryPath)
	if err != nil {
		return fmt.Errorf("validate rebuilt catalog: %w", err)
	}
	_, listErr := validated.List(ctx, runcatalog.Query{IncludeUnavailable: true})
	closeErr = validated.Close()
	if listErr != nil {
		return fmt.Errorf("validate rebuilt catalog contents: %w", listErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close validated catalog: %w", closeErr)
	}
	if err := os.Rename(temporaryPath, catalogPath); err != nil {
		return fmt.Errorf("atomically replace catalog (stop the backend before rebuilding): %w", err)
	}
	fmt.Fprintf(output, "catalog rebuilt: %d run(s) indexed at %s\n", report.Indexed, catalogPath)
	return nil
}

func backup(catalogPath, destination string, output io.Writer) (err error) {
	if _, err := os.Stat(catalogPath + "-wal"); err == nil {
		return errors.New("catalog has a WAL sidecar; stop the backend and checkpoint the database before backup")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect catalog WAL sidecar: %w", err)
	}
	source, err := os.Open(catalogPath)
	if err != nil {
		return fmt.Errorf("open catalog: %w", err)
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return fmt.Errorf("inspect catalog: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("catalog is not a regular file")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}
	temporary, err := os.OpenFile(destination+".partial", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create backup: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		if err != nil {
			_ = os.Remove(temporaryPath)
		}
	}()
	if _, err = io.Copy(temporary, source); err != nil {
		return fmt.Errorf("copy catalog backup: %w", err)
	}
	if err = temporary.Sync(); err != nil {
		return fmt.Errorf("sync catalog backup: %w", err)
	}
	if err = temporary.Close(); err != nil {
		return fmt.Errorf("close catalog backup: %w", err)
	}
	validated, err := runcatalog.Open(temporaryPath)
	if err != nil {
		return fmt.Errorf("validate catalog backup: %w", err)
	}
	if _, err = validated.List(context.Background(), runcatalog.Query{IncludeUnavailable: true, Limit: 1}); err != nil {
		_ = validated.Close()
		return fmt.Errorf("validate catalog backup contents: %w", err)
	}
	if err = validated.Close(); err != nil {
		return fmt.Errorf("close validated backup: %w", err)
	}
	// A hard link publishes the already-synchronized file atomically and, unlike
	// rename on Unix, fails rather than replacing an existing backup.
	if err = os.Link(temporaryPath, destination); err != nil {
		return fmt.Errorf("publish catalog backup: %w", err)
	}
	if err = os.Remove(temporaryPath); err != nil {
		return fmt.Errorf("remove published backup staging link: %w", err)
	}
	fmt.Fprintf(output, "catalog backup created at %s\n", destination)
	return nil
}

func hashManifest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
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
		return "", err
	}
	if read > runstore.MaxManifestSize {
		return "", fmt.Errorf("manifest exceeds %d bytes", runstore.MaxManifestSize)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
