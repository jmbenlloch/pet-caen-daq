//go:build hdf5

package hdf5store

import (
	"path/filepath"
	"testing"

	hdf5 "github.com/next-exp/hdf5-go"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
)

func TestWriterStoresQueryFriendlyEffectiveConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "configuration.h5")
	plan := dt5202.ConfigurationPlan{
		Board: 3,
		Writes: []dt5202.RegisterWrite{
			{Address: dt5202.ChannelMaskLow, Value: 1 << 5},
			{Address: dt5202.ChargeDiscriminatorMaskLow, Value: 1 << 5},
			{Address: dt5202.TimeDiscriminatorMaskLow, Value: 1 << 5},
			{Address: dt5202.TriggerMask, Value: 0x41},
		},
		HV: dt5202.HVPlan{
			VoltageV: 45.4, CurrentLimitMA: 1, TemperatureFeedback: true, FeedbackMVPerC: 35,
			TemperatureCoefficients: [3]float64{1, 2, 3},
			Transactions:            []dt5202.HVTransaction{{Register: 4, DataType: 2, Data: 99}},
		},
		Pedestal: dt5202.PedestalPlan{Common: 10, AcquisitionMode: 1, ZeroSuppressLowGain: 2, ZeroSuppressHighGain: 3},
	}
	plan.Citiroc[0].Channels[5] = dt5202.CitirocChannel{
		TimeFineThreshold: 4, ChargeFineThreshold: 6, HVAdjustment: 257,
		HighGain: 12, LowGain: 13, CalibrateHighGain: true,
	}
	plan.Citiroc[0].Common = dt5202.CitirocCommon{
		DiscriminatorMask: 0xffffffdf, ChargeCoarseThreshold: 250, TimeCoarseThreshold: 220,
		LowShapingTime: 3, HighShapingTime: 4, EnableChannelTriggers: true,
	}
	writer, err := CreateWithMetadata(path, Metadata{
		RunID: "typed-configuration", EffectiveConfiguration: []dt5202.ConfigurationPlan{plan},
		Boards: []runstore.BoardIdentity{{Board: 3, Chain: 2, Node: 1, ProductID: 5202, FirmwareRevision: 0x0708}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Finalize([]byte(`{"schema_version":1,"run_id":"typed-configuration","event_count":"0"}`)); err != nil {
		t.Fatal(err)
	}
	file, err := hdf5.OpenFile(path, hdf5.F_ACC_RDONLY)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	var boards []configurationBoardRow
	readConfigurationRows(t, file, "configuration/effective/boards", &boards, 1)
	if boards[0].Board != 3 || boards[0].Chain != 2 || boards[0].Node != 1 || boards[0].FirmwareRevision != 0x0708 {
		t.Fatalf("boards = %+v", boards)
	}
	var channels []configurationChannelRow
	readConfigurationRows(t, file, "configuration/effective/channels", &channels, 64)
	channel := channels[5]
	if channel.Board != 3 || channel.Channel != 5 || channel.ReadoutEnabled != 1 || channel.QDEnabled != 1 ||
		channel.TDEnabled != 1 || channel.QDFine != 6 || channel.TDFine != 4 || channel.HVAdjustment != 257 ||
		channel.CalibrateHighGain != 1 {
		t.Fatalf("channel = %+v", channel)
	}
	var streams []configurationStreamWordRow
	readConfigurationRows(t, file, "configuration/effective/citiroc_stream_words", &streams, 72)
	if streams[0].Board != 3 || streams[0].Chip != 0 || streams[0].WordIndex != 0 || streams[0].BitCount != dt5202.CitirocBitCount {
		t.Fatalf("stream = %+v", streams[0])
	}
	var writes []configurationWriteRow
	readConfigurationRows(t, file, "configuration/effective/fpga_writes", &writes, 4)
	if writes[3].Ordinal != 3 || writes[3].Address != uint32(dt5202.TriggerMask) || writes[3].Value != 0x41 {
		t.Fatalf("writes = %+v", writes)
	}
	var hv []configurationHVRow
	readConfigurationRows(t, file, "configuration/effective/hv_plans", &hv, 1)
	if hv[0].VoltageV != 45.4 || hv[0].TemperatureFeedback != 1 || hv[0].Coefficient2 != 3 {
		t.Fatalf("HV = %+v", hv)
	}
	var transactions []configurationHVTransactionRow
	readConfigurationRows(t, file, "configuration/effective/hv_transactions", &transactions, 1)
	if transactions[0].Register != 4 || transactions[0].Data != 99 {
		t.Fatalf("transactions = %+v", transactions)
	}
	var pedestal []configurationPedestalChannelRow
	readConfigurationRows(t, file, "configuration/effective/pedestal_channels", &pedestal, 64)
	if pedestal[5].ZeroSuppressLowGain != 2 || pedestal[5].ZeroSuppressHighGain != 3 {
		t.Fatalf("pedestal = %+v", pedestal[5])
	}
}

func readConfigurationRows[T any](t *testing.T, file *hdf5.File, name string, rows *[]T, want int) {
	t.Helper()
	dataset, err := file.OpenDataset(name)
	if err != nil {
		t.Fatal(err)
	}
	defer dataset.Close()
	space := dataset.Space()
	if space == nil {
		t.Fatalf("%s has no dataspace", name)
	}
	dimensions, _, err := space.SimpleExtentDims()
	space.Close()
	if err != nil {
		t.Fatal(err)
	}
	if int(dimensions[0]) != want {
		t.Fatalf("%s rows = %d, want %d", name, dimensions[0], want)
	}
	*rows = make([]T, want)
	if want > 0 {
		if err := dataset.Read(rows); err != nil {
			t.Fatal(err)
		}
	}
}
