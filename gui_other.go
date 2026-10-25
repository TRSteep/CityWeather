//go:build !windows

package main

import (
	"errors"
	"fmt"
)

func showFatal(text string) { fmt.Println(text) }
func runGUI(_ []City) error {
	return errors.New(tr("gui_other_01"))
}
