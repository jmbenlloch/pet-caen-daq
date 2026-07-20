package dt5202

// Register is a DT5202 FPGA register address. These constants are
// source-confirmed against FERS_Registers_520X.h bundled with JANUS 5.0.0.
type Register uint32

const (
	AcquisitionControl          Register = 0x01000000
	RunMask                     Register = 0x01000004
	TriggerMask                 Register = 0x01000008
	TimeReferenceMask           Register = 0x0100000c
	ValidationMask              Register = 0x01000010
	T0OutputMask                Register = 0x01000014
	T1OutputMask                Register = 0x01000018
	VetoMask                    Register = 0x0100001c
	AcquisitionControl2         Register = 0x01000020
	TimeReferenceDelay          Register = 0x01000048
	TimeReferenceWindow         Register = 0x0100004c
	DwellTime                   Register = 0x01000050
	ListSize                    Register = 0x01000054
	PacketMaximumCount          Register = 0x01000064
	DigitalProbe                Register = 0x01000068
	ChannelMaskLow              Register = 0x01000100
	ChannelMaskHigh             Register = 0x01000104
	CitirocConfig               Register = 0x01000108
	CitirocEnable               Register = 0x0100010c
	CitirocProbe                Register = 0x01000110
	ChargeCoarseThreshold       Register = 0x01000114
	TimeCoarseThreshold         Register = 0x01000118
	LowGainShapingTime          Register = 0x0100011c
	HighGainShapingTime         Register = 0x01000120
	HoldDelay                   Register = 0x01000124
	AnalogMuxSequenceControl    Register = 0x01000128
	WaveformLength              Register = 0x0100012c
	CitirocSlowControl          Register = 0x01000130
	CitirocSlowData             Register = 0x01000134
	ChargeDiscriminatorMaskLow  Register = 0x01000138
	ChargeDiscriminatorMaskHigh Register = 0x0100013c
	TimeDiscriminatorMaskLow    Register = 0x01000140
	TimeDiscriminatorMaskHigh   Register = 0x01000144
	TestPulseControl            Register = 0x01000200
	TestPulseDAC                Register = 0x01000204
	HVRegisterAddress           Register = 0x01000210
	HVRegisterData              Register = 0x01000214
	TriggerHoldOff              Register = 0x01000218
	DCOffset                    Register = 0x01000220
	SPIData                     Register = 0x01000224
	TestLED                     Register = 0x01000228
	TDCMode                     Register = 0x0100022c
	TDCData                     Register = 0x01000230
	TriggerLogicDefinition      Register = 0x01000234
	ChannelTriggerWidth         Register = 0x01000238
	TriggerLogicWidth           Register = 0x0100023c
	I2CAddress                  Register = 0x01000240
	I2CData                     Register = 0x01000244
	FirmwareRevision            Register = 0x01000300
	AcquisitionStatus           Register = 0x01000304
	RealTime                    Register = 0x01000308
	DeadTime                    Register = 0x01000310
	BoardTemperature            Register = 0x01000340
	FPGATemperature             Register = 0x01000348
	TimeORCount                 Register = 0x01000350
	ChargeORCount               Register = 0x01000354
	HVVoltageMonitor            Register = 0x01000356
	HVCurrentMonitor            Register = 0x01000358
	HVStatus                    Register = 0x01000360
	ProductID                   Register = 0x01000400
	PCBRevision                 Register = 0x01000404
	FERSCode                    Register = 0x01000408
	MicrocontrollerStatus       Register = 0x01000600
	MicrocontrollerShutdown     Register = 0x01000604
	SoftwareCompatibility       Register = 0x01004000
	Commands                    Register = 0x01008000
	RebootFPGA                  Register = 0x0100fff0
	ZeroSuppressionLowGain      Register = 0x02000000
	ZeroSuppressionHighGain     Register = 0x02000004
	ChargeFineThreshold         Register = 0x02000008
	TimeFineThreshold           Register = 0x0200000c
	LowGain                     Register = 0x02000010
	HighGain                    Register = 0x02000014
	HVIndividualAdjustment      Register = 0x02000018
	HitCounter                  Register = 0x02000800
)

func IndividualRegister(base Register, channel uint8) Register {
	return Register(0x02000000 | (uint32(base) & 0xffff) | uint32(channel)<<16)
}

func BroadcastRegister(base Register) Register {
	return Register(0x03000000 | (uint32(base) & 0xffff))
}

type Command uint32

const (
	CommandTimeReset            Command = 0x11
	CommandAcquisitionStart     Command = 0x12
	CommandAcquisitionStop      Command = 0x13
	CommandSoftwareTrigger      Command = 0x14
	CommandGlobalReset          Command = 0x15
	CommandTestPulse            Command = 0x16
	CommandResetPeriodicTrigger Command = 0x17
	CommandClearData            Command = 0x18
	CommandValidation           Command = 0x19
	CommandSetVeto              Command = 0x1a
	CommandClearVeto            Command = 0x1b
	CommandTDLinkSync           Command = 0x1c
	CommandUseInternalClock     Command = 0x1e
	CommandUseExternalClock     Command = 0x1f
	CommandConfigureASIC        Command = 0x20
)

type Status uint32

const (
	StatusReady Status = 1 << iota
	StatusFailure
	StatusRunning
	StatusTDLinkSynchronized
	StatusFPGAOverTemperature
	StatusTDCReadoutError
	StatusTDLinkLossOfLock
	StatusTDC0LossOfLock
	StatusTDC1LossOfLock
	StatusReadoutClockLossOfLock
	StatusTDLinkDisabled
	StatusTDC0OverTemperature
	StatusTDC1OverTemperature
	StatusBoardOverTemperature
	StatusCRCError
	statusUnused15
	StatusSPIBusy
	StatusI2CBusy
	StatusI2CFailure
)

func (s Status) Has(flag Status) bool { return s&flag != 0 }
