package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

type City struct {
	ID       int         `json:"id,omitempty"`
	Name     string      `json:"name"`
	Lat      json.Number `json:"lat"`
	Lon      json.Number `json:"lon"`
	Timezone string      `json:"timezone,omitempty"`
	Country  string      `json:"country,omitempty"`
	Region   string      `json:"region,omitempty"`
}

func (c City) label() string {
	parts := []string{c.Name}
	if c.Region != "" {
		parts = append(parts, c.Region)
	}
	if c.Country != "" {
		parts = append(parts, c.Country)
	}
	return strings.Join(parts, ", ")
}

func (c City) validate() error {
	lat, e1 := c.Lat.Float64()
	lon, e2 := c.Lon.Float64()
	if strings.TrimSpace(c.Name) == "" || e1 != nil || e2 != nil || math.IsNaN(lat) || math.IsNaN(lon) || math.IsInf(lat, 0) || math.IsInf(lon, 0) || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return fmt.Errorf("некорректное название или координаты города %q", c.Name)
	}
	if c.Timezone != "" {
		if _, err := time.LoadLocation(c.Timezone); err != nil {
			return fmt.Errorf("неизвестный часовой пояс города %q: %s", c.Name, c.Timezone)
		}
	}
	return nil
}

func loadCities(path string) ([]City, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать cities.json: %w", err)
	}
	var cities []City
	if err := json.Unmarshal(data, &cities); err != nil {
		return nil, fmt.Errorf("ошибка cities.json: %w", err)
	}
	if cities == nil {
		cities = []City{}
	}
	for _, c := range cities {
		if err := c.validate(); err != nil {
			return nil, err
		}
	}
	return cities, nil
}

func filterCities(cities []City, query string) []City {
	query = strings.ToLower(strings.TrimSpace(query))
	var result []City
	for _, city := range cities {
		if strings.Contains(strings.ToLower(city.label()), query) {
			result = append(result, city)
		}
	}
	return result
}

type Observation struct {
	Time           string
	Temp, Humidity *float64
	Conditions     string
}
type archiveResponse struct {
	Timezone string `json:"timezone"`
	Hourly   struct {
		Time        []int64    `json:"time"`
		Temperature []*float64 `json:"temperature_2m"`
		Humidity    []*float64 `json:"relative_humidity_2m"`
		Code        []*int     `json:"weather_code"`
	} `json:"hourly"`
}
type DayRow struct {
	Date               string
	Morning, Afternoon *Observation
}
type WeatherReport struct {
	City                 City
	Timezone, Start, End string
	Rows                 []DayRow
}
type service struct {
	client                 *http.Client
	weatherURL, geocodeURL string
	now                    func() time.Time
}

func newService() *service {
	return &service{client: &http.Client{Timeout: 30 * time.Second}, weatherURL: "https://archive-api.open-meteo.com/v1/archive", geocodeURL: "https://geocoding-api.open-meteo.com/v1/search", now: time.Now}
}

func (s *service) getJSON(ctx context.Context, endpoint string, result any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return errors.New("не удалось создать запрос")
	}
	req.Header.Set("User-Agent", "CityWeather/1.0")
	resp, err := s.client.Do(req)
	// Keep messages independent of transport details.
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("не удалось подключиться к сервису; проверьте интернет и повторите запрос")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		switch resp.StatusCode {
		case 401, 403:
			return errors.New("Open-Meteo отклонил запрос; повторите позже")
		case 429:
			return errors.New("превышен лимит запросов сервиса; повторите позже или уменьшите период")
		default:
			return fmt.Errorf("сервис вернул HTTP %d; повторите запрос позже", resp.StatusCode)
		}
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(result); err != nil {
		return errors.New("сервис вернул некорректные или слишком большие данные")
	}
	return nil
}

func (s *service) search(ctx context.Context, query string) ([]City, error) {
	query = strings.TrimSpace(query)
	if len([]rune(query)) < 2 {
		return nil, errors.New("введите не менее двух букв названия города")
	}
	params := url.Values{"name": {query}, "count": {"50"}, "language": {"ru"}, "format": {"json"}}
	var data struct {
		Results []struct {
			ID        int     `json:"id"`
			Name      string  `json:"name"`
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			Timezone  string  `json:"timezone"`
			Country   string  `json:"country"`
			Region    string  `json:"admin1"`
		} `json:"results"`
	}
	if err := s.getJSON(ctx, s.geocodeURL+"?"+params.Encode(), &data); err != nil {
		return nil, err
	}
	cities := make([]City, 0, len(data.Results))
	for _, r := range data.Results {
		c := City{ID: r.ID, Name: r.Name, Lat: json.Number(strconv.FormatFloat(r.Latitude, 'f', -1, 64)), Lon: json.Number(strconv.FormatFloat(r.Longitude, 'f', -1, 64)), Timezone: r.Timezone, Country: r.Country, Region: r.Region}
		if c.validate() == nil {
			cities = append(cities, c)
		}
	}
	return cities, nil
}

