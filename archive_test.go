package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestWeatherWithoutKeySelectsExactHoursAndPreservesGaps(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Yekaterinburg")
	epoch := func(day, hour int) int64 { return time.Date(2026, 10, day, hour, 0, 0, 0, loc).Unix() }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		for key, want := range map[string]string{
			"start_date": "2026-10-18", "end_date": "2026-10-24", "timezone": "Asia/Yekaterinburg",
			"latitude": "54.7388", "longitude": "55.9721", "timeformat": "unixtime", "models": "ecmwf_ifs",
			"hourly": "temperature_2m,relative_humidity_2m,weather_code",
		} {
			if q.Get(key) != want {
				t.Errorf("%s=%s", key, q.Get(key))
			}
		}
		if q.Has("key") || q.Has("apikey") || r.Header.Get("Authorization") != "" {
			t.Error("authentication should not be sent")
		}
		fmt.Fprintf(w, `{"timezone":"Asia/Yekaterinburg","hourly":{
   "time":[%d,%d,%d,%d,%d,%d],
   "temperature_2m":[-90,0,12.5,90,null,99],
   "relative_humidity_2m":[null,80,50,null,null,null],
   "weather_code":[null,3,0,null,null,null]}}`, epoch(18, 7), epoch(18, 8), epoch(18, 14), epoch(18, 15), epoch(20, 8), epoch(25, 8))
	}))
	defer server.Close()
	s := newService()
	s.weatherURL = server.URL
	s.now = func() time.Time { return time.Date(2026, 10, 24, 22, 0, 0, 0, time.UTC) }
	report, err := s.weather(context.Background(), testCity(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Rows) != 7 {
		t.Fatalf("rows=%d", len(report.Rows))
	}
	row := report.Rows[0]
	if row.Morning == nil || row.Afternoon == nil || *row.Morning.Temp != 0 || *row.Afternoon.Temp != 12.5 {
		t.Fatalf("wrong hours: %+v", row)
	}
	if row.Morning.Conditions != "Пасмурно" || row.Afternoon.Conditions != "Ясно" {
		t.Fatal("wrong WMO description")
	}
	if report.Rows[1].Morning != nil || report.Rows[1].Afternoon != nil {
		t.Fatal("missing day fabricated")
	}
	if report.Rows[2].Morning.Temp != nil || report.Rows[2].Afternoon != nil {
		t.Fatal("missing hour or null fabricated")
	}
	if report.Rows[6].Date != "2026-10-24" {
		t.Fatal("included today")
	}
	if observationCells(row.Morning)[0] != "0.0 °C" || observationCells(nil)[0] != "нет данных" {
		t.Fatal("zero confused with missing value")
	}
}

func TestWeatherUsesDSTAtEachTimestamp(t *testing.T) {
	loc, _ := time.LoadLocation("America/New_York")
	before := time.Date(2026, 10, 31, 8, 0, 0, 0, loc).Unix()
	after := time.Date(2026, 11, 1, 8, 0, 0, 0, loc).Unix()
	if after-before != 25*3600 {
		t.Fatal("bad DST fixture")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"hourly":{"time":[%d,%d],"temperature_2m":[1,2]}}`, before, after)
	}))
	defer server.Close()
	s := newService()
	s.weatherURL = server.URL
	s.now = func() time.Time { return time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC) }
	city := testCity()
	city.Timezone = "America/New_York"
	report, err := s.weather(context.Background(), city, 2)
	if err != nil {
		t.Fatal(err)
	}
	for i, row := range report.Rows {
		if row.Morning == nil || *row.Morning.Temp != float64(i+1) {
			t.Fatalf("wrong DST mapping: %+v", row)
		}
	}
}

func TestLegacyCityTimezoneLookup(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		want := "Asia/Yekaterinburg"
		if requests == 1 {
			want = "auto"
		}
		if r.URL.Query().Get("timezone") != want {
			t.Errorf("want zone %s", want)
		}
		fmt.Fprint(w, `{"timezone":"Asia/Yekaterinburg","hourly":{"time":[]}}`)
	}))
	defer server.Close()
	s := newService()
	s.weatherURL = server.URL
	city := testCity()
	city.Timezone = ""
	report, err := s.weather(context.Background(), city, 1)
	if err != nil || requests != 2 || report.Timezone != "Asia/Yekaterinburg" {
		t.Fatalf("requests=%d, report=%+v, err=%v", requests, report, err)
	}
}

func TestArchiveErrorsAndCancellation(t *testing.T) {
	for _, status := range []int{400, 401, 403, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
			defer server.Close()
			s := newService()
			s.weatherURL = server.URL
			if _, err := s.weather(context.Background(), testCity(), 1); err == nil {
				t.Fatal("missing HTTP error")
			}
		})
	}
	for _, body := range []string{"not JSON", `{"hourly":{"time":[1,2],"temperature_2m":[3]}}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		s := newService()
		s.weatherURL = server.URL
		if _, err := s.weather(context.Background(), testCity(), 1); err == nil {
			t.Fatal("invalid response accepted")
		}
		server.Close()
		if _, err := s.weather(context.Background(), testCity(), 1); err == nil {
			t.Fatal("missing network error")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := newService().weather(ctx, testCity(), 1); err != context.Canceled {
		t.Fatalf("cancellation: %v", err)
	}
}

// Opt-in smoke test against the public service, excluded from ordinary offline tests.
func TestLiveOpenMeteo(t *testing.T) {
	if os.Getenv("CITYWEATHER_LIVE_TEST") != "1" {
		t.Skip("set CITYWEATHER_LIVE_TEST=1 for public API verification")
	}
	s := newService()
	report, err := s.weather(context.Background(), testCity(), 7)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range report.Rows {
		if (!row.MorningPending && (row.Morning == nil || row.Morning.Temp == nil)) || (!row.AfternoonPending && (row.Afternoon == nil || row.Afternoon.Temp == nil)) {
			t.Fatalf("incomplete real data: %+v", row)
		}
	}
	data, err := json.Marshal(report)
	if err != nil || strings.Contains(string(data), "apikey") {
		t.Fatal("invalid report", err)
	}
	t.Logf("Open-Meteo: %s..%s, %d complete days, last 08:00 %.1f°C / 14:00 %.1f°C", report.Start, report.End, len(report.Rows), *report.Rows[6].Morning.Temp, *report.Rows[6].Afternoon.Temp)
}
