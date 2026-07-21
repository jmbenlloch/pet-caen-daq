package dt5202

import "testing"

func TestApplyPedestalCalibrationCorrectsAndClampsEnergy(t *testing.T) {
	calibration := PedestalCalibration{Source: "golden flash fixture"}
	calibration.LowGain[0] = 70
	calibration.HighGain[0] = 20
	calibration.LowGain[1] = 100
	calibration.HighGain[1] = 0
	event := SpectroscopyEvent{Energies: []Energy{
		{Channel: 0, LowGain: 10, HighGain: MaxEnergy - 10, HasLowGain: true, HasHighGain: true},
		{Channel: 1, LowGain: 20, HasLowGain: true},
	}}
	got := ApplyPedestalCalibration(event, 50, calibration)
	if got.Energies[0].LowGain != 0 {
		t.Errorf("low clamp = %d, want 0", got.Energies[0].LowGain)
	}
	if got.Energies[0].HighGain != MaxEnergy {
		t.Errorf("high clamp = %d, want %d", got.Energies[0].HighGain, MaxEnergy)
	}
	if got.Energies[1].LowGain != 0 {
		t.Errorf("corrected low = %d, want 0", got.Energies[1].LowGain)
	}
	if event.Energies[0].LowGain != 10 || event.Energies[0].HighGain != MaxEnergy-10 {
		t.Fatalf("input event was mutated: %#v", event.Energies[0])
	}
}

func TestWithPedestalCalibrationCompletesSpectroscopyZeroSuppression(t *testing.T) {
	plan := ConfigurationPlan{Board: 0, Deferred: []string{"Pedestal"}, Pedestal: PedestalPlan{Common: 50, AcquisitionMode: 1, ZeroSuppressLowGain: 50, ZeroSuppressHighGain: 60}}
	calibration := PedestalCalibration{Source: "source-confirmed fixture"}
	for channel := range ChannelCount {
		calibration.LowGain[channel] = uint16(100 + channel)
		calibration.HighGain[channel] = uint16(200 + channel)
	}
	got, err := plan.WithPedestalCalibration(calibration)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Deferred) != 0 {
		t.Fatalf("deferred = %#v", got.Deferred)
	}
	if len(got.Writes) != 2*ChannelCount {
		t.Fatalf("writes = %d, want %d", len(got.Writes), 2*ChannelCount)
	}
	if got.Writes[0] != (RegisterWrite{ZeroSuppressionLowGain, 100}) || got.Writes[1] != (RegisterWrite{ZeroSuppressionHighGain, 210}) {
		t.Fatalf("channel 0 writes = %#v", got.Writes[:2])
	}
}

func TestWithPedestalCalibrationDoesNotWriteZeroSuppressionForTimingSpectroscopy(t *testing.T) {
	plan := ConfigurationPlan{Deferred: []string{"Pedestal"}, Pedestal: PedestalPlan{Common: 50, AcquisitionMode: 3, ZeroSuppressLowGain: 50, ZeroSuppressHighGain: 50}}
	got, err := plan.WithPedestalCalibration(PedestalCalibration{Source: "flash page 4"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Writes) != 0 || len(got.Deferred) != 0 {
		t.Fatalf("completed plan = %#v", got)
	}
}

func TestWithPedestalCalibrationRequiresEvidenceSource(t *testing.T) {
	_, err := (ConfigurationPlan{}).WithPedestalCalibration(PedestalCalibration{})
	if err == nil {
		t.Fatal("expected missing calibration source error")
	}
}

func TestZeroSuppressionMatchesSourceUint16Wrapping(t *testing.T) {
	if got, want := zeroSuppressionValue(1, 100, 0), uint16(0xff9d); got != want {
		t.Fatalf("wrapped threshold = %#x, want %#x", got, want)
	}
	if got := zeroSuppressionValue(0, 100, 200); got != 0 {
		t.Fatalf("disabled threshold = %d", got)
	}
}
