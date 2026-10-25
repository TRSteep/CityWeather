package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

type preferences struct {
	LastCity *City `json:"last_city,omitempty"`
}

type cityStore struct {
	dir    string
	cities []City
	prefs  preferences
}

func loadPreferences(dir string) (preferences, error) {
	var prefs preferences
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if errors.Is(err, os.ErrNotExist) {
		return prefs, nil
	}
	if err != nil {
		return prefs, err
	}
	if err := json.Unmarshal(data, &prefs); err != nil {
		return preferences{}, err
	}
	if prefs.LastCity != nil {
		if err := prefs.LastCity.validate(); err != nil {
			return preferences{}, err
		}
	}
	return prefs, nil
}

// Write beside the destination and replace only after all bytes are on disk.
func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".cityweather-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func sameCity(a, b City) bool {
	if a.ID != 0 && b.ID != 0 {
		return a.ID == b.ID
	}
	if !strings.EqualFold(strings.TrimSpace(a.Name), strings.TrimSpace(b.Name)) {
		return false
	}
	if a.Country != "" && b.Country != "" && !strings.EqualFold(a.Country, b.Country) {
		return false
	}
	if a.Region != "" && b.Region != "" && !strings.EqualFold(a.Region, b.Region) {
		return false
	}
	alat, e1 := a.Lat.Float64()
	alon, e2 := a.Lon.Float64()
	blat, e3 := b.Lat.Float64()
	blon, e4 := b.Lon.Float64()
	// Older city files have approximate centre coordinates and no GeoNames ID.
	return e1 == nil && e2 == nil && e3 == nil && e4 == nil && math.Abs(alat-blat) < 0.15 && math.Abs(alon-blon) < 0.15
}

func cityIndex(cities []City, city City) int {
	for i, saved := range cities {
		if sameCity(saved, city) {
			return i
		}
	}
	return -1
}

func (s *cityStore) remember(city City) error {
	prefs := preferences{LastCity: &city}
	if err := writeJSON(filepath.Join(s.dir, "settings.json"), prefs); err != nil {
		return fmt.Errorf(tr("storage_01"), err)
	}
	s.prefs = prefs
	return nil
}

func (s *cityStore) add(city City) (bool, error) {
	if cityIndex(s.cities, city) >= 0 {
		return false, nil
	}
	updated := append(append([]City{}, s.cities...), city)
	if err := writeJSON(filepath.Join(s.dir, "cities.json"), updated); err != nil {
		return false, fmt.Errorf(tr("storage_02"), err)
	}
	s.cities = updated
	return true, nil
}

func (s *cityStore) remove(city City) error {
	index := cityIndex(s.cities, city)
	if index < 0 {
		return errors.New(tr("storage_03"))
	}
	updated := append([]City{}, s.cities[:index]...)
	updated = append(updated, s.cities[index+1:]...)
	// Clear the remembered city before removing it, so a failed settings write
	// cannot bring a deleted city back into the selector on the next launch.
	if s.prefs.LastCity != nil && sameCity(*s.prefs.LastCity, city) {
		if err := writeJSON(filepath.Join(s.dir, "settings.json"), preferences{}); err != nil {
			return fmt.Errorf(tr("storage_04"), err)
		}
		s.prefs = preferences{}
	}
	if err := writeJSON(filepath.Join(s.dir, "cities.json"), updated); err != nil {
		return fmt.Errorf(tr("storage_05"), err)
	}
	s.cities = updated
	return nil
}
