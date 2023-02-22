//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

func init() {
	con := windows.Handle(os.Stdin.Fd())

	var mode uint32
	if err := windows.GetConsoleMode(con, &mode); err == nil {
		mode |= windows.ENABLE_EXTENDED_FLAGS
		mode &^= windows.ENABLE_QUICK_EDIT_MODE
		_ = windows.SetConsoleMode(con, mode)
	}
}
