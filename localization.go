package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
)

//go:embed locale.json
var defaultLocaleJSON []byte

var defaultText = decodeDefaultLocale()
var textCatalog = defaultText
var formatPlaceholders = regexp.MustCompile(`%(?:\[[0-9]+\])?[-+# 0]*[0-9]*(?:\.[0-9]+)?[a-zA-Z%]`)

func decodeDefaultLocale() map[string]string {
	var values map[string]string
	if err := json.Unmarshal(defaultLocaleJSON, &values); err != nil {
		panic(err)
	}
	return values
}

func readLocale(path string) (map[string]string, error) {
	values := make(map[string]string, len(defaultText))
	for key, value := range defaultText {
		values[key] = value
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return values, nil
	}
	if err != nil {
		return nil, err
	}
	var translated map[string]string
	if err := json.Unmarshal(data, &translated); err != nil {
		return nil, err
	}
	for key, value := range translated {
		original, ok := defaultText[key]
		if !ok {
			continue
		}
		if !slices.Equal(formatPlaceholders.FindAllString(original, -1), formatPlaceholders.FindAllString(value, -1)) {
			return nil, fmt.Errorf(tr("locale_placeholders_error"), key)
		}
		values[key] = value
	}
	return values, nil
}

func loadLocale(dir string) error {
	values, err := readLocale(filepath.Join(dir, "locale.json"))
	if err != nil {
		return err
	}
	textCatalog = values
	return nil
}

func tr(key string, args ...any) string {
	value, ok := textCatalog[key]
	if !ok {
		value = defaultText[key]
	}
	if len(args) > 0 {
		return fmt.Sprintf(value, args...)
	}
	return value
}

func windowTitle() string { return tr("window_title", appName, appVersion) }
