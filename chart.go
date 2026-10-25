package main

import "math"

type temperaturePoint struct {
	Day   int
	Value float64
}

// Keep missing days as breaks in the line, rather than inventing a trend.
func temperatureSegments(rows []DayRow, afternoon bool) [][]temperaturePoint {
	var segments [][]temperaturePoint
	var current []temperaturePoint
	for day, row := range rows {
		observation, pending := row.Morning, row.MorningPending
		if afternoon {
			observation, pending = row.Afternoon, row.AfternoonPending
		}
		if pending || observation == nil || observation.Temp == nil || math.IsNaN(*observation.Temp) || math.IsInf(*observation.Temp, 0) {
			if len(current) > 0 {
				segments = append(segments, current)
				current = nil
			}
			continue
		}
		current = append(current, temperaturePoint{Day: day, Value: *observation.Temp})
	}
	if len(current) > 0 {
		segments = append(segments, current)
	}
	return segments
}

func chartScale(series ...[][]temperaturePoint) (low, high, step float64, available bool) {
	low, high = math.Inf(1), math.Inf(-1)
	for _, segments := range series {
		for _, segment := range segments {
			for _, point := range segment {
				low = math.Min(low, point.Value)
				high = math.Max(high, point.Value)
			}
		}
	}
	if math.IsInf(low, 1) {
		return 0, 1, 0.25, false
	}
	if high-low < 1 {
		centre := (low + high) / 2
		low, high = centre-1, centre+1
	}
	padding := (high - low) * 0.08
	low -= padding
	high += padding
	raw := (high - low) / 4
	power := math.Pow(10, math.Floor(math.Log10(raw)))
	step = power
	for _, factor := range []float64{1, 2, 2.5, 5, 10} {
		if factor*power >= raw {
			step = factor * power
			break
		}
	}
	return math.Floor(low/step) * step, math.Ceil(high/step) * step, step, true
}

func chartLabelDays(count, limit int) []int {
	if count <= 0 || limit <= 0 {
		return nil
	}
	if count == 1 || limit == 1 {
		return []int{0}
	}
	stride := int(math.Ceil(float64(count-1) / float64(limit-1)))
	var days []int
	for day := 0; day < count-1; day += stride {
		days = append(days, day)
	}
	// Preserve the final date; avoid a label almost touching its neighbour.
	if len(days) > 1 && count-1-days[len(days)-1] < stride/2 {
		days = days[:len(days)-1]
	}
	return append(days, count-1)
}
