package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testCity() City {
	return City{Name: "Уфа", Lat: "54.7388", Lon: "55.9721", Timezone: "Asia/Yekaterinburg"}
}

func TestPastPeriod(t *testing.T) {
	tests := []struct {
		name, now, zone string
		days            int
		start, end      string
	}{
		{"city ahead of host", "2026-10-24T22:00:00Z", "Asia/Yekaterinburg", 7, "2026-10-18", "2026-10-24"},
		{"city behind host", "2026-01-01T01:00:00Z", "America/New_York", 1, "2025-12-30", "2025-12-30"},
		{"DST fall", "2026-11-03T12:00:00Z", "America/New_York", 7, "2026-10-27", "2026-11-02"},
		{"DST spring", "2026-03-10T12:00:00Z", "America/New_York", 7, "2026-03-03", "2026-03-09"},
		{"leap day", "2024-03-01T12:00:00Z", "Europe/Moscow", 1, "2024-02-29", "2024-02-29"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now, _ := time.Parse(time.RFC3339, tt.now)
			start, end, err := pastPeriod(now, tt.zone, tt.days)
			if err != nil || start.Format(time.DateOnly) != tt.start || end.Format(time.DateOnly) != tt.end {
				t.Fatalf("%s..%s: %v", start, end, err)
			}
			count := 0
			for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
				count++
			}
			if count != tt.days {
				t.Fatalf("got %d days", count)
			}
		})
	}
	for _, days := range []int{-1, 0, 367} {
		if _, _, err := pastPeriod(time.Now(), "UTC", days); err == nil {
			t.Fatalf("accepted %d", days)
		}
	}
}

func TestSearchDisambiguatesAndEncodesQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("name") != "Уфа & район" || r.URL.Query().Get("language") != "ru" {
			t.Errorf("bad query: %s", r.URL.RawQuery)
		}
		fmt.Fprint(w, `{"results":[{"name":"Уфа","latitude":54.7,"longitude":55.9,"timezone":"Asia/Yekaterinburg","admin1":"Башкортостан","country":"Россия"}]}`)
	}))
	defer server.Close()
	s := newService()
	s.geocodeURL = server.URL
	cities, err := s.search(context.Background(), "Уфа & район")
	if err != nil || len(cities) != 1 {
		t.Fatalf("%+v, %v", cities, err)
	}
	if cities[0].label() != "Уфа, Башкортостан, Россия" {
		t.Fatal(cities[0].label())
	}
	if len(filterCities(cities, "БАШКОРТ")) != 1 || len(filterCities(cities, "")) != 1 {
		t.Fatal("local search failed")
	}
	if _, err := s.search(context.Background(), "у"); err == nil {
		t.Fatal("short query accepted")
	}
}

func TestCityFileValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cities.json")
	for _, data := range []string{`[{"name":"A","lat":"0","lon":"0"}]`, `[{"name":"B","lat":-90,"lon":180}]`} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadCities(path); err != nil {
			t.Fatal(err)
		}
	}
	for _, data := range []string{`[]`, `{`, `[{"name":"A","lat":91,"lon":0}]`, `[{"name":"A","lat":"NaN","lon":"0"}]`, `[{"name":"A","lat":0,"lon":0,"timezone":"Invalid/Zone"}]`} {
		os.WriteFile(path, []byte(data), 0600)
		if _, err := loadCities(path); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
	cities, err := loadCities("cities.json")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(cities)
	if err != nil || len(encoded) == 0 {
		t.Fatal("cannot encode cities", err)
	}
}
