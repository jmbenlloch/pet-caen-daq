package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"connectrpc.com/connect"
	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
)

func TestSourceNumbersSurviveRestartAndConcurrentAllocation(t *testing.T) {
	parent := t.TempDir()
	s := &SourceScanService{Parent: parent}
	for n := 1; n <= 3; n++ {
		id, err := s.allocateSourceID(context.Background())
		if err != nil || id != fmt.Sprintf("xy-%d", n) {
			t.Fatalf("id=%s error=%v", id, err)
		}
		s = &SourceScanService{Parent: parent}
	}
	// Reservations retain failed/deleted scan numbers without reusing them.
	if err := s.persist(&daqv1.SourceScan{ScanId: "xy-20"}); err != nil {
		t.Fatal(err)
	}
	id, err := s.allocateSourceID(context.Background())
	if err != nil || id != "xy-21" {
		t.Fatalf("restored record allocation %s %v", id, err)
	}
	var wg sync.WaitGroup
	ids := make(chan string, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			other := &SourceScanService{Parent: parent}
			id, err := other.allocateSourceID(context.Background())
			if err != nil {
				t.Error(err)
				return
			}
			ids <- id
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		if seen[id] {
			t.Fatalf("duplicate ID %s", id)
		}
		seen[id] = true
	}
	if len(seen) != 12 {
		t.Fatalf("allocated %d", len(seen))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.allocateSourceID(ctx); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestSourceHistoryIncludesLegacyAndInterruptedScans(t *testing.T) {
	s := &SourceScanService{Parent: t.TempDir()}
	records := []*daqv1.SourceScan{
		{ScanId: "xy-abcdef0123456789", StartedAt: "2026-09-01T00:00:00Z", State: "completed", CompletedAt: "2026-09-01T00:01:00Z", CompletedPoints: 1, Points: []*daqv1.SourceScanPoint{{RunId: "42"}}},
		{ScanId: "xy-1", StartedAt: "2026-09-02T00:00:00Z", State: "moving"},
		{ScanId: "xy-2", StartedAt: "2026-09-03T00:00:00Z", State: "cancelled", CompletedAt: "2026-09-03T00:01:00Z"},
	}
	for _, record := range records {
		if err := s.persist(record); err != nil {
			t.Fatal(err)
		}
	}
	response, err := s.ListSourceScans(context.Background(), connect.NewRequest(&daqv1.ListSourceScansRequest{Limit: 2}))
	if err != nil {
		t.Fatal(err)
	}
	if response.Msg.TotalCount != 3 || len(response.Msg.Scans) != 2 || response.Msg.Scans[0].ScanId != "xy-2" || response.Msg.Scans[1].State != "failed" {
		t.Fatal(response.Msg)
	}
	response, err = s.ListSourceScans(context.Background(), connect.NewRequest(&daqv1.ListSourceScansRequest{Limit: 2, Offset: 2}))
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Msg.Scans) != 1 || response.Msg.Scans[0].ScanId != records[0].ScanId || response.Msg.Scans[0].TotalPoints != 1 {
		t.Fatal(response.Msg)
	}
	loaded, err := s.GetSourceScan(context.Background(), connect.NewRequest(&daqv1.GetSourceScanRequest{ScanId: records[0].ScanId}))
	if err != nil || loaded.Msg.Scan.Points[0].RunId != "42" {
		t.Fatalf("legacy load %v %v", loaded, err)
	}
	s.scan = records[1]
	response, err = s.ListSourceScans(context.Background(), connect.NewRequest(&daqv1.ListSourceScansRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if response.Msg.Scans[1].State != "moving" {
		t.Fatal("active scan reported interrupted")
	}
	if _, err := s.ListSourceScans(context.Background(), connect.NewRequest(&daqv1.ListSourceScansRequest{Limit: 101})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal(err)
	}
	response, err = s.ListSourceScans(context.Background(), connect.NewRequest(&daqv1.ListSourceScansRequest{Offset: 100}))
	if err != nil || len(response.Msg.Scans) != 0 {
		t.Fatalf("offset %v %v", response, err)
	}
	if err := os.WriteFile(filepath.Join(s.Parent, "source-scans", "xy-broken.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListSourceScans(context.Background(), connect.NewRequest(&daqv1.ListSourceScansRequest{})); connect.CodeOf(err) != connect.CodeInternal {
		t.Fatal("corrupt record silently ignored")
	}
}

func TestSourceHistoryEmpty(t *testing.T) {
	s := &SourceScanService{Parent: t.TempDir()}
	response, err := s.ListSourceScans(context.Background(), connect.NewRequest(&daqv1.ListSourceScansRequest{}))
	if err != nil || response.Msg.TotalCount != 0 || len(response.Msg.Scans) != 0 {
		t.Fatalf("empty %v %v", response, err)
	}
}
