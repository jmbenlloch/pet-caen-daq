package stagebridge

import (
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// OpenSerial opens an exclusive Windows COM port at 115200 8N1, no flow control.
// DTR is asserted; opening the port may reset an Arduino. The bridge must wait
// for boot before forwarding the first command (including for TCP backends).
func OpenSerial(name string) (io.ReadWriteCloser, error) {
	name = strings.ToUpper(name)
	if !regexp.MustCompile(`^COM[1-9][0-9]*$`).MatchString(name) {
		return nil, fmt.Errorf("expected COM port name such as COM3, got %q", name)
	}
	path, err := windows.UTF16PtrFromString(`\\.\` + name)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(path, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (io.ReadWriteCloser, error) { _ = windows.CloseHandle(h); return nil, err }
	var dcb windows.DCB
	dcb.DCBlength = uint32(unsafe.Sizeof(dcb))
	if err := windows.GetCommState(h, &dcb); err != nil {
		return fail(err)
	}
	dcb.BaudRate, dcb.ByteSize, dcb.Parity, dcb.StopBits = 115200, 8, 0, 0
	// fBinary | DTR_CONTROL_ENABLE | RTS_CONTROL_ENABLE; all flow control off.
	dcb.Flags = 1 | (1 << 4) | (1 << 12)
	if err := windows.SetCommState(h, &dcb); err != nil {
		return fail(err)
	}
	// Independent overlapped reads/writes prevent a pending read from delaying
	// motion keepalives. Timeout reads are not mistaken for EOF.
	timeouts := windows.CommTimeouts{ReadIntervalTimeout: ^uint32(0), ReadTotalTimeoutMultiplier: ^uint32(0), ReadTotalTimeoutConstant: 100, WriteTotalTimeoutConstant: 500}
	if err := windows.SetCommTimeouts(h, &timeouts); err != nil {
		return fail(err)
	}
	if err := windows.PurgeComm(h, windows.PURGE_RXCLEAR|windows.PURGE_TXCLEAR); err != nil {
		return fail(err)
	}
	return &serialPort{handle: h}, nil
}

type serialPort struct {
	handle          windows.Handle
	closed          bool
	lifecycle       sync.Mutex
	pending         sync.WaitGroup
	readMu, writeMu sync.Mutex
	once            sync.Once
	closeErr        error
}

func (p *serialPort) Read(b []byte) (int, error) {
	p.readMu.Lock()
	defer p.readMu.Unlock()
	for {
		n, err := p.transfer(b, false)
		if n > 0 || err != nil || len(b) == 0 {
			return n, err
		}
	}
}

func (p *serialPort) Write(b []byte) (int, error) {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	n, err := p.transfer(b, true)
	if err == nil && n != len(b) {
		err = io.ErrShortWrite
	}
	return n, err
}

// Issue under lifecycle so Close cannot cancel before a new operation starts.
// Keep each buffer, event and OVERLAPPED alive until completion, even on cancel.
func (p *serialPort) transfer(b []byte, write bool) (int, error) {
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(event)
	o := windows.Overlapped{HEvent: event}
	var n uint32
	p.lifecycle.Lock()
	if p.closed {
		p.lifecycle.Unlock()
		return 0, os.ErrClosed
	}
	if len(b) == 0 {
		p.lifecycle.Unlock()
		return 0, nil
	}
	p.pending.Add(1)
	if write {
		err = windows.WriteFile(p.handle, b, &n, &o)
	} else {
		err = windows.ReadFile(p.handle, b, &n, &o)
	}
	p.lifecycle.Unlock()
	defer p.pending.Done()
	if errors.Is(err, windows.ERROR_IO_PENDING) {
		err = windows.GetOverlappedResult(p.handle, &o, &n, true)
	}
	runtime.KeepAlive(b)
	return int(n), err
}

func (p *serialPort) Close() error {
	p.once.Do(func() {
		p.lifecycle.Lock()
		p.closed = true
		err := windows.CancelIoEx(p.handle, nil)
		if errors.Is(err, windows.ERROR_NOT_FOUND) {
			err = nil
		}
		p.lifecycle.Unlock()
		p.pending.Wait()
		p.closeErr = errors.Join(err, windows.CloseHandle(p.handle))
	})
	return p.closeErr
}
