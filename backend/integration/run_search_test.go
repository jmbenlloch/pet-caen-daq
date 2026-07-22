//go:build integration

package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1/daqv1connect"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runcatalog"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/service"
)

func TestGeneratedClientSearchesRunCatalog(t *testing.T) {
	parent := t.TempDir()
	manifest := runstore.Manifest{RunID: "searchable", RequestedBy: "integration", StartedAt: "2026-07-22T12:00:00Z"}
	writer, err := runstore.Create(parent, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Finalize("2026-07-22T12:01:00Z", "operator_stop"); err != nil {
		t.Fatal(err)
	}
	manifest, err = runstore.ReadManifest(filepath.Join(parent, "run-searchable"), "searchable")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := runcatalog.Open(filepath.Join(parent, "catalog.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	mode := "SPECT_TIMING"
	if err := catalog.IndexManifest(context.Background(), runcatalog.IndexRequest{
		Manifest: manifest, ManifestPath: filepath.Join(parent, "run-searchable", "manifest.json"), ManifestSHA256: "manifest-hash",
		Configuration: []runcatalog.ConfigurationValue{{Layer: "requested", Parameter: "AcquisitionMode", Board: -1, Channel: -1, Type: runcatalog.ValueText, Text: &mode, RawValue: mode}},
	}); err != nil {
		t.Fatal(err)
	}

	path, handler := daqv1connect.NewRunServiceHandler(&service.RunService{RunParent: parent, Catalog: catalog})
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	server := httptest.NewServer(mux)
	defer server.Close()
	client := daqv1connect.NewRunServiceClient(server.Client(), server.URL)
	response, err := client.SearchRuns(context.Background(), connect.NewRequest(&daqv1.SearchRunsRequest{Configuration: []*daqv1.ConfigurationPredicate{{
		Parameter: "AcquisitionMode", Layer: daqv1.ConfigurationLayer_CONFIGURATION_LAYER_REQUESTED,
		Comparison: &daqv1.ConfigurationPredicate_Text{Text: &daqv1.TextComparison{Equal: mode}},
	}}}))
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Msg.GetRuns()) != 1 || response.Msg.GetRuns()[0].GetRunId() != "searchable" {
		t.Fatalf("search response=%+v", response.Msg)
	}
}
