package scanrecord

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const JournalName = "evidence.jsonl"

// Append durably records evidence before another point can overwrite hardware
// state. There is one owner per scan; no unbounded telemetry queue is used.
func Append(dir string, record Record) (err error) {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(dir, JournalName), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	defer func() {
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
	}()
	if _, err = file.Write(append(data, '\n')); err != nil {
		return err
	}
	return file.Sync()
}

func Read(dir string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(dir, JournalName))
	if os.IsNotExist(err) {
		return nil, nil
	} // Older scans have no evidence journal.
	return data, err
}
