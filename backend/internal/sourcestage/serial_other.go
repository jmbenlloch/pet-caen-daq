//go:build !linux

package sourcestage

import (
	"fmt"
	"io"
)

func openSerial(string) (io.ReadWriteCloser, error) {
	return nil, fmt.Errorf("native source-stage serial transport currently requires Linux; tcp:// is available for the simulator")
}
