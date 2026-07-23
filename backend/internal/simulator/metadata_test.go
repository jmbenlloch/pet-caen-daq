package simulator

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
)

func TestConcentratorMetadataSupportsFERSlib(t *testing.T) {
	server, err := Start("127.0.0.1:0", "127.0.0.1:0", ProductionTopology())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Errorf("close simulator: %v", err)
		}
	})

	connection, err := net.Dial("tcp", server.ControlAddress())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	for _, test := range []struct {
		opcode  string
		request []byte
		check   func([]byte) bool
	}{
		{"VERS", nil, func(payload []byte) bool {
			return bytes.HasPrefix(payload[0:16], []byte("2026.4.1.1")) &&
				bytes.HasPrefix(payload[16:48], []byte("25.11.24.01-2-2")) &&
				bytes.HasPrefix(payload[48:64], []byte("66643"))
		}},
		{"RBIC", nil, func(payload []byte) bool {
			return string(payload) == "0;DT5215;WDT5215XAAAA;0;1.0;8;0;0;02:00:00:00:00:01\x00"
		}},
	} {
		t.Run(test.opcode, func(t *testing.T) {
			if _, err := connection.Write(append([]byte(test.opcode), test.request...)); err != nil {
				t.Fatal(err)
			}
			header := make([]byte, 4)
			if _, err := io.ReadFull(connection, header); err != nil {
				t.Fatal(err)
			}
			payload := make([]byte, binary.LittleEndian.Uint32(header))
			if _, err := io.ReadFull(connection, payload); err != nil {
				t.Fatal(err)
			}
			if !test.check(payload) {
				t.Fatalf("unexpected %s payload %q", test.opcode, payload)
			}
		})
	}

	if _, err := connection.Write([]byte("CRRG\x0f\x00\x00\x00")); err != nil {
		t.Fatal(err)
	}
	registerReply := make([]byte, 8)
	if _, err := io.ReadFull(connection, registerReply); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(registerReply, make([]byte, 8)) {
		t.Fatalf("unexpected CRRG reply %x", registerReply)
	}

	if _, err := connection.Write([]byte("CWRG\x0f\x00\x00\x00\x01\x00\x00\x00")); err != nil {
		t.Fatal(err)
	}
	status := make([]byte, 4)
	if _, err := io.ReadFull(connection, status); err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(status) != 0 {
		t.Fatalf("unexpected CWRG status %x", status)
	}
}
