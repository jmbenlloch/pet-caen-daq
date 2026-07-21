package dt5202

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
)

const ChannelCount = 64

type RegisterWrite struct {
	Address Register
	Value   uint32
}

// ConfigurationPlan is the requested configuration translated into effective
// FPGA writes. Deferred names are explicit hardware settings handled by later
// Citiroc, probe, calibration, or HV-peripheral stages.
type ConfigurationPlan struct {
	Board    int
	Writes   []RegisterWrite
	Citiroc  [2]CitirocChip
	HV       HVPlan
	Deferred []string
	Inactive []InactiveSetting
}

type InactiveSetting struct {
	Name   string
	Reason string
}

// ValidateReadback compares the final requested value at every written
// address with a hardware or simulator register snapshot.
func (p ConfigurationPlan) ValidateReadback(actual map[Register]uint32) error {
	expected := make(map[Register]uint32, len(p.Writes))
	for _, write := range p.Writes {
		expected[write.Address] = write.Value
	}
	for address, want := range expected {
		got, ok := actual[address]
		if !ok {
			return fmt.Errorf("board %d: register %#08x missing from readback", p.Board, address)
		}
		if got != want {
			return fmt.Errorf("board %d: register %#08x effective value %#08x, requested %#08x", p.Board, address, got, want)
		}
	}
	return nil
}

