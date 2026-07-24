//go:build hdf5

package hdf5store

import (
	"errors"
	"fmt"
	"unsafe"

	hdf5 "github.com/next-exp/hdf5-go"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/staircase"
)

type staircasePointRow struct {
	Threshold          uint32
	ElapsedNanoseconds uint64
	TORCount           uint32
	QORCount           uint32
	TORRateCPS         float64
	QORRateCPS         float64
}

type staircaseChannelRow struct {
	PointIndex uint32
	Channel    uint8
	Count      uint32
	RateCPS    float64
}

// WriteStaircase writes a finalized, self-contained scan artifact. Point rows
// and channel rows are separate datasets so channel arrays remain portable
// across HDF5 readers.
func WriteStaircase(path string, metadata []byte, points []staircase.Point) (err error) {
	compression, err := compressionName()
	if err != nil {
		return err
	}
	file, err := hdf5.CreateFile(path, hdf5.F_ACC_EXCL)
	if err != nil {
		return fmt.Errorf("create staircase HDF5: %w", err)
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	root, err := file.OpenGroup("/")
	if err != nil {
		return err
	}
	defer root.Close()
	if err := writeUintAttribute(root, "schema_version", 1); err != nil {
		return err
	}
	scan, err := file.CreateGroup("staircase")
	if err != nil {
		return err
	}
	defer scan.Close()
	if err := createBytes(scan, "metadata_json", metadata); err != nil {
		return err
	}
	pointDataset, err := createTable(scan, "points", compoundStaircasePoint(), compression)
	if err != nil {
		return err
	}
	defer pointDataset.Close()
	channelDataset, err := createTable(scan, "channel_measurements", compoundStaircaseChannel(), compression)
	if err != nil {
		return err
	}
	defer channelDataset.Close()
	pointTable := table{dataset: pointDataset}
	channelTable := table{dataset: channelDataset}
	pointRows := make([]staircasePointRow, 0, len(points))
	channelRows := make([]staircaseChannelRow, 0, len(points)*staircase.ChannelCount)
	for pointIndex, point := range points {
		pointRows = append(pointRows, staircasePointRow{
			Threshold: point.Threshold, ElapsedNanoseconds: uint64(point.Elapsed),
			TORCount: point.TORCount, QORCount: point.QORCount,
			TORRateCPS: point.TORRateCPS, QORRateCPS: point.QORRateCPS,
		})
		for channel := 0; channel < staircase.ChannelCount; channel++ {
			channelRows = append(channelRows, staircaseChannelRow{
				PointIndex: uint32(pointIndex), Channel: uint8(channel),
				Count: point.ChannelCounts[channel], RateCPS: point.ChannelRatesCPS[channel],
			})
		}
	}
	if err := appendRows(&pointTable, pointRows); err != nil {
		return err
	}
	if err := appendRows(&channelTable, channelRows); err != nil {
		return err
	}
	return file.Flush(hdf5.F_SCOPE_GLOBAL)
}

func compoundStaircasePoint() *hdf5.CompoundType {
	var value staircasePointRow
	return mustCompound(unsafe.Sizeof(value), []field{
		{"threshold", unsafe.Offsetof(value.Threshold), hdf5.T_STD_U32LE},
		{"elapsed_nanoseconds", unsafe.Offsetof(value.ElapsedNanoseconds), hdf5.T_STD_U64LE},
		{"t_or_count", unsafe.Offsetof(value.TORCount), hdf5.T_STD_U32LE},
		{"q_or_count", unsafe.Offsetof(value.QORCount), hdf5.T_STD_U32LE},
		{"t_or_rate_cps", unsafe.Offsetof(value.TORRateCPS), hdf5.T_IEEE_F64LE},
		{"q_or_rate_cps", unsafe.Offsetof(value.QORRateCPS), hdf5.T_IEEE_F64LE},
	})
}

func compoundStaircaseChannel() *hdf5.CompoundType {
	var value staircaseChannelRow
	return mustCompound(unsafe.Sizeof(value), []field{
		{"point_index", unsafe.Offsetof(value.PointIndex), hdf5.T_STD_U32LE},
		{"channel", unsafe.Offsetof(value.Channel), hdf5.T_STD_U8LE},
		{"count", unsafe.Offsetof(value.Count), hdf5.T_STD_U32LE},
		{"rate_cps", unsafe.Offsetof(value.RateCPS), hdf5.T_IEEE_F64LE},
	})
}
