package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/simulator"
)

func main() {
	controlAddress := flag.String("control", "127.0.0.1:9760", "control listen address")
	streamAddress := flag.String("stream", "127.0.0.1:9000", "stream listen address")
	flag.Parse()

	server, err := simulator.Start(*controlAddress, *streamAddress, simulator.ProductionTopology())
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Printf("CAEN simulator control=%s stream=%s\n", server.ControlAddress(), server.StreamAddress())

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	if err := server.Close(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
