package dt5202

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type initializationIO struct {
	calls   []string
	control uint32
	failAt  int
	reads   int
	invalid int
}

func (h *initializationIO) record(ctx context.Context, s string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	h.calls = append(h.calls, s)
	if len(h.calls) == h.failAt {
		return errors.New("injected")
	}
	return nil
}
func (h *initializationIO) WriteRegister(ctx context.Context, _, _ uint16, a, v uint32) error {
	if err := h.record(ctx, fmt.Sprintf("W %08x %08x", a, v)); err != nil {
		return err
	}
	if a == uint32(CitirocSlowControl) {
		h.control = v
	}
	return nil
}
func (h *initializationIO) SendCommand(ctx context.Context, _, _ uint16, c, d uint32) error {
	return h.record(ctx, fmt.Sprintf("C %02x %d", c, d))
}
func (h *initializationIO) ReadRegister(ctx context.Context, _, _ uint16, a uint32) (uint32, error) {
	if err := h.record(ctx, fmt.Sprintf("R %08x", a)); err != nil {
		return 0, err
	}
	if a == uint32(CitirocSlowControl) {
		return h.control, nil
	}
	h.reads++
	if h.invalid > 0 {
		h.invalid--
		return 0xdeadbeef, nil
	}
	return 0, nil
}

// Vendor FERS_configure_5202.c automatic path and ReadSCbsFromChip;
// capture frames 3878/3884/4034/4040 confirm two commands per chip.
func TestInitializationUsesDelayedLoadAndReadbackPerChip(t *testing.T) {
	h := &initializationIO{}
	if err := ApplyConfiguration(context.Background(), h, 0, 0, ConfigurationPlan{}, true); err != nil {
		t.Fatal(err)
	}
	want := []string{"C 15 1000000"}
	for chip := uint32(0); chip < 2; chip++ {
		sel := chip << 9
		want = append(want, fmt.Sprintf("W 01000130 %08x", sel), "C 20 1000000", "R 01000130", fmt.Sprintf("W 01000130 %08x", sel), "C 20 1000000")
		for word := uint32(0); word < 36; word++ {
			want = append(want, fmt.Sprintf("W 01000130 %08x", sel|word), "R 01000134")
		}
		want = append(want, fmt.Sprintf("W 01000130 %08x", sel))
	}
	if strings.Join(h.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("sequence differs\ngot:\n%s\nwant:\n%s", strings.Join(h.calls, "\n"), strings.Join(want, "\n"))
	}
}
func TestInitializationStopsOnEveryIOFailure(t *testing.T) {
	good := &initializationIO{}
	if err := ApplyConfiguration(context.Background(), good, 0, 0, ConfigurationPlan{}, true); err != nil {
		t.Fatal(err)
	}
	for fail := 1; fail <= len(good.calls); fail++ {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			h := &initializationIO{failAt: fail}
			err := ApplyConfiguration(context.Background(), h, 0, 0, ConfigurationPlan{}, true)
			if err == nil || !strings.Contains(err.Error(), "injected") {
				t.Fatalf("failure %d: %v", fail, err)
			}
			// Only deferred selector restoration is allowed after an I/O failure.
			if len(h.calls) > fail+1 {
				t.Fatalf("continued after failure: %v", h.calls[fail:])
			}
		})
	}
}
func TestInitializationHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h := &initializationIO{}
	if err := ApplyConfiguration(ctx, h, 0, 0, ConfigurationPlan{}, true); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(h.calls) != 0 {
		t.Fatal(h.calls)
	}
}
func TestASICReadbackRejectsInvalidWordWithoutReload(t *testing.T) {
	h := &initializationIO{invalid: 1}
	_, err := ReadCitirocStream(context.Background(), h, 0, 0, 0)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "deadbeef") {
		t.Fatalf("invalid readback accepted: %v", err)
	}
	if h.reads != 1 {
		t.Fatalf("unexpected data reads: %d", h.reads)
	}
	if h.control != 0 {
		t.Fatalf("selector not restored: %x", h.control)
	}
}
