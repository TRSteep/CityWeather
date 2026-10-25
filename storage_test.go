package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCityLifecycleAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	s := cityStore{dir: dir, cities: []City{}}
	city := testCity()
	// Remember a requested city even before the weather request succeeds.
	if err := s.remember(city); err != nil {
		t.Fatal(err)
	}
	prefs, err := loadPreferences(dir)
	if err != nil || prefs.LastCity == nil || !sameCity(*prefs.LastCity, city) {
		t.Fatal("last request not persisted", err)
	}
	if len(s.cities) != 0 {
		t.Fatal("remember unexpectedly saved an unconfirmed city")
	}
	if added, err := s.add(city); err != nil || !added {
		t.Fatal("first add", added, err)
	}
	duplicate := city
	duplicate.ID = 123
	duplicate.Lat = "54.74"
	duplicate.Region = "Башкортостан"
	if added, err := s.add(duplicate); err != nil || added {
		t.Fatal("duplicate added", added, err)
	}
	cities, err := loadCities(filepath.Join(dir, "cities.json"))
	if err != nil || len(cities) != 1 {
		t.Fatal("saved cities not loaded", err)
	}
	restarted := cityStore{dir: dir, cities: cities, prefs: prefs}
	if err := restarted.remove(city); err != nil {
		t.Fatal(err)
	}
	cities, err = loadCities(filepath.Join(dir, "cities.json"))
	if err != nil || len(cities) != 0 {
		t.Fatal("last city cannot be removed", err)
	}
	prefs, err = loadPreferences(dir)
	if err != nil || prefs.LastCity != nil {
		t.Fatal("deleted city still remembered", err)
	}
	if _, err := restarted.add(duplicate); err != nil {
		t.Fatal("cannot add after emptying list", err)
	}
}

func TestCityIdentitySeparatesNamesakes(t *testing.T) {
	a := testCity()
	a.ID = 1
	b := a
	b.ID = 2
	if sameCity(a, b) {
		t.Fatal("different GeoNames IDs merged")
	}
	b.ID = 0
	b.Lat = "30"
	if sameCity(a, b) {
		t.Fatal("distant namesakes merged")
	}
	b = a
	b.ID = 0
	b.Country = "Другая страна"
	a.Country = "Россия"
	if sameCity(a, b) {
		t.Fatal("countries ignored")
	}
}

func TestStorageFailuresKeepSavedList(t *testing.T) {
	dir := t.TempDir()
	city := testCity()
	s := cityStore{dir: dir, cities: []City{city}}
	if err := os.Mkdir(filepath.Join(dir, "cities.json"), 0755); err != nil {
		t.Fatal(err)
	}
	other := city
	other.Name = "Другой город"
	if _, err := s.add(other); err == nil || len(s.cities) != 1 {
		t.Fatal("failed add changed list")
	}
	if err := s.remove(city); err == nil || len(s.cities) != 1 {
		t.Fatal("failed delete changed list")
	}
	if err := os.Mkdir(filepath.Join(dir, "settings.json"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := s.remember(city); err == nil || s.prefs.LastCity != nil {
		t.Fatal("failed remember changed preferences")
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, ".cityweather-*.tmp"))
	if err != nil || len(leftovers) != 0 {
		t.Fatal("temporary files leaked", leftovers, err)
	}
}

func TestPreferencesMissingOrCorrupt(t *testing.T) {
	dir := t.TempDir()
	if prefs, err := loadPreferences(dir); err != nil || prefs.LastCity != nil {
		t.Fatal("missing preferences", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPreferences(dir); err == nil {
		t.Fatal("corruption ignored")
	}
}
