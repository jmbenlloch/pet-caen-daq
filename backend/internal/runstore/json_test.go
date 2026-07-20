package runstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunLifecycleAndReplay(t *testing.T) {
	w, err := Create(t.TempDir(), Manifest{RunID: "54-replay", StartedAt: "2026-07-17T11:06:37Z"})
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"board":0,"trigger_id":"42"}`)
	if err = w.Append(Envelope{Kind: "spectroscopy_timing", Sequence: 1, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(w.Directory(), "incomplete")); err != nil {
		t.Fatal(err)
	}
	if err = w.Finalize("2026-07-17T11:06:52Z", "completed"); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(w.Directory(), "incomplete")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("marker still present: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(w.Directory(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	r := NewReader(bytes.NewReader(data), 0)
	e, err := r.Next()
	if err != nil {
		t.Fatal(err)
	}
	if e.Sequence != 1 || e.Kind != "spectroscopy_timing" || !bytes.Equal(e.Payload, payload) {
		t.Fatalf("event = %#v", e)
	}
	if _, err = r.Next(); err != io.EOF {
		t.Fatalf("end = %v", err)
	}
	manifestData, err := os.ReadFile(filepath.Join(w.Directory(), "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err = json.Unmarshal(manifestData, &m); err != nil {
		t.Fatal(err)
	}
	if m.EventCount != 1 || m.TerminationReason != "completed" {
		t.Fatalf("manifest = %#v", m)
	}
}
func TestAbortLeavesIncompleteMarker(t *testing.T) {
	w, err := Create(t.TempDir(), Manifest{RunID: "interrupted"})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Abort(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(w.Directory(), "incomplete")); err != nil {
		t.Fatal(err)
	}
}
func TestReaderReportsTruncationAndBounds(t *testing.T) {
	for _, tc := range []struct {
		name, data, want string
		max              int
	}{{"truncated", `{"schema_version":1}`, "truncated", 100}, {"oversize", strings.Repeat("x", 20) + "\n", "exceeds", 10}} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewReader(strings.NewReader(tc.data), tc.max).Next()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
