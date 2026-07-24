//go:build !hdf5

package runpipeline

import (
	"strings"
	"testing"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
)

func TestJSONBuildRejectsHistogramHDF5PersistenceAtRunCreation(t *testing.T) {
	factory := Factory{Options: Options{
		Parent: t.TempDir(), Capacity: 1, Backpressure: acquisition.BackpressureBlock, Now: time.Now,
	}}
	_, err := factory.New("histograms", acquisition.RunOptions{PersistHistograms: true})
	if err == nil || !strings.Contains(err.Error(), "HDF5-enabled build") {
		t.Fatalf("error = %v", err)
	}
}
