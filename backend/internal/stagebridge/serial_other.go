//go:build !windows

package stagebridge

import (
	"fmt"
	"io"
)

func OpenSerial(name string) (io.ReadWriteCloser, error) {
	return nil, fmt.Errorf("the serial bridge requires Windows (requested %q)", name)
}
