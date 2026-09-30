package main

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/stagebridge"
)

func main() {
	const version = "20260930.2-overlapped"
	showVersion := flag.Bool("version", false, "print build version and exit")
	logPath := flag.String("log-file", "", "append structured JSON diagnostics to this file")
	debug := flag.Bool("debug", false, "log bounded serial/TCP chunks and keepalive timing")
	resetWait := flag.Duration("reset-wait", 2*time.Second, "Arduino boot wait after opening COM port")
	serial := flag.String("serial", "", "Windows COM port (required, e.g. COM3)")
	listen := flag.String("listen", "127.0.0.1:19010", "TCP listen address; non-loopback requires firewall restrictions")
	flag.Parse()
	if *showVersion {
		println(version)
		return
	}
	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	var output io.Writer = os.Stderr
	if *logPath != "" {
		f, err := os.OpenFile(*logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			slog.Error("open log file", "error", err)
			os.Exit(1)
		}
		defer f.Close()
		output = io.MultiWriter(os.Stderr, f)
	}
	log := slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{Level: level}))
	if *resetWait < 0 || *resetWait > 5*time.Second {
		log.Error("reset-wait must be between 0 and 5s")
		os.Exit(2)
	}
	if *serial == "" || flag.NArg() != 0 {
		flag.Usage()
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Error("listen", "error", err)
		os.Exit(1)
	}
	log.Warn("bridge permits motor control; no authentication or encryption; restrict access to the DAQ host", "version", version, "listen", listener.Addr(), "serial", *serial, "reset_wait", *resetWait)
	err = stagebridge.Serve(ctx, listener, func() (io.ReadWriteCloser, error) {
		port, err := stagebridge.OpenSerial(*serial)
		if err != nil {
			return nil, err
		}
		log.Info("serial opened; waiting for Arduino boot", "wait", *resetWait)
		timer := time.NewTimer(*resetWait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			_ = port.Close()
			return nil, ctx.Err()
		case <-timer.C:
			return port, nil
		}
	}, log)
	if err != nil {
		log.Error("bridge stopped", "error", err)
		os.Exit(1)
	}
}
