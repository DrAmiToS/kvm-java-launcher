//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/go-gl/glfw/v3.4/glfw"
)

const attachParentProcess = ^uintptr(0)

var (
	kernel32        = syscall.NewLazyDLL("kernel32.dll")
	procAttach      = kernel32.NewProc("AttachConsole")
	procAlloc       = kernel32.NewProc("AllocConsole")
	procFree        = kernel32.NewProc("FreeConsole")
	procGetStd      = kernel32.NewProc("GetStdHandle")
	procSetConsole  = kernel32.NewProc("SetConsoleTitleW")
	procSetStd      = kernel32.NewProc("SetStdHandle")
	consoleAttached bool
)

func hiddenConsoleSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

func attachDebugConsole() func() {
	if r, _, _ := procAttach.Call(attachParentProcess); r == 0 {
		if r, _, _ := procAlloc.Call(); r != 0 {
			consoleAttached = true
			if title, err := syscall.UTF16PtrFromString("KVM Java Launcher Debug"); err == nil {
				procSetConsole.Call(uintptr(unsafe.Pointer(title)))
			}
		}
	}
	bindConsoleStream(syscall.STD_OUTPUT_HANDLE, &os.Stdout)
	bindConsoleStream(syscall.STD_ERROR_HANDLE, &os.Stderr)
	bindConsoleStream(syscall.STD_INPUT_HANDLE, &os.Stdin)
	return func() {
		if consoleAttached {
			procFree.Call()
		}
	}
}

func debugConsoleNote() string {
	return "Note: this is a Windows GUI-subsystem executable; cmd.exe does not wait for it. For reliable debug output run KVM-Java-Launcher-debug.exe."
}

func configureSoftwareOpenGL(debug bool) {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	dir := filepath.Dir(exe)
	if _, err := os.Stat(filepath.Join(dir, "opengl32.dll")); err != nil {
		return
	}
	setDefaultEnv("GALLIUM_DRIVER", "llvmpipe")
	setDefaultEnv("LIBGL_ALWAYS_SOFTWARE", "1")
	if debug {
		fmt.Println("Software OpenGL: local opengl32.dll found, using Mesa llvmpipe CPU renderer.")
	}
}

func guiPreflight() error {
	runtime.LockOSThread()
	if err := glfw.Init(); err != nil {
		return err
	}
	defer glfw.Terminate()
	glfw.WindowHint(glfw.Visible, glfw.False)
	glfw.WindowHint(glfw.ContextVersionMajor, 2)
	glfw.WindowHint(glfw.ContextVersionMinor, 1)
	win, err := glfw.CreateWindow(1, 1, "KVM Java Launcher OpenGL check", nil, nil)
	if err != nil {
		return err
	}
	win.Destroy()
	return nil
}

func setDefaultEnv(key, value string) {
	if os.Getenv(key) == "" {
		_ = os.Setenv(key, value)
	}
}

func bindConsoleStream(stdHandle int, target **os.File) {
	handle, _, _ := procGetStd.Call(uintptr(uint32(stdHandle)))
	if handle == 0 || handle == ^uintptr(0) {
		return
	}
	switch stdHandle {
	case syscall.STD_OUTPUT_HANDLE:
		*target = os.NewFile(handle, "CONOUT$")
	case syscall.STD_ERROR_HANDLE:
		*target = os.NewFile(handle, "CONERR$")
	case syscall.STD_INPUT_HANDLE:
		*target = os.NewFile(handle, "CONIN$")
	}
	procSetStd.Call(uintptr(uint32(stdHandle)), handle)
}