func (s *service) fetchArchive(ctx context.Context, city City, zone, start, end string) (archiveResponse, error) {
	params := url.Values{
		"latitude": {string(city.Lat)}, "longitude": {string(city.Lon)},
		"timezone": {zone}, "start_date": {start}, "end_date": {end},
		"hourly":     {"temperature_2m,relative_humidity_2m,weather_code"},
		"timeformat": {"unixtime"}, "temperature_unit": {"celsius"}, "models": {"ecmwf_ifs"},
	}
	var data archiveResponse
	err := s.getJSON(ctx, s.weatherURL+"?"+params.Encode(), &data)
	return data, err
}

func pastPeriod(now time.Time, zone string, days int) (time.Time, time.Time, error) {
	if days < 1 || days > 366 {
		return time.Time{}, time.Time{}, errors.New("укажите целое число дней от 1 до 366")
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("неизвестный часовой пояс: %s", zone)
	}
	local := now.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	return today.AddDate(0, 0, -days), today.AddDate(0, 0, -1), nil
}

func (s *service) weather(ctx context.Context, city City, days int) (WeatherReport, error) {
	var report WeatherReport
	if err := city.validate(); err != nil {
		return report, err
	}
	if days < 1 || days > 366 {
		return report, errors.New("укажите целое число дней от 1 до 366")
	}
	zone := city.Timezone
	if zone == "" {
		yesterday := s.now().UTC().AddDate(0, 0, -1).Format(time.DateOnly)
		metadata, err := s.fetchArchive(ctx, city, "auto", yesterday, yesterday)
		if err != nil {
			return report, err
		}
		zone = metadata.Timezone
	}
	start, end, err := pastPeriod(s.now(), zone, days)
	if err != nil {
		return report, err
	}
	report = WeatherReport{City: city, Timezone: zone, Start: start.Format(time.DateOnly), End: end.Format(time.DateOnly)}
	data, err := s.fetchArchive(ctx, city, zone, report.Start, report.End)
	if err != nil {
		return report, err
	}
	hourly := data.Hourly
	for _, n := range []int{len(hourly.Temperature), len(hourly.Humidity), len(hourly.Code)} {
		if n != 0 && n != len(hourly.Time) {
			return report, errors.New("Open-Meteo вернул несогласованные почасовые данные")
		}
	}
	loc, _ := time.LoadLocation(zone)
	byTime := make(map[string]*Observation)
	for i, epoch := range hourly.Time {
		local := time.Unix(epoch, 0).In(loc)
		if local.Minute() != 0 || local.Second() != 0 || (local.Hour() != 8 && local.Hour() != 14) {
			continue
		}
		o := &Observation{Time: local.Format(time.TimeOnly)}
		if len(hourly.Temperature) > i {
			o.Temp = hourly.Temperature[i]
		}
		if len(hourly.Humidity) > i {
			o.Humidity = hourly.Humidity[i]
		}
		if len(hourly.Code) > i {
			o.Conditions = weatherDescription(hourly.Code[i])
		}
		byTime[local.Format("2006-01-02 15:04")] = o
	}
	for date := start; !date.After(end); date = date.AddDate(0, 0, 1) {
		day := date.Format(time.DateOnly)
		report.Rows = append(report.Rows, DayRow{Date: day, Morning: byTime[day+" 08:00"], Afternoon: byTime[day+" 14:00"]})
	}
	return report, nil
}

// WMO weather interpretation codes returned by Open-Meteo.
func weatherDescription(code *int) string {
	if code == nil {
		return "нет данных"
	}
	switch *code {
	case 0:
		return "Ясно"
	case 1:
		return "Преимущественно ясно"
	case 2:
		return "Переменная облачность"
	case 3:
		return "Пасмурно"
	case 45, 48:
		return "Туман"
	case 51, 53, 55:
		return "Морось"
	case 56, 57:
		return "Переохлаждённая морось"
	case 61, 63, 65:
		return "Дождь"
	case 66, 67:
		return "Переохлаждённый дождь"
	case 71, 73, 75:
		return "Снег"
	case 77:
		return "Снежные зёрна"
	case 80, 81, 82:
		return "Ливень"
	case 85, 86:
		return "Снежный ливень"
	case 95:
		return "Гроза"
	case 96, 99:
		return "Гроза с градом"
	default:
		return fmt.Sprintf("Код WMO: %d", *code)
	}
}

func number(value *float64, suffix string) string {
	if value == nil {
		return "нет данных"
	}
	return fmt.Sprintf("%.1f%s", *value, suffix)
}
func observationCells(o *Observation) []string {
	if o == nil {
		return []string{"нет данных", "нет данных", "нет данных"}
	}
	conditions := o.Conditions
	if conditions == "" {
		conditions = "нет данных"
	}
	return []string{number(o.Temp, " °C"), number(o.Humidity, " %"), conditions}
}
