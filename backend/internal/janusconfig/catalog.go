package janusconfig

import "fmt"

// Owner identifies the subsystem that must give an assignment semantics.
// Classification is deliberately separate from parsing: parsing is lossless,
// while validation must reject any assignment whose behavior is unknown.
type Owner string

const (
	OwnerTopology   Owner = "topology"
	OwnerHardware   Owner = "hardware"
	OwnerRunControl Owner = "run_control"
	OwnerStorage    Owner = "storage"
	OwnerAnalysis   Owner = "analysis"
)

// ClassifiedAssignment pairs a losslessly parsed assignment with its declared
// semantic owner.
type ClassifiedAssignment struct {
	Assignment Assignment
	Owner      Owner
}

var settingOwners = map[string]Owner{
	"Open": OwnerTopology,

	"HV_Vbias": OwnerHardware, "HV_Imax": OwnerHardware,
	"HV_Adjust_Range": OwnerHardware, "HV_IndivAdj": OwnerHardware,
	"TempSensType": OwnerHardware, "TempFeedbackCoeff": OwnerHardware,
	"EnableTempFeedback": OwnerHardware, "AcquisitionMode": OwnerHardware,
	"EnableToT": OwnerHardware, "BunchTrgSource": OwnerHardware,
	"VetoSource": OwnerHardware, "ValidationSource": OwnerHardware,
	"ValidationMode": OwnerHardware, "CountingMode": OwnerHardware,
	"ChTrg_Width": OwnerHardware, "EnableCntZeroSuppr": OwnerHardware,
	"TrgIdMode": OwnerHardware, "TriggerLogic": OwnerHardware,
	"Tlogic_Width": OwnerHardware, "MajorityLevel": OwnerHardware,
	"PtrgPeriod": OwnerHardware, "TrefSource": OwnerHardware,
	"TrefWindow": OwnerHardware, "TrefDelay": OwnerHardware,
	"T0_Out": OwnerHardware, "T1_Out": OwnerHardware,
	"ChEnableMask0": OwnerHardware, "ChEnableMask1": OwnerHardware,
	"FastShaperInput": OwnerHardware, "TD_CoarseThreshold": OwnerHardware,
	"TD_FineThreshold": OwnerHardware, "Hit_HoldOff": OwnerHardware,
	"Tlogic_Mask0": OwnerHardware, "Tlogic_Mask1": OwnerHardware,
	"QD_CoarseThreshold": OwnerHardware, "QD_FineThreshold": OwnerHardware,
	"Q_DiscrMask0": OwnerHardware, "Q_DiscrMask1": OwnerHardware,
	"GainSelect": OwnerHardware, "HG_Gain": OwnerHardware,
	"LG_Gain": OwnerHardware, "Pedestal": OwnerHardware,
	"ZS_Threshold_LG": OwnerHardware, "ZS_Threshold_HG": OwnerHardware,
	"HG_ShapingTime": OwnerHardware, "LG_ShapingTime": OwnerHardware,
	"HoldDelay": OwnerHardware, "MuxClkPeriod": OwnerHardware,
	"AnalogProbe0": OwnerHardware, "DigitalProbe0": OwnerHardware,
	"ProbeChannel0": OwnerHardware, "AnalogProbe1": OwnerHardware,
	"DigitalProbe1": OwnerHardware, "ProbeChannel1": OwnerHardware,
	"TestPulseSource": OwnerHardware, "TestPulseAmplitude": OwnerHardware,
	"TestPulseDestination": OwnerHardware, "TestPulsePreamp": OwnerHardware,

	"StartRunMode": OwnerRunControl, "StopRunMode": OwnerRunControl,
	"EventBuildingMode": OwnerRunControl, "TstampCoincWindow": OwnerRunControl,
	"PresetTime": OwnerRunControl, "PresetCounts": OwnerRunControl,
	"JobFirstRun": OwnerRunControl, "JobLastRun": OwnerRunControl,
	"RunSleep": OwnerRunControl, "EnableJobs": OwnerRunControl,
	"RunNumber_AutoIncr": OwnerRunControl,

	"DataFilePath": OwnerStorage, "OF_OutFileUnit": OwnerStorage,
	"OF_EnMaxSize": OwnerStorage, "OF_MaxSize": OwnerStorage,
	"OF_RawData": OwnerStorage, "OF_ListBin": OwnerStorage,
	"OF_ListAscii": OwnerStorage, "OF_ListCSV": OwnerStorage,
	"OF_Sync": OwnerStorage, "OF_ServiceInfo": OwnerStorage,
	"OF_RunInfo": OwnerStorage,

	"DataAnalysis": OwnerAnalysis, "EnableListZeroSuppr": OwnerAnalysis,
	"OF_SpectHisto": OwnerAnalysis, "OF_ToAHisto": OwnerAnalysis,
	"OF_ToTHisto": OwnerAnalysis, "OF_MCS": OwnerAnalysis,
	"OF_Staircase": OwnerAnalysis, "EHistoNbin": OwnerAnalysis,
	"ToAHistoNbin": OwnerAnalysis, "ToARebin": OwnerAnalysis,
	"ToAHistoMin": OwnerAnalysis, "MCSHistoNbin": OwnerAnalysis,
}

// Classify rejects the whole document if any setting lacks an explicit owner.
// This prevents syntactically valid production fields from being ignored.
func (d *Document) Classify() ([]ClassifiedAssignment, error) {
	classified := make([]ClassifiedAssignment, 0, len(d.Assignments))
	for _, assignment := range d.Assignments {
		owner, ok := settingOwners[assignment.Name]
		if !ok {
			return nil, fmt.Errorf("line %d: unsupported JANUS setting %q", assignment.Line, assignment.Name)
		}
		classified = append(classified, ClassifiedAssignment{Assignment: assignment, Owner: owner})
	}
	return classified, nil
}
