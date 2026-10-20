package runstore

// SourcePosition preserves the complete motion/acquisition configuration with
// each independent run, including requested and quantized logical coordinates.
type SourcePosition struct {
	ScanID        string  `json:"scan_id"`
	Index         uint32  `json:"index"`
	Row           uint32  `json:"row"`
	Column        uint32  `json:"column"`
	RequestedXMM  float64 `json:"requested_x_mm"`
	RequestedYMM  float64 `json:"requested_y_mm"`
	XSteps        int32   `json:"x_steps"`
	YSteps        int32   `json:"y_steps"`
	CalculatedXMM float64 `json:"calculated_x_mm"`
	CalculatedYMM float64 `json:"calculated_y_mm"`
	PositionBasis string  `json:"position_basis"`
	// Configuration is the versioned protobuf JSON SourceScanConfiguration.
	Configuration string `json:"configuration_json"`
}
