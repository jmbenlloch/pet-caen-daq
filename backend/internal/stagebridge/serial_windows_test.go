package stagebridge

import (
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// Real Windows overlapped I/O, using a private named pipe instead of COM hardware.
// A blocked reply read must not prevent outgoing keepalives on the same handle.
func TestWindowsOverlappedDuplexAndCancel(t *testing.T) {
	name, err := windows.UTF16PtrFromString(fmt.Sprintf(`\\.\pipe\pet-stage-test-%d-%d`, os.Getpid(), time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateNamedPipe(name, windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_OVERLAPPED, windows.PIPE_TYPE_BYTE|windows.PIPE_WAIT, 1, 4096, 4096, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	server := &serialPort{handle: h}
	defer server.Close()
	c, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		t.Fatal(err)
	}
	client := &serialPort{handle: c}
	defer client.Close()
	readDone := make(chan error, 1)
	go func() { _, err := server.Read(make([]byte, 32)); readDone <- err }()
	// Allow ReadFile to pend before exercising writes. The enclosing test timeout
	// is also bounded by the Windows test task.
	time.Sleep(50 * time.Millisecond)
	for i := 0; i < 12; i++ {
		done := make(chan error, 1)
		go func() { _, err := server.Write([]byte("K\n")); done <- err }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("write blocked behind pending read")
		}
		b := make([]byte, 2)
		if n, err := io.ReadFull(client, b); err != nil || n != 2 || string(b) != "K\n" {
			t.Fatalf("keepalive read: n=%d bytes=%q err=%v", n, b, err)
		}
	}
	// Fill the pipe without reading its peer so both directions are pending.
	writeDone := make(chan error, 1)
	go func() { _, err := server.Write(make([]byte, 1024*1024)); writeDone <- err }()
	time.Sleep(50 * time.Millisecond)
	closed := make(chan error, 1)
	go func() { closed <- server.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("close failed to cancel read")
	}
	select {
	case err := <-readDone:
		if err == nil {
			t.Fatal("canceled read succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("reader not joined")
	}
	select {
	case err := <-writeDone:
		if err == nil {
			t.Fatal("canceled write succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("writer not joined")
	}
	if _, err := server.Write([]byte("K\n")); err == nil {
		t.Fatal("write after Close succeeded")
	}
}
