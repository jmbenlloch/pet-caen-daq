package service

import (
	"fmt"
	"math"

	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
)

func sourcePlan(c *daqv1.SourceScanConfiguration) ([]*daqv1.SourceScanPoint, error) {
	if c == nil {
		return nil, fmt.Errorf("source scan configuration is required")
	}
	for _, v := range []float64{c.XMinMm, c.XMaxMm, c.YMinMm, c.YMaxMm, c.PitchMm, c.MmPerTurn} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("coordinates and calibration must be finite")
		}
	}
	if c.PitchMm <= 0 || c.MmPerTurn <= 0 || c.FullStepsPerTurn == 0 || c.Microsteps == 0 || c.FullStepsPerTurn > 100000 || c.Microsteps > 256 {
		return nil, fmt.Errorf("positive pitch/calibration required (steps/turn <= 100000, microsteps <= 256)")
	}
	if c.XMinMm > c.XMaxMm || c.YMinMm > c.YMaxMm {
		return nil, fmt.Errorf("axis minimum exceeds maximum")
	}
	if c.PulseHighUs < 2 || c.PulseHighUs > 10000 || c.PulseLowUs < 2 || c.PulseLowUs > 10000 || c.SettleMilliseconds > 60000 {
		return nil, fmt.Errorf("pulse durations must be 2–10000 us and settling at most 60000 ms")
	}
	if (c.DwellMilliseconds == 0) == (c.EventCount == 0) || c.DwellMilliseconds > 86400000 || c.PointTimeoutSeconds == 0 || c.PointTimeoutSeconds > 86400 || c.DwellMilliseconds >= uint64(c.PointTimeoutSeconds)*1000 {
		return nil, fmt.Errorf("choose positive time or event count, and a point timeout (1–86400 s) longer than dwell")
	}
	counts := [2]int{}
	for i, span := range []float64{(c.XMaxMm - c.XMinMm) / c.PitchMm, (c.YMaxMm - c.YMinMm) / c.PitchMm} {
		if span > 9999 || math.Abs(span-math.Round(span)) > 1e-7 {
			return nil, fmt.Errorf("each axis span must be an integer multiple of pitch, at most 10000 points")
		}
		counts[i] = int(math.Round(span)) + 1
	}
	if counts[0]*counts[1] > 10000 {
		return nil, fmt.Errorf("at most 10000 scan points are supported")
	}
	scale := float64(c.FullStepsPerTurn) * float64(c.Microsteps) / c.MmPerTurn
	if math.IsInf(scale, 0) || math.IsNaN(scale) || scale <= 0 {
		return nil, fmt.Errorf("calibration produces an invalid steps/mm scale")
	}
	if c.PitchMm*scale < 1 {
		return nil, fmt.Errorf("pitch is smaller than one motor step")
	}
	points := make([]*daqv1.SourceScanPoint, 0, counts[0]*counts[1])
	lastX, lastY := int32(0), int32(0)
	for row := 0; row < counts[1]; row++ {
		for k := 0; k < counts[0]; k++ {
			col := k
			if row%2 == 1 {
				col = counts[0] - 1 - k
			}
			x, y := c.XMinMm+float64(col)*c.PitchMm, c.YMaxMm-float64(row)*c.PitchMm
			xs, ys := math.Round(x*scale), math.Round(y*scale)
			// Match the firmware's conservative integer range and avoid delta overflow.
			if math.Abs(xs) > 10000000 || math.Abs(ys) > 10000000 {
				return nil, fmt.Errorf("position exceeds +/-10000000 logical steps")
			}
			travel := (math.Abs(xs-float64(lastX)) + math.Abs(ys-float64(lastY))) * float64(c.PulseHighUs+c.PulseLowUs) / 1e6
			if travel+(float64(c.SettleMilliseconds)+float64(c.DwellMilliseconds))/1000 >= float64(c.PointTimeoutSeconds) {
				return nil, fmt.Errorf("point timeout is shorter than planned travel, settling and dwell")
			}
			p := &daqv1.SourceScanPoint{Index: uint32(len(points)), Row: uint32(row), Column: uint32(col), RequestedXMm: x, RequestedYMm: y, XSteps: int32(xs), YSteps: int32(ys), CalculatedXMm: xs / scale, CalculatedYMm: ys / scale}
			points = append(points, p)
			lastX, lastY = p.XSteps, p.YSteps
		}
	}
	return points, nil
}
