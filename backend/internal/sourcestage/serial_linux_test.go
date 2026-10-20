package sourcestage

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestLinuxSerialPTY(t *testing.T) {
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	master := os.NewFile(uintptr(fd), "pty-master")
	defer master.Close()
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	number, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stage, err := Open(ctx, fmt.Sprintf("/dev/pts/%d", number))
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Close()
	go func() {
		line, _ := bufio.NewReader(master).ReadString('\n')
		if line == "HELLO\n" {
			_, _ = master.Write([]byte("PETXY 1\r\n"))
		}
	}()
	if err := stage.Exchange(ctx, "HELLO", "PETXY 1"); err != nil {
		t.Fatal(err)
	}
}
