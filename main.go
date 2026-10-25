package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	dir := dataDirectory()
	if err := loadLocale(dir); err != nil {
		showFatal(tr("locale_error", err))
		return
	}
	cities, err := loadCities(filepath.Join(dir, "cities.json"))
	if err != nil {
		showFatal(err.Error())
		return
	}
	if err := runGUI(cities); err != nil {
		showFatal(fmt.Sprintf(tr("main_01"), err))
	}
}

func dataDirectory() string {
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		if _, err := os.Stat(filepath.Join(dir, "cities.json")); err == nil {
			return dir
		}
	}
	return "."
}
