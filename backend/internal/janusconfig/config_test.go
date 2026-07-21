package janusconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseProductionConfiguration(t *testing.T) {
	fixture := filepath.Join("..", "..", "..", "test", "fixtures", "janus", "config_same4_v3_good.txt")
	file, err := os.Open(fixture)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	document, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, want := len(document.Assignments), 103; got != want {
		t.Fatalf("assignment count = %d, want %d", got, want)
	}
	classified, err := document.Classify()
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	if got, want := len(classified), len(document.Assignments); got != want {
		t.Fatalf("classified assignment count = %d, want %d", got, want)
	}
	connections, err := document.Connections()
	if err != nil {
		t.Fatalf("Connections() error = %v", err)
	}
	if err := ValidateProductionTopology(connections); err != nil {
		t.Fatalf("ValidateProductionTopology() error = %v", err)
	}
	if got := connections[0].Host; got != "172.16.0.11" {
		t.Fatalf("first connection host = %q", got)
	}

	last := document.Assignments[len(document.Assignments)-1]
	if last.Name != "TD_CoarseThreshold" || last.Index == nil || *last.Index != 3 || last.Value != "178" {
		t.Fatalf("last assignment = %#v", last)
	}
}

func TestClassifyRejectsUnknownSetting(t *testing.T) {
	document, err := Parse(strings.NewReader("AcquisitionMode SPECTROSCOPY\nAcquistionMode COUNTING\n"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = document.Classify()
	if err == nil || !strings.Contains(err.Error(), `line 2: unsupported JANUS setting "AcquistionMode"`) {
		t.Fatalf("Classify() error = %v", err)
	}
}

func TestClassifyDeclaresSubsystemOwners(t *testing.T) {
	document, err := Parse(strings.NewReader("Open[0] usb:172.16.0.11:tdl:0:0\nHG_Gain 55\nPresetTime 10\nOF_RawData 1\nEHistoNbin 4K\n"))
	if err != nil {
		t.Fatal(err)
	}
	classified, err := document.Classify()
	if err != nil {
		t.Fatal(err)
	}
	want := []Owner{OwnerTopology, OwnerHardware, OwnerRunControl, OwnerStorage, OwnerAnalysis}
	for i := range want {
		if classified[i].Owner != want[i] {
			t.Errorf("assignment %d owner = %q, want %q", i, classified[i].Owner, want[i])
		}
	}
}

func TestParsePreservesValuesContainingSpaces(t *testing.T) {
	document, err := Parse(strings.NewReader("DataFilePath C:\\data files\\run # comment\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := document.Assignments[0].Value; got != `C:\data files\run` {
		t.Fatalf("value = %q", got)
	}
}

func TestParseBoardAndChannelOverride(t *testing.T) {
	document, err := Parse(strings.NewReader("TD_FineThreshold[2][17] 9\n"))
	if err != nil {
		t.Fatal(err)
	}
	a := document.Assignments[0]
	if a.Name != "TD_FineThreshold" || a.Index == nil || *a.Index != 2 || a.Channel == nil || *a.Channel != 17 || a.Value != "9" {
		t.Fatalf("assignment = %#v", a)
	}
}

func TestParseRejectsMissingValue(t *testing.T) {
	_, err := Parse(strings.NewReader("AcquisitionMode\n"))
	if err == nil {
		t.Fatal("Parse() succeeded, want error")
	}
}

func TestConnectionsRejectDuplicateBoard(t *testing.T) {
	document, err := Parse(strings.NewReader("Open[0] usb:172.16.0.11:tdl:0:0\nOpen[0] usb:172.16.0.11:tdl:1:0\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := document.Connections(); err == nil {
		t.Fatal("Connections() succeeded, want duplicate error")
	}
}
