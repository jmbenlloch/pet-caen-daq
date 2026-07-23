//go:build hdf5

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/hdf5store"
)

func main() {
	source := flag.String("source", "", "finalized JSON development run directory")
	output := flag.String("output", "", "new benchmark HDF5 output path")
	flag.Parse()
	if *source == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "both -source and -output are required")
		os.Exit(2)
	}
	result, err := hdf5store.BenchmarkJSONRun(*source, *output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "benchmark HDF5 writer: %v\n", err)
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintf(os.Stderr, "encode benchmark result: %v\n", err)
		os.Exit(1)
	}
}
