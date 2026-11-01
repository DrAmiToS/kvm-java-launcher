//go:build !windows

package main

import "syscall"

func hiddenConsoleSysProcAttr() *syscall.SysProcAttr {
	return nil
}

func attachDebugConsole() func() {
	return func() {}
}

func debugConsoleNote() string {
	return ""
}

func configureSoftwareOpenGL(bool) {}

func guiPreflight() error {
	return nil
}
