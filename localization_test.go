package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

func TestLocaleOverrideAndFormatValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "locale.json")
	if err := os.WriteFile(path, []byte(`{"gui_windows_08":"Load weather","window_title":"%s %s - Past weather"}`), 0600); err != nil {
		t.Fatal(err)
	}
	values, err := readLocale(path)
	if err != nil || values["gui_windows_08"] != "Load weather" || values["weather_16"] != defaultText["weather_16"] {
		t.Fatal("override or fallback failed", err)
	}
	if err := os.WriteFile(path, []byte(`{"window_title":"%d"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readLocale(path); err == nil {
		t.Fatal("incompatible placeholders accepted")
	}
	if err := os.WriteFile(path, []byte(`{broken`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readLocale(path); err == nil {
		t.Fatal("broken JSON accepted")
	}
}

func TestApplicationTextComesFromCatalog(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if literal, ok := node.(*ast.BasicLit); ok && literal.Kind == token.STRING {
				value, _ := strconv.Unquote(literal.Value)
				for _, r := range value {
					if unicode.Is(unicode.Cyrillic, r) {
						t.Errorf("text outside catalog: %s", fset.Position(literal.Pos()))
						break
					}
				}
			}
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			name, ok := call.Fun.(*ast.Ident)
			if !ok || name.Name != "tr" || len(call.Args) == 0 {
				return true
			}
			key, ok := call.Args[0].(*ast.BasicLit)
			if !ok {
				return true
			}
			value, _ := strconv.Unquote(key.Value)
			if _, found := defaultText[value]; !found {
				t.Errorf("missing key %q in %s", value, path)
			}
			return true
		})
	}
}
