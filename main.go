package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	dir := dataDirectory()
	cities, err := loadCities(filepath.Join(dir, "cities.json"))
	if err != nil {
		showFatal(err.Error())
		return
	}
	if err := runGUI(cities); err != nil {
		showFatal(fmt.Sprintf("Не удалось открыть окно: %v", err))
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