// PlanProductionConfiguration translates the hardware-owned settings used by
// the committed production configuration. Global assignments apply to every
// board and later indexed assignments override only their selected board.
func PlanProductionConfiguration(doc *janusconfig.Document, board int) (ConfigurationPlan, error) {
	if board < 0 {
		return ConfigurationPlan{}, fmt.Errorf("invalid board index %d", board)
	}
	values := make(map[string]janusconfig.Assignment)
	for _, a := range doc.Assignments {
		if a.Index == nil || *a.Index == board {
			values[a.Name] = a
		}
	}
	require := func(name string) (janusconfig.Assignment, error) {
		a, ok := values[name]
		if !ok {
			return janusconfig.Assignment{}, fmt.Errorf("board %d: missing required setting %q", board, name)
		}
		return a, nil
	}
	u32 := func(name string, bits int) (uint32, error) {
		a, err := require(name)
		if err != nil {
			return 0, err
		}
		v, err := strconv.ParseUint(strings.TrimSpace(a.Value), 0, bits)
		if err != nil {
			return 0, fmt.Errorf("line %d: %s: %w", a.Line, name, err)
		}
		return uint32(v), nil
	}
	choice := func(name string, options map[string]uint32) (uint32, error) {
		a, err := require(name)
		if err != nil {
			return 0, err
		}
		v, ok := options[a.Value]
		if !ok {
			return 0, fmt.Errorf("line %d: %s has unsupported value %q", a.Line, name, a.Value)
		}
		return v, nil
	}
	timeNS := func(name string) (float64, error) {
		a, err := require(name)
		if err != nil {
			return 0, err
		}
		fields := strings.Fields(a.Value)
		if len(fields) == 0 || len(fields) > 2 {
			return 0, fmt.Errorf("line %d: invalid time %q", a.Line, a.Value)
		}
		v, err := strconv.ParseFloat(fields[0], 64)
		if err != nil {
			return 0, fmt.Errorf("line %d: invalid time %q", a.Line, a.Value)
		}
		unit := "ns"
		if len(fields) == 2 {
			unit = fields[1]
		}
		switch unit {
		case "ns":
		case "us":
			v *= 1e3
		case "ms":
			v *= 1e6
		case "s":
			v *= 1e9
		default:
			return 0, fmt.Errorf("line %d: unsupported time unit %q", a.Line, unit)
		}
		return v, nil
	}
	decimalUnit := func(name string, units map[string]float64) (float64, error) {
		a, err := require(name)
		if err != nil {
			return 0, err
		}
		fields := strings.Fields(a.Value)
		if len(fields) < 1 || len(fields) > 2 {
			return 0, fmt.Errorf("line %d: invalid %s value %q", a.Line, name, a.Value)
		}
		value, err := strconv.ParseFloat(fields[0], 64)
		if err != nil {
			return 0, fmt.Errorf("line %d: invalid %s value %q", a.Line, name, a.Value)
		}
		if len(fields) == 2 {
			multiplier, ok := units[fields[1]]
			if !ok {
				return 0, fmt.Errorf("line %d: unsupported %s unit %q", a.Line, name, fields[1])
			}
			value *= multiplier
		}
		return value, nil
	}

	acqMode, err := choice("AcquisitionMode", map[string]uint32{"SPECTROSCOPY": 1, "TIMING_CSTART": 2, "SPECT_TIMING": 3, "COUNTING": 4, "WAVEFORM": 8, "TIMING_CSTOP": 0x12})
	if err != nil {
		return ConfigurationPlan{}, err
	}
	tot, err := u32("EnableToT", 2)
	if err != nil {
		return ConfigurationPlan{}, err
	}
	validationMode, err := choice("ValidationMode", map[string]uint32{"DISABLED": 0, "ACCEPT": 1, "REJECT": 2})
	if err != nil {
		return ConfigurationPlan{}, err
	}
	countingMode, err := choice("CountingMode", map[string]uint32{"SINGLES": 0, "PAIRED_AND": 1})
	if err != nil {
		return ConfigurationPlan{}, err
	}
	if acqMode != 4 {
		countingMode = 0
	}
	trgID, err := choice("TrgIdMode", map[string]uint32{"TRIGGER_CNT": 0, "VALIDATION_CNT": 1})
	if err != nil {
		return ConfigurationPlan{}, err
	}
	gainSelect, err := choice("GainSelect", map[string]uint32{"AUTO": 0, "HIGH": 1, "LOW": 2, "BOTH": 3})
	if err != nil {
		return ConfigurationPlan{}, err
	}
	cntZS, err := u32("EnableCntZeroSuppr", 1)
	if err != nil {
		return ConfigurationPlan{}, err
	}
	acqControl := acqMode | tot<<8 | gainSelect<<12 | validationMode<<24 | trgID<<26 | countingMode<<27 | cntZS<<30

	mask0, err := u32("ChEnableMask0", 32)
	if err != nil {
		return ConfigurationPlan{}, err
	}
	mask1, err := u32("ChEnableMask1", 32)
	if err != nil {
		return ConfigurationPlan{}, err
	}
	triggerMask, err := choice("BunchTrgSource", map[string]uint32{"SW_ONLY": 1, "T1-IN": 3, "Q-OR": 5, "T-OR": 9, "T0-IN": 0x11, "PTRG": 0x21, "TLOGIC": 0x41})
	if err != nil {
		return ConfigurationPlan{}, err
	}
	trefMask, err := choice("TrefSource", map[string]uint32{"T0-IN": 1, "T1-IN": 2, "Q-OR": 4, "T-OR": 8, "PTRG": 0x10, "TLOGIC": 0x40})
	if err != nil {
		return ConfigurationPlan{}, err
	}
	validationMask, err := choice("ValidationSource", map[string]uint32{"SW_CMD": 1, "T0-IN": 2, "T1-IN": 4})
	if err != nil {
		return ConfigurationPlan{}, err
	}
	vetoMask, err := choice("VetoSource", map[string]uint32{"DISABLED": 0, "SW_CMD": 1, "T0-IN": 2, "T1-IN": 4})
	if err != nil {
		return ConfigurationPlan{}, err
	}
	outOptions := map[string]uint32{"T0-IN": 1, "T1-IN": 1, "BUNCHTRG": 2, "T-OR": 4, "Q-OR": 4, "RUN": 8, "PTRG": 0x10, "BUSY": 0x20, "DPROBE": 0x40, "TLOGIC": 0x80, "SQ_WAVE": 0x100, "TDL_SYNC": 0x200, "RUN_SYNC": 0x400, "ZERO": 0}
	t0, err := choice("T0_Out", outOptions)
	if err != nil {
		return ConfigurationPlan{}, err
	}
	t1, err := choice("T1_Out", outOptions)
	if err != nil {
		return ConfigurationPlan{}, err
	}
	majority, err := u32("MajorityLevel", 8)
	if err != nil {
		return ConfigurationPlan{}, err
	}
	logic, err := choice("TriggerLogic", map[string]uint32{"OR64": 0, "AND2_OR32": 1, "OR32_AND2": 2, "OR16_AND4": 3, "MAJ64": 4, "MAJ32_AND2": 5, "OR_QUAD": 6})
	if err != nil {
		return ConfigurationPlan{}, err
	}
	chWidth, err := timeNS("ChTrg_Width")
	if err != nil {
		return ConfigurationPlan{}, err
	}
	logicWidth, err := timeNS("Tlogic_Width")
	if err != nil {
		return ConfigurationPlan{}, err
	}
	period, err := timeNS("PtrgPeriod")
	if err != nil {
		return ConfigurationPlan{}, err
	}
	trefWindow, err := timeNS("TrefWindow")
	if err != nil {
		return ConfigurationPlan{}, err
	}
	trefDelay, err := timeNS("TrefDelay")
	if err != nil {
		return ConfigurationPlan{}, err
	}
	holdDelay, err := timeNS("HoldDelay")
	if err != nil {
		return ConfigurationPlan{}, err
	}
	muxPeriod, err := timeNS("MuxClkPeriod")
	if err != nil {
		return ConfigurationPlan{}, err
	}
	holdOff, err := timeNS("Hit_HoldOff")
	if err != nil {
		return ConfigurationPlan{}, err
	}
	qd, err := u32("QD_CoarseThreshold", 16)
	if err != nil {
		return ConfigurationPlan{}, err
	}
	td, err := u32("TD_CoarseThreshold", 16)
	if err != nil {
		return ConfigurationPlan{}, err
	}
	qdMask0, err := u32("Q_DiscrMask0", 32)
	if err != nil {
		return ConfigurationPlan{}, err
	}
	qdMask1, err := u32("Q_DiscrMask1", 32)
	if err != nil {
		return ConfigurationPlan{}, err
	}
	tdMask0, err := u32("Tlogic_Mask0", 32)
	if err != nil {
		return ConfigurationPlan{}, err
	}
	tdMask1, err := u32("Tlogic_Mask1", 32)
	if err != nil {
		return ConfigurationPlan{}, err
	}
	shape := map[string]uint32{"87.5 ns": 0, "75 ns": 1, "62.5 ns": 2, "50 ns": 3, "37.5 ns": 4, "25 ns": 5, "12.5 ns": 6}
	lgShape, err := choice("LG_ShapingTime", shape)
	if err != nil {
		return ConfigurationPlan{}, err
	}
	hgShape, err := choice("HG_ShapingTime", shape)
	if err != nil {
		return ConfigurationPlan{}, err
	}
	tpSource, err := choice("TestPulseSource", map[string]uint32{"OFF": 0, "EXT": 0, "T0-IN": 1, "T1-IN": 2, "PTRG": 3, "SW-CMD": 4})
	if err != nil {
		return ConfigurationPlan{}, err
	}
	tpAmp, err := u32("TestPulseAmplitude", 16)
	if err != nil {
		return ConfigurationPlan{}, err
	}
	if values["TestPulseSource"].Value == "OFF" {
		tpAmp = 0
	}
	if _, err := choice("TestPulsePreamp", map[string]uint32{"LG": 1, "HG": 2, "BOTH": 3}); err != nil {
		return ConfigurationPlan{}, err
	}
	destination, err := require("TestPulseDestination")
	if err != nil {
		return ConfigurationPlan{}, err
	}
	if destination.Value != "NONE" && destination.Value != "ALL" && destination.Value != "EVEN" && destination.Value != "ODD" {
		fields := strings.Fields(destination.Value)
		if len(fields) != 2 || fields[0] != "CH" {
			return ConfigurationPlan{}, fmt.Errorf("line %d: TestPulseDestination has unsupported value %q", destination.Line, destination.Value)
		}
		channel, parseErr := strconv.Atoi(fields[1])
		if parseErr != nil || channel < 0 || channel >= ChannelCount {
			return ConfigurationPlan{}, fmt.Errorf("line %d: TestPulseDestination channel %q out of range", destination.Line, fields[1])
		}
	}
	if _, err := u32("ZS_Threshold_LG", 16); err != nil {
		return ConfigurationPlan{}, err
	}
	if _, err := u32("ZS_Threshold_HG", 16); err != nil {
		return ConfigurationPlan{}, err
	}
	probeChannel0, err := u32("ProbeChannel0", 6)
	if err != nil {
		return ConfigurationPlan{}, err
	}
	probeChannel1, err := u32("ProbeChannel1", 6)
	if err != nil {
		return ConfigurationPlan{}, err
	}
	analogOptions := map[string]uint32{"OFF": 0, "FAST": 1, "SLOW_LG": 2, "SLOW_HG": 3, "PREAMP_HG": 4, "PREAMP_LG": 5}
	analog0, err := choice("AnalogProbe0", analogOptions)
	if err != nil {
		return ConfigurationPlan{}, err
	}
	analog1, err := choice("AnalogProbe1", analogOptions)
	if err != nil {
		return ConfigurationPlan{}, err
	}
	digitalOptions := map[string]uint32{"OFF": 0xff, "PEAK_LG": 0x10, "PEAK_HG": 0x11, "HOLD": 0x16, "START_CONV": 0x12, "DATA_COMMIT": 0x21, "DATA_VALID": 0x20, "CLK_1024": 0, "VAL_WINDOW": 0x1a, "T_OR": 4, "Q_OR": 5}
	digital0, err := choice("DigitalProbe0", digitalOptions)
	if err != nil {
		return ConfigurationPlan{}, err
	}
	digital1, err := choice("DigitalProbe1", digitalOptions)
	if err != nil {
		return ConfigurationPlan{}, err
	}
	analogValue := func(selection, channel uint32) uint32 {
		if selection == 0 {
			return 0
		}
		return 0x80 | channel | (selection-1)<<9
	}
	fastShaper, err := choice("FastShaperInput", map[string]uint32{"HG-PA": 0, "LG-PA": 1})
	if err != nil {
		return ConfigurationPlan{}, err
	}
	hvRange, err := choice("HV_Adjust_Range", map[string]uint32{"2.5": 0, "4.5": 1, "DISABLED": 2})
	if err != nil {
		return ConfigurationPlan{}, err
	}
	hvAdjustment, err := u32("HV_IndivAdj", 8)
	if err != nil {
		return ConfigurationPlan{}, err
	}
	hvVoltage, err := decimalUnit("HV_Vbias", map[string]float64{"uV": 1e-6, "mV": 1e-3, "V": 1})
	if err != nil {
		return ConfigurationPlan{}, err
	}
	if hvVoltage < 20 || hvVoltage > 85 {
		return ConfigurationPlan{}, fmt.Errorf("HV_Vbias %.6g V outside DT5202 production range [20,85] V", hvVoltage)
	}
	hvCurrent, err := decimalUnit("HV_Imax", map[string]float64{"uA": 1e-3, "mA": 1, "A": 1e3})
	if err != nil {
		return ConfigurationPlan{}, err
	}
	if hvCurrent < 0 {
		return ConfigurationPlan{}, fmt.Errorf("HV_Imax %.6g mA must not be negative", hvCurrent)
	}
	feedbackCoefficient, err := decimalUnit("TempFeedbackCoeff", map[string]float64{})
	if err != nil {
		return ConfigurationPlan{}, err
	}
	feedbackEnable, err := u32("EnableTempFeedback", 1)
	if err != nil {
		return ConfigurationPlan{}, err
	}
	tempSensor, err := require("TempSensType")
	if err != nil {
		return ConfigurationPlan{}, err
	}
	coefficients := [3]float64{}
	switch tempSensor.Value {
	case "TMP37":
		coefficients = [3]float64{0, 50, 0}
	case "LM94021_G11":
		coefficients = [3]float64{194.25, -73.63, 0}
	case "LM94021_G00":
		coefficients = [3]float64{188.12, -181.62, 0}
	default:
		fields := strings.Fields(tempSensor.Value)
		if len(fields) != 3 {
			return ConfigurationPlan{}, fmt.Errorf("line %d: TempSensType must be a known sensor or three coefficients", tempSensor.Line)
		}
		for index := range fields {
			value, parseErr := strconv.ParseFloat(fields[index], 64)
			if parseErr != nil {
				return ConfigurationPlan{}, fmt.Errorf("line %d: invalid TempSensType coefficient %q", tempSensor.Line, fields[index])
			}
			coefficients[index] = value
		}
	}

	plan := ConfigurationPlan{Board: board, Writes: []RegisterWrite{
		{ChannelMaskLow, mask0}, {ChannelMaskHigh, mask1}, {AcquisitionControl, acqControl},
		{ChannelTriggerWidth, uint32(chWidth / 8)}, {TriggerLogicWidth, uint32(logicWidth / 8)},
		{T0OutputMask, t0}, {T1OutputMask, t1}, {DwellTime, uint32(period / 8)}, {TriggerMask, triggerMask},
		{RunMask, 1}, {TimeReferenceMask, trefMask}, {TimeReferenceWindow, uint32(trefWindow * 2)},
		{TimeReferenceDelay, uint32(int32(trefDelay / 8))}, {TriggerLogicDefinition, majority<<8 | logic},
		{VetoMask, vetoMask}, {ValidationMask, validationMask}, {TestPulseControl, tpSource}, {TestPulseDAC, tpAmp},
		{ChargeCoarseThreshold, qd}, {TimeCoarseThreshold, td}, {LowGainShapingTime, lgShape}, {HighGainShapingTime, hgShape},
		{ChargeDiscriminatorMaskLow, qdMask0}, {ChargeDiscriminatorMaskHigh, qdMask1},
		{TimeDiscriminatorMaskLow, tdMask0}, {TimeDiscriminatorMaskHigh, tdMask1},
		{HoldDelay, uint32(holdDelay / 8)}, {AnalogMuxSequenceControl, uint32(muxPeriod / 8)}, {TriggerHoldOff, uint32(holdOff / 8)},
		{CitirocSlowControl, 0}, {CitirocProbe, analogValue(analog0, probeChannel0)},
		{CitirocSlowControl, 0x200}, {CitirocProbe, analogValue(analog1, probeChannel1)},
		{DigitalProbe, digital0 | digital1<<8},
	}}
	lg, err := u32("LG_Gain", 8)
	if err != nil {
		return ConfigurationPlan{}, err
	}
	hg, err := u32("HG_Gain", 8)
	if err != nil {
		return ConfigurationPlan{}, err
	}
	qfine, err := u32("QD_FineThreshold", 8)
	if err != nil {
		return ConfigurationPlan{}, err
	}
	tfine, err := u32("TD_FineThreshold", 8)
	if err != nil {
		return ConfigurationPlan{}, err
	}
	var citirocChannels [ChannelCount]CitirocChannel
	for ch := uint8(0); ch < ChannelCount; ch++ {
		hvValue := uint32(0x100 | hvAdjustment)
		if hvRange == 2 {
			hvValue = 0x1ff
		}
		plan.Writes = append(plan.Writes,
			RegisterWrite{IndividualRegister(LowGain, ch), lg}, RegisterWrite{IndividualRegister(HighGain, ch), hg},
			RegisterWrite{IndividualRegister(ChargeFineThreshold, ch), qfine}, RegisterWrite{IndividualRegister(TimeFineThreshold, ch), tfine},
			RegisterWrite{IndividualRegister(HVIndividualAdjustment, ch), hvValue})
		citirocChannels[ch] = CitirocChannel{TimeFineThreshold: uint8(tfine), ChargeFineThreshold: uint8(qfine), HVAdjustment: uint16(hvValue), HighGain: uint8(hg), LowGain: uint8(lg)}
	}
	common := CitirocCommon{DiscriminatorMask: qdMask0, LowShapingTime: uint8(lgShape), HighShapingTime: uint8(hgShape), ChargeCoarseThreshold: uint16(qd), TimeCoarseThreshold: uint16(td), FastShaperOnLowGain: fastShaper != 0, InputDACReference45V: hvRange != 0, PeakSensingExternalTrigger: true, OTAForceOn: true, NegativeTriggerPolarity: true}
	plan.Citiroc = SplitCitirocChannels(citirocChannels, common)
	plan.Citiroc[1].Common.DiscriminatorMask = qdMask1
	plan.HV = buildHVPlan(hvVoltage, hvCurrent, coefficients, feedbackEnable != 0, feedbackCoefficient)
	plan.Deferred = []string{"Pedestal"}
	if acqMode == 1 {
		plan.Deferred = append(plan.Deferred, "ZS_Threshold_LG", "ZS_Threshold_HG")
	} else {
		plan.Inactive = append(plan.Inactive,
			InactiveSetting{"ZS_Threshold_LG", "JANUS applies energy zero suppression only in spectroscopy mode"},
			InactiveSetting{"ZS_Threshold_HG", "JANUS applies energy zero suppression only in spectroscopy mode"})
	}
	if values["TestPulseSource"].Value == "OFF" {
		plan.Inactive = append(plan.Inactive,
			InactiveSetting{"TestPulseAmplitude", "test-pulse source is OFF; effective DAC is zero"},
			InactiveSetting{"TestPulseDestination", "test-pulse source is OFF"},
			InactiveSetting{"TestPulsePreamp", "test-pulse source is OFF"})
	}
	if analog0 == 0 {
		plan.Inactive = append(plan.Inactive, InactiveSetting{"ProbeChannel0", "analog probe 0 is OFF"})
	}
	if analog1 == 0 {
		plan.Inactive = append(plan.Inactive, InactiveSetting{"ProbeChannel1", "analog probe 1 is OFF"})
	}
	return plan, nil
}
