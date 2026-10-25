//go:build !windows

package main

import "fmt"

func showFatal(text string) { fmt.Println(text) }
func runGUI(_ []City) error {
	return fmt.Errorf("этот графический интерфейс предназначен для Windows")
}
