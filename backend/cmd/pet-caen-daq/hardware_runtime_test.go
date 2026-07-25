package main

import (
	"testing"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
)

func TestSameConfigurationIgnoresSourceLineNumbers(t *testing.T) {
	board, channel := 1, 2
	left := &janusconfig.Document{Assignments: []janusconfig.Assignment{
		{Name: "Open", Index: &board, Value: "usb:172.16.0.11:tdl:1:0", Line: 1},
		{Name: "QD_FineThreshold", Index: &board, Channel: &channel, Value: "17", Line: 2},
	}}
	right := cloneDocument(left)
	right.Assignments[0].Line = 20
	right.Assignments[1].Line = 21
	if !sameConfiguration(left, right) {
		t.Fatal("equivalent parsed configurations were not reused")
	}
	right.Assignments[1].Value = "18"
	if sameConfiguration(left, right) {
		t.Fatal("changed configuration was reused")
	}
}

func TestCloneDocumentDoesNotAliasIndexes(t *testing.T) {
	board := 1
	original := &janusconfig.Document{Assignments: []janusconfig.Assignment{{Name: "Open", Index: &board, Value: "usb:172.16.0.11:tdl:1:0"}}}
	clone := cloneDocument(original)
	*clone.Assignments[0].Index = 3
	if *original.Assignments[0].Index != 1 {
		t.Fatal("clone aliases original index")
	}
}

func TestTopologySynchronizedRequiresEveryBoard(t *testing.T) {
	topology := dt5215.Topology{Boards: []dt5215.BoardInfo{
		{AcquisitionState: uint32(dt5202.StatusReady | dt5202.StatusTDLinkSynchronized)},
		{AcquisitionState: uint32(dt5202.StatusReady | dt5202.StatusTDLinkSynchronized)},
	}}
	if !topologySynchronized(topology) {
		t.Fatal("fully synchronized topology was rejected")
	}
	topology.Boards[1].AcquisitionState = uint32(dt5202.StatusReady)
	if topologySynchronized(topology) {
		t.Fatal("partially synchronized topology was accepted")
	}
	if topologySynchronized(dt5215.Topology{}) {
		t.Fatal("empty topology was accepted")
	}
}
