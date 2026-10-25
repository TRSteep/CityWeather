package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTodayOnlyShowsElapsedHoursInCityTimezone(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Yekaterinburg")
	for _, test := range []struct {
		hour, minute     int
		today, afternoon bool
	}{
		{7, 59, false, false}, {8, 0, true, false}, {13, 59, true, false}, {14, 0, true, true}, {23, 59, true, true},
	} {
		t.Run(fmt.Sprintf("%02d:%02d", test.hour, test.minute), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				end := "2026-10-24"
				if test.today {
					end = "2026-10-25"
				}
				if r.URL.Query().Get("end_date") != end {
					t.Errorf("wrong end date: %s", r.URL.RawQuery)
				}
				// Return forecast values for both hours, including the future one.
				fmt.Fprintf(w, `{"hourly":{"time":[%d,%d],"temperature_2m":[4.5,9.9]}}`, time.Date(2026, 10, 25, 8, 0, 0, 0, loc).Unix(), time.Date(2026, 10, 25, 14, 0, 0, 0, loc).Unix())
			}))
			defer server.Close()
			s := newService()
			s.weatherURL = server.URL
			s.now = func() time.Time { return time.Date(2026, 10, 25, test.hour, test.minute, 0, 0, loc).UTC() }
			report, err := s.weather(context.Background(), testCity(), 7)
			if err != nil {
				t.Fatal(err)
			}
			wantRows := 7
			if test.today {
				wantRows++
			}
			if len(report.Rows) != wantRows {
				t.Fatalf("got %d rows, want %d", len(report.Rows), wantRows)
			}
			last := report.Rows[len(report.Rows)-1]
			if last.Today != test.today {
				t.Fatal("incorrect today marker")
			}
			if !test.today {
				return
			}
			if last.Morning == nil || *last.Morning.Temp != 4.5 {
				t.Fatal("morning not shown at 08:00")
			}
			if last.AfternoonPending == test.afternoon {
				t.Fatal("wrong pending state")
			}
			if test.afternoon {
				if last.Afternoon == nil || *last.Afternoon.Temp != 9.9 {
					t.Fatal("afternoon not shown at 14:00")
				}
			} else {
				if last.Afternoon != nil {
					t.Fatal("future forecast leaked into history")
				}
				if !strings.Contains(rowCells(last)[4], tr("table_02")) {
					t.Fatal("future hour not labelled")
				}
				if temperatureText([]DayRow{last}, 4) != "" {
					t.Fatal("future value copied")
				}
			}
		})
	}
}
