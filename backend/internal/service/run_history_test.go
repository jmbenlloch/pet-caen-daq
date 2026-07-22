package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runcatalog"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
)

func TestRunHistory(t *testing.T) {
	parent := t.TempDir()
	writer, err := runstore.Create(parent, runstore.Manifest{RunID: "54", StartedAt: "2026-07-21T10:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Append(runstore.Envelope{Kind: "test", Payload: []byte(`{"value":54}`)}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Finalize("2026-07-21T10:01:00Z", "operator_stop"); err != nil {
		t.Fatal(err)
	}

	service := &RunService{RunParent: parent}
	listed, err := service.ListRuns(context.Background(), connect.NewRequest(&daqv1.ListRunsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Msg.Runs) != 1 || listed.Msg.Runs[0].GetRunId() != "54" || listed.Msg.Runs[0].GetEventCount() != 1 || listed.Msg.Runs[0].GetIncomplete() {
		t.Fatalf("runs = %+v", listed.Msg.Runs)
	}

	_, err = service.ListRuns(context.Background(), connect.NewRequest(&daqv1.ListRunsRequest{Limit: 101}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("limit error = %v", err)
	}
}

func TestSearchRunsUsesTypedPredicatesAndPagination(t *testing.T) {
	parent := t.TempDir()
	catalog, err := runcatalog.Open(filepath.Join(parent, "catalog.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	threshold := int64(220)
	for index, id := range []string{"newer", "older"} {
		started := []string{"2026-07-22T12:00:00Z", "2026-07-22T11:00:00Z"}[index]
		writer, err := runstore.Create(parent, runstore.Manifest{RunID: id, StartedAt: started, RequestedBy: "operator"})
		if err != nil {
			t.Fatal(err)
		}
		if err := writer.Finalize(started, "operator_stop"); err != nil {
			t.Fatal(err)
		}
		manifest, err := runstore.ReadManifest(filepath.Join(parent, "run-"+id), id)
		if err != nil {
			t.Fatal(err)
		}
		if err := catalog.IndexManifest(context.Background(), runcatalog.IndexRequest{
			Manifest: manifest, ManifestPath: filepath.Join(parent, "run-"+id, "manifest.json"), ManifestSHA256: "hash-" + id,
			Configuration: []runcatalog.ConfigurationValue{{Layer: "resolved", Parameter: "TD_CoarseThreshold", Board: 2, Channel: -1, Type: runcatalog.ValueInteger, Integer: &threshold, RawValue: "220"}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	minimum := int64(200)
	request := &daqv1.SearchRunsRequest{Limit: 1, Configuration: []*daqv1.ConfigurationPredicate{{
		Parameter: "TD_CoarseThreshold", Layer: daqv1.ConfigurationLayer_CONFIGURATION_LAYER_RESOLVED,
		Scope:      &daqv1.ConfigurationScope{Scope: &daqv1.ConfigurationScope_Board{Board: 2}},
		Comparison: &daqv1.ConfigurationPredicate_Integer{Integer: &daqv1.IntegerComparison{Minimum: &minimum}},
	}}}
	service := &RunService{RunParent: parent, Catalog: catalog}
	first, err := service.SearchRuns(context.Background(), connect.NewRequest(request))
	if err != nil || len(first.Msg.GetRuns()) != 1 || first.Msg.GetRuns()[0].GetRunId() != "newer" || first.Msg.GetNextPageToken() == "" {
		t.Fatalf("first=%+v error=%v", first, err)
	}
	request.PageToken = first.Msg.GetNextPageToken()
	second, err := service.SearchRuns(context.Background(), connect.NewRequest(request))
	if err != nil || len(second.Msg.GetRuns()) != 1 || second.Msg.GetRuns()[0].GetRunId() != "older" || second.Msg.GetNextPageToken() != "" {
		t.Fatalf("second=%+v error=%v", second, err)
	}
}

func TestSearchRunsValidatesContractAndCatalogAvailability(t *testing.T) {
	service := &RunService{}
	_, err := service.SearchRuns(context.Background(), connect.NewRequest(&daqv1.SearchRunsRequest{}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("missing catalog error=%v", err)
	}
	catalog, openErr := runcatalog.Open(filepath.Join(t.TempDir(), "catalog.sqlite3"))
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer catalog.Close()
	service.Catalog = catalog
	service.RunParent = t.TempDir()
	_, err = service.SearchRuns(context.Background(), connect.NewRequest(&daqv1.SearchRunsRequest{PageToken: "not-base64"}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("page token error=%v", err)
	}
	_, err = service.SearchRuns(context.Background(), connect.NewRequest(&daqv1.SearchRunsRequest{Configuration: []*daqv1.ConfigurationPredicate{{Parameter: "HV_Vbias"}}}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("predicate error=%v", err)
	}
}

func TestCompletedRunReconcilesCatalogWithoutFailingOnCatalogError(t *testing.T) {
	called := false
	reported := error(nil)
	service := &RunService{
		RunParent: "runs",
		ReconcileCatalog: func(_ context.Context, parent string) error {
			called = parent == "runs"
			return errors.New("catalog locked")
		},
		CatalogError: func(err error) { reported = err },
	}
	service.reconcileCatalog(context.Background())
	if !called || reported == nil || reported.Error() != "catalog locked" {
		t.Fatalf("called=%t reported=%v", called, reported)
	}
}
