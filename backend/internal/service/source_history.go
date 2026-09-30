package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// Reservations survive restarts and deletion of scan records. Atomic Mkdir
// prevents two processes from allocating the same number in a shared run root.
func (s *SourceScanService) allocateSourceID(ctx context.Context) (string, error) {
	dir := filepath.Join(s.Parent, "source-scans")
	reservations := filepath.Join(dir, ".allocated")
	if err := os.MkdirAll(reservations, 0750); err != nil {
		return "", err
	}
	var maximum uint64
	for _, path := range []string{dir, reservations} {
		entries, err := os.ReadDir(path)
		if err != nil {
			return "", err
		}
		for _, entry := range entries {
			name := strings.TrimSuffix(entry.Name(), ".json")
			if !strings.HasPrefix(name, "xy-") {
				continue
			}
			n, err := strconv.ParseUint(strings.TrimPrefix(name, "xy-"), 10, 64)
			if err == nil && n > maximum {
				maximum = n
			}
		}
	}
	for maximum < math.MaxUint64 {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		maximum++
		id := fmt.Sprintf("xy-%d", maximum)
		if err := os.Mkdir(filepath.Join(reservations, id), 0750); errors.Is(err, os.ErrExist) {
			continue
		} else if err != nil {
			return "", err
		}
		return id, nil
	}
	return "", fmt.Errorf("source scan number exhausted")
}

func (s *SourceScanService) ListSourceScans(ctx context.Context, req *connect.Request[daqv1.ListSourceScansRequest]) (*connect.Response[daqv1.ListSourceScansResponse], error) {
	limit := req.Msg.Limit
	if limit == 0 {
		limit = 20
	}
	if limit > 100 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("limit must be at most 100"))
	}
	if s.Parent == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("source scan storage is not configured"))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(filepath.Join(s.Parent, "source-scans"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	summaries := make([]*daqv1.SourceScanHistoryEntry, 0)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, connect.NewError(connect.CodeCanceled, err)
		}
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !validRunID.MatchString(id) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.Parent, "source-scans", entry.Name()))
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		scan := &daqv1.SourceScan{}
		if err := protojson.Unmarshal(data, scan); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("read source scan %s: %w", id, err))
		}
		if scan.ScanId != id {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("source scan identity mismatch: %s", id))
		}
		if s.scan != nil && s.scan.ScanId == id {
			scan = s.scan
		} else if scan.CompletedAt == "" {
			scan.State = "failed"
		}
		summaries = append(summaries, &daqv1.SourceScanHistoryEntry{ScanId: id, StartedAt: scan.StartedAt, CompletedAt: scan.CompletedAt, State: scan.State, CompletedPoints: scan.CompletedPoints, TotalPoints: uint32(len(scan.Points)), RequestedBy: scan.RequestedBy})
	}
	sort.Slice(summaries, func(i, j int) bool {
		left, _ := time.Parse(time.RFC3339Nano, summaries[i].StartedAt)
		right, _ := time.Parse(time.RFC3339Nano, summaries[j].StartedAt)
		if left.Equal(right) {
			ln, le := strconv.ParseUint(strings.TrimPrefix(summaries[i].ScanId, "xy-"), 10, 64)
			rn, re := strconv.ParseUint(strings.TrimPrefix(summaries[j].ScanId, "xy-"), 10, 64)
			if le == nil && re == nil {
				return ln > rn
			}
			return summaries[i].ScanId > summaries[j].ScanId
		}
		return left.After(right)
	})
	total := uint32(len(summaries))
	start := min(req.Msg.Offset, total)
	end := start + min(limit, total-start)
	return connect.NewResponse(&daqv1.ListSourceScansResponse{Scans: summaries[start:end], TotalCount: total}), nil
}
