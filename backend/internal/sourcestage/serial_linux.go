//go:build linux

package sourcestage

import (
	"io"
	"os"

	"golang.org/x/sys/unix"
)

func openSerial(path string) (io.ReadWriteCloser, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (io.ReadWriteCloser, error) { _ = unix.Close(fd); return nil, err }
	term := &unix.Termios{Cflag: unix.B115200 | unix.CS8 | unix.CREAD | unix.CLOCAL, Ispeed: unix.B115200, Ospeed: unix.B115200}
	term.Cc[unix.VMIN] = 1
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, term); err != nil {
		return fail(err)
	}
	if err := unix.IoctlSetInt(fd, unix.TCFLSH, unix.TCIOFLUSH); err != nil {
		return fail(err)
	}
	// Keep O_NONBLOCK: os.File uses the runtime poller so Close interrupts Read.
	return os.NewFile(uintptr(fd), path), nil
}
