package main

import (
	"strconv"
	"strings"
)

// Keep a line for every day so pasted columns retain their date alignment.
func temperatureText(rows []DayRow, column int) string {
	values := make([]string, len(rows))
	for i, row := range rows {
		observation := row.Morning
		if column == 4 {
			observation = row.Afternoon
		}
		if observation != nil && observation.Temp != nil {
			values[i] = strconv.FormatFloat(*observation.Temp, 'f', 1, 64)
		}
	}
	return strings.Join(values, "\r\n")
}
