package main

import (
	"fmt"
	"time"
)

func rowCells(row DayRow) []string {
	date, _ := time.Parse(time.DateOnly, row.Date)
	label := date.Format(tr("date_format"))
	if row.Today {
		label = fmt.Sprintf(tr("table_01"), label)
	}
	morning, afternoon := observationCells(row.Morning), observationCells(row.Afternoon)
	if row.MorningPending {
		morning = []string{tr("table_02"), tr("table_03"), tr("table_03")}
	}
	if row.AfternoonPending {
		afternoon = []string{tr("table_02"), tr("table_03"), tr("table_03")}
	}
	cells := append([]string{label}, morning...)
	return append(cells, afternoon...)
}
