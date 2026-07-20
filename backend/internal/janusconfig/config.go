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
	Value string
	Line  int
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

		name, index, err := parseKey(key)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNumber, err)
		}
		doc.Assignments = append(doc.Assignments, Assignment{
			Name:  name,
			Index: index,
			Value: value,
			Line:  lineNumber,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read JANUS configuration: %w", err)
	}
	return doc, nil
}

func parseKey(key string) (string, *int, error) {
	open := strings.IndexByte(key, '[')
	if open < 0 {
		if key == "" {
			return "", nil, fmt.Errorf("empty parameter name")
		}
		return key, nil, nil
	}
	if !strings.HasSuffix(key, "]") || open == 0 {
		return "", nil, fmt.Errorf("invalid indexed parameter %q", key)
	}
	value, err := strconv.Atoi(key[open+1 : len(key)-1])
	if err != nil || value < 0 {
		return "", nil, fmt.Errorf("invalid index in parameter %q", key)
	}
	return key[:open], &value, nil
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
	if err != nil || chain < 0 || chain > 7 {
		return Connection{}, fmt.Errorf("invalid TDlink chain %q", parts[3])
	}
	node, err := strconv.Atoi(parts[4])
	if err != nil || node < 0 || node > 15 {
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

// ValidateProductionTopology checks the version-one topology contract.
func ValidateProductionTopology(connections []Connection) error {
	if len(connections) != 4 {
		return fmt.Errorf("expected 4 board connections, got %d", len(connections))
	}
	for expected := 0; expected < 4; expected++ {
		found := false
		for _, connection := range connections {
			if connection.Board == expected && connection.Chain == expected && connection.Node == 0 {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("expected board %d on TDlink %d node 0", expected, expected)
		}
	}
	return nil
}
