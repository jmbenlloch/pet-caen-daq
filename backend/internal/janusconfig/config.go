// Package janusconfig parses JANUS text configuration files without applying
// hardware-specific defaults. It preserves every assignment so unsupported
// settings cannot disappear silently.
package janusconfig

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
)

// Assignment is one configuration assignment from a JANUS file.
type Assignment struct {
	Name  string
	Index *int
	// Channel is present only for JANUS Parameter[board][channel]
	// assignments. Index remains the board index for compatibility.
	Channel *int
	Value   string
	Line    int
}

// Document is an ordered JANUS configuration document.
type Document struct {
	Assignments []Assignment
}

// Connection identifies one board through a DT5215 TDlink.
type Connection struct {
	Board     int
	Interface string
	Host      string
	Chain     int
	Node      int
}

const (
	MaxChains        = 8
	MaxNodesPerChain = 16
	MaxBoards        = MaxChains * MaxNodesPerChain
)

// Parse reads a JANUS configuration. Empty lines and full-line comments are
// ignored; inline comments begin with '#'.
func Parse(r io.Reader) (*Document, error) {
	scanner := bufio.NewScanner(r)
	// Production files contain long option comments. Keep a defensive bound but
	// allow substantially more than bufio.Scanner's default token size.
	scanner.Buffer(make([]byte, 4096), 1024*1024)

	doc := &Document{}
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(strings.TrimSuffix(scanner.Text(), "\r"))
		if comment := strings.IndexByte(line, '#'); comment >= 0 {
			line = strings.TrimSpace(line[:comment])
		}
		if line == "" {
			continue
		}

		keyEnd := strings.IndexAny(line, " \t")
		if keyEnd < 0 {
			return nil, fmt.Errorf("line %d: assignment %q has no value", lineNumber, line)
		}
		key := line[:keyEnd]
		value := strings.TrimSpace(line[keyEnd:])
		if value == "" {
			return nil, fmt.Errorf("line %d: assignment %q has no value", lineNumber, key)
		}

		name, index, channel, err := parseKey(key)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNumber, err)
		}
		doc.Assignments = append(doc.Assignments, Assignment{
			Name:    name,
			Index:   index,
			Channel: channel,
			Value:   value,
			Line:    lineNumber,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read JANUS configuration: %w", err)
	}
	return doc, nil
}

func parseKey(key string) (string, *int, *int, error) {
	open := strings.IndexByte(key, '[')
	if open < 0 {
		if key == "" {
			return "", nil, nil, fmt.Errorf("empty parameter name")
		}
		return key, nil, nil, nil
	}
	if open == 0 {
		return "", nil, nil, fmt.Errorf("invalid indexed parameter %q", key)
	}
	name := key[:open]
	rest := key[open:]
	indexes := make([]int, 0, 2)
	for rest != "" {
		if rest[0] != '[' {
			return "", nil, nil, fmt.Errorf("invalid indexed parameter %q", key)
		}
		close := strings.IndexByte(rest, ']')
		if close < 2 {
			return "", nil, nil, fmt.Errorf("invalid indexed parameter %q", key)
		}
		value, err := strconv.Atoi(rest[1:close])
		if err != nil || value < 0 || len(indexes) == 2 {
			return "", nil, nil, fmt.Errorf("invalid index in parameter %q", key)
		}
		indexes = append(indexes, value)
		rest = rest[close+1:]
	}
	index := indexes[0]
	if len(indexes) == 1 {
		return name, &index, nil, nil
	}
	channel := indexes[1]
	return name, &index, &channel, nil
}

// Connections parses every indexed Open assignment. Direct board connections
// are intentionally rejected in this first slice.
func (d *Document) Connections() ([]Connection, error) {
	var connections []Connection
	seen := make(map[int]struct{})
	for _, assignment := range d.Assignments {
		if assignment.Name != "Open" {
			continue
		}
		if assignment.Channel != nil {
			return nil, fmt.Errorf("line %d: Open accepts only a board index", assignment.Line)
		}
		if assignment.Index == nil {
			return nil, fmt.Errorf("line %d: Open must have a board index", assignment.Line)
		}
		if _, ok := seen[*assignment.Index]; ok {
			return nil, fmt.Errorf("line %d: duplicate Open[%d]", assignment.Line, *assignment.Index)
		}
		connection, err := parseConnection(*assignment.Index, assignment.Value)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", assignment.Line, err)
		}
		seen[*assignment.Index] = struct{}{}
		connections = append(connections, connection)
	}
	if len(connections) == 0 {
		return nil, fmt.Errorf("configuration has no Open assignments")
	}
	return connections, nil
}

func parseConnection(board int, value string) (Connection, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 5 || parts[2] != "tdl" {
		return Connection{}, fmt.Errorf("unsupported connection path %q", value)
	}
	if parts[0] != "usb" && parts[0] != "eth" {
		return Connection{}, fmt.Errorf("unsupported concentrator interface %q", parts[0])
	}
	if net.ParseIP(parts[1]) == nil {
		return Connection{}, fmt.Errorf("invalid concentrator IP address %q", parts[1])
	}
	chain, err := strconv.Atoi(parts[3])
	if err != nil || chain < 0 || chain >= MaxChains {
		return Connection{}, fmt.Errorf("invalid TDlink chain %q", parts[3])
	}
	node, err := strconv.Atoi(parts[4])
	if err != nil || node < 0 || node >= MaxNodesPerChain {
		return Connection{}, fmt.Errorf("invalid TDlink node %q", parts[4])
	}
	return Connection{
		Board:     board,
		Interface: parts[0],
		Host:      parts[1],
		Chain:     chain,
		Node:      node,
	}, nil
}

// ValidateProductionTopology checks that logical board numbers and physical
// TDlink addresses form a complete, unambiguous topology supported by one
// DT5215 concentrator.
func ValidateProductionTopology(connections []Connection) error {
	if len(connections) < 1 || len(connections) > MaxBoards {
		return fmt.Errorf("expected between 1 and %d board connections, got %d", MaxBoards, len(connections))
	}
	boards := make(map[int]struct{}, len(connections))
	addresses := make(map[[2]int]int, len(connections))
	nodesByChain := make(map[int]map[int]struct{}, MaxChains)
	host, iface := connections[0].Host, connections[0].Interface
	for _, connection := range connections {
		if connection.Board < 0 || connection.Board >= len(connections) {
			return fmt.Errorf("board index %d is outside contiguous range 0-%d", connection.Board, len(connections)-1)
		}
		if _, exists := boards[connection.Board]; exists {
			return fmt.Errorf("duplicate board index %d", connection.Board)
		}
		boards[connection.Board] = struct{}{}
		address := [2]int{connection.Chain, connection.Node}
		if board, exists := addresses[address]; exists {
			return fmt.Errorf("boards %d and %d both use TDlink %d node %d", board, connection.Board, connection.Chain, connection.Node)
		}
		addresses[address] = connection.Board
		if nodesByChain[connection.Chain] == nil {
			nodesByChain[connection.Chain] = make(map[int]struct{})
		}
		nodesByChain[connection.Chain][connection.Node] = struct{}{}
		if connection.Host != host || connection.Interface != iface {
			return fmt.Errorf("board %d uses concentrator %s:%s; expected %s:%s", connection.Board, connection.Interface, connection.Host, iface, host)
		}
	}
	for chain, nodes := range nodesByChain {
		for node := 0; node < len(nodes); node++ {
			if _, exists := nodes[node]; !exists {
				return fmt.Errorf("TDlink %d node indices must be contiguous from 0; node %d is missing", chain, node)
			}
		}
	}
	return nil
}
