package main

import (
	"math"
	"testing"
)

func TestChartKeepsGapsAndPendingHours(t *testing.T) {
	zero, cold, warm, nan := 0.0, -5.5, 12.0, math.NaN()
	rows := []DayRow{
		{Morning: &Observation{Temp: &zero}, Afternoon: &Observation{Temp: &warm}},
		{},
		{Morning: &Observation{Temp: &cold}, Afternoon: &Observation{Temp: &warm}, AfternoonPending: true},
		{Morning: &Observation{Temp: &warm}},
		{Morning: &Observation{Temp: &nan}},
	}
	segments := temperatureSegments(rows, false)
	if len(segments) != 2 || len(segments[0]) != 1 || len(segments[1]) != 2 {
		t.Fatalf("gaps bridged: %+v", segments)
	}
	if segments[0][0].Value != 0 || segments[1][0].Day != 2 || segments[1][0].Value != -5.5 {
		t.Fatal("zero or day alignment lost", segments)
	}
	afternoon := temperatureSegments(rows, true)
	if len(afternoon) != 1 || len(afternoon[0]) != 1 {
		t.Fatal("future temperature plotted", afternoon)
	}
}

func TestChartScaleIncludesBothSeries(t *testing.T) {
	a := [][]temperaturePoint{{{Day: 0, Value: -12.3}, {Day: 1, Value: 0}}}
	b := [][]temperaturePoint{{{Day: 0, Value: 18.7}}}
	low, high, step, available := chartScale(a, b)
	if !available || low > -12.3 || high < 18.7 || step <= 0 {
		t.Fatalf("invalid range %g..%g step %g", low, high, step)
	}
	if (high-low)/step > 7 {
		t.Fatal("too many grid lines")
	}
	low, high, step, available = chartScale([][]temperaturePoint{{{Day: 0, Value: 0}}})
	if !available || !(low < 0 && high > 0) || step <= 0 {
		t.Fatal("constant temperatures have no range")
	}
	if _, _, _, available := chartScale(nil); available {
		t.Fatal("empty data reported as available")
	}
}

func TestChartDateLabelsStayReadable(t *testing.T) {
	for _, count := range []int{1, 7, 30, 366, 367} {
		for _, limit := range []int{2, 6, 12} {
			labels := chartLabelDays(count, limit)
			if len(labels) > limit || labels[0] != 0 || labels[len(labels)-1] != count-1 {
				t.Fatalf("count=%d limit=%d labels=%v", count, limit, labels)
			}
			for i := 1; i < len(labels); i++ {
				if labels[i] <= labels[i-1] {
					t.Fatal("overlapping dates", labels)
				}
			}
		}
	}
}
