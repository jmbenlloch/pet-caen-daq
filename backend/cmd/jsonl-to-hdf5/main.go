//go:build hdf5

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/hdf5store"
)

func main() {
	source := flag.String("source", "", "finalized JSON development run directory")
	output := flag.String("output", "", "new HDF5 output path")
	flag.Parse()
	if *source == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "both -source and -output are required")
		os.Exit(2)
	}
	if err := hdf5store.ConvertJSONRun(*source, *output); err != nil {
		fmt.Fprintf(os.Stderr, "convert JSON run: %v\n", err)
		os.Exit(1)
	}
}
