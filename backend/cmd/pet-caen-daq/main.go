package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	configurationPath := flag.String("config", "", "path to a JANUS configuration")
	controlAddress := flag.String("control", "172.16.0.11:9760", "DT5215 control address")
	streamAddress := flag.String("stream", "172.16.0.11:9000", "DT5215 stream address")
	flag.Parse()
	if *configurationPath == "" {
		return fmt.Errorf("-config is required")
	}

	configuration, err := os.Open(*configurationPath)
	if err != nil {
		return fmt.Errorf("open configuration: %w", err)
	}
	defer configuration.Close()
	document, err := janusconfig.Parse(configuration)
	if err != nil {
		return err
	}
	connections, err := document.Connections()
	if err != nil {
		return err
	}
	if err := janusconfig.ValidateProductionTopology(connections); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := dt5215.Dial(ctx, *controlAddress, *streamAddress)
	if err != nil {
		return err
	}
	defer client.Close()
	topology, err := client.DiscoverProductionTopology(ctx, connections)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(topology)
}
