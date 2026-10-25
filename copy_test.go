package main

import "testing"

func TestTemperatureTextPreservesNumbersAndGaps(t *testing.T) {
	zero, cold, warm := 0.0, -2.7, 15.2
	rows := []DayRow{
		{Morning: &Observation{Temp: &zero}, Afternoon: &Observation{Temp: &warm}},
		{},
		{Morning: &Observation{Temp: &cold}, Afternoon: &Observation{}},
	}
	if got := temperatureText(rows, 1); got != "0.0\r\n\r\n-2.7" {
		t.Fatalf("morning: %q", got)
	}
	if got := temperatureText(rows, 4); got != "15.2\r\n\r\n" {
		t.Fatalf("afternoon: %q", got)
	}
	if got := temperatureText(rows[2:], 1); got != "-2.7" {
		t.Fatalf("single cell: %q", got)
	}
	if got := temperatureText(nil, 1); got != "" {
		t.Fatalf("empty: %q", got)
	}
}
