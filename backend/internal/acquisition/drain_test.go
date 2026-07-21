package acquisition

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
)

type drainRead struct {
	raw    []byte
	events []dt5215.StreamEvent
	err    error
}
type scriptedDrainHardware struct {
	reads   []drainRead
	stopErr error
	stops   int
	stall   bool
}

func (h *scriptedDrainHardware) SendCommand(context.Context, uint16, uint16, uint32, uint32) error {
	h.stops++
	return h.stopErr
}
func (h *scriptedDrainHardware) ReadRawStreamBatch(ctx context.Context) ([]byte, []dt5215.StreamEvent, error) {
	if h.stall {
		<-ctx.Done()
		return nil, nil, ctx.Err()
	}
	if len(h.reads) == 0 {
		return nil, nil, io.EOF
	}
	next := h.reads[0]
	h.reads = h.reads[1:]
	return next.raw, next.events, next.err
}

func completion(chain uint8, ready bool) dt5215.StreamEvent {
	status := uint32(dt5202.StatusRunning)
	if ready {
		status = uint32(dt5202.StatusReady)
	}
	payload := make([]byte, 20)
	binary.LittleEndian.PutUint32(payload, uint32(1)<<24|1<<12|2048)
	binary.LittleEndian.PutUint32(payload[16:], status|100<<16)
	return dt5215.StreamEvent{Chain: chain, Descriptor: dt5215.Descriptor{Qualifier: dt5202.QualifierService}, Payload: payload}
}

func TestStopAndDrainDeliversPendingEventsBeforeCompletion(t *testing.T) {
	pending := dt5215.StreamEvent{Chain: 0, Descriptor: dt5215.Descriptor{Qualifier: dt5202.QualifierSpectroscopy}}
	hardware := &scriptedDrainHardware{reads: []drainRead{{raw: []byte("pending"), events: []dt5215.StreamEvent{pending}}, {raw: []byte("complete0"), events: []dt5215.StreamEvent{completion(0, true)}}, {raw: []byte("running"), events: []dt5215.StreamEvent{completion(1, false)}}, {raw: []byte("complete1"), events: []dt5215.StreamEvent{completion(1, true)}}}}
	var delivered []string
	result, err := StopAndDrain(context.Background(), hardware, 2, func(raw []byte, _ []dt5215.StreamEvent) error { delivered = append(delivered, string(raw)); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if hardware.stops != 1 || result.Batches != 4 || result.Events != 4 || result.CompletedChains != 2 {
		t.Fatalf("result=%#v stops=%d", result, hardware.stops)
	}
	want := []string{"pending", "complete0", "running", "complete1"}
	for i := range want {
		if delivered[i] != want[i] {
			t.Fatalf("delivered=%v", delivered)
		}
	}
}

func TestStopAndDrainMissingCompletion(t *testing.T) {
	hardware := &scriptedDrainHardware{reads: []drainRead{{events: []dt5215.StreamEvent{completion(0, true)}}}}
	_, err := StopAndDrain(context.Background(), hardware, 2, nil)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("error=%v", err)
	}
}

func TestStopAndDrainCompletesAfterCaptureVerifiedIdlePeriod(t *testing.T) {
	result, err := StopAndDrain(context.Background(), &scriptedDrainHardware{stall: true}, 4, nil)
	if err != nil || result.CompletedChains != 4 || result.Batches != 0 {
		t.Fatalf("result=%#v error=%v", result, err)
	}
}

func TestStopAndDrainTimeoutAndCancellation(t *testing.T) {
	for _, test := range []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
		want error
	}{
		{"timeout", func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 10*time.Millisecond)
		}, context.DeadlineExceeded},
		{"cancel", func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx, func() {}
		}, context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := test.ctx()
			defer cancel()
			_, err := StopAndDrain(ctx, &scriptedDrainHardware{stall: true}, 1, nil)
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestStopAndDrainPreservesDeliveryAndStopErrors(t *testing.T) {
	delivery := errors.New("storage failed")
	_, err := StopAndDrain(context.Background(), &scriptedDrainHardware{reads: []drainRead{{events: []dt5215.StreamEvent{completion(0, true)}}}}, 1, func([]byte, []dt5215.StreamEvent) error { return delivery })
	if !errors.Is(err, delivery) {
		t.Fatalf("delivery error=%v", err)
	}
	stop := errors.New("stop failed")
	_, err = StopAndDrain(context.Background(), &scriptedDrainHardware{stopErr: stop}, 1, nil)
	if !errors.Is(err, stop) {
		t.Fatalf("stop error=%v", err)
	}
	primary := errors.New("acquisition failed")
	joined := JoinStopError(primary, stop)
	if !errors.Is(joined, primary) || !errors.Is(joined, stop) {
		t.Fatalf("joined=%v", joined)
	}
}
