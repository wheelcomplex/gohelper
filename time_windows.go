//go:build windows && cgo
// +build windows,cgo

package main

/*
#include <windows.h>
#include <stdint.h>

int set_time(uint32_t ticksUpper, uint32_t ticksLower) {
	FILETIME fileTime;
	fileTime.dwLowDateTime  = (DWORD)ticksLower;
	fileTime.dwHighDateTime = (DWORD)ticksUpper;

	SYSTEMTIME sysTime;
	BOOL result = FileTimeToSystemTime(&fileTime, &sysTime);
	if (!result)
		return GetLastError();

	result = SetSystemTime(&sysTime);
	if (!result)
		return GetLastError();

	return 0;
}
*/
import "C"

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"syscall"
	"time"

	prompt "github.com/c-bata/go-prompt"
)

const (
	nsecPerTick        = 100
	windowsEpochOffset = 0x019db1ded53e8000 // ticks[UnixEpoch] - ticks[WindowsEpoch]
)

var (
	unixEpoch = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
)

func setPlatformTime(t time.Time) error {
	// Calculate the number of 100ns ticks since the Windows epoch (Jan 1,
	// 1601). We have to start with the time since the unix epoch, since go's
	// Duration field cannot hold a duration longer than ~290 years.
	nsec := t.Sub(unixEpoch).Nanoseconds()
	ticks := uint64(nsec)/nsecPerTick + windowsEpochOffset

	// Split ticks into upper and lower dword values.
	ticksUpper := uint32(ticks >> 32)
	ticksLower := uint32(ticks & 0xffffffff)

	// Call the C function to set the system time.
	errcode := C.set_time(C.uint32_t(ticksUpper), C.uint32_t(ticksLower))
	switch errcode {
	case 0:
		return nil
	case 19:
		fallthrough
	case 1314:
		return fmt.Errorf("insufficient permissions")
	default:
		return fmt.Errorf("unable to set system time (errcode=%v)", errcode)
	}
}

// https://github.com/iwdgo/sigint-windows/blob/master/signal_windows.go
func sendWindowsCtrlBreak(pid int) error {
	d, e := syscall.LoadDLL("kernel32.dll")
	if e != nil {
		return fmt.Errorf("LoadDLL: %v\n", e)
	}
	p, e := d.FindProc("GenerateConsoleCtrlEvent")
	if e != nil {
		return fmt.Errorf("FindProc: %v\n", e)
	}
	r, _, e := p.Call(syscall.CTRL_BREAK_EVENT, uintptr(pid))
	if r == 0 {
		return fmt.Errorf("GenerateConsoleCtrlEvent: %v\n", e)
	}
	return nil
}

func cmdSetup(cmd *exec.Cmd) error {
	var err error

	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | syscall.CREATE_UNICODE_ENVIRONMENT,
	}

	return err
}

func executor(t string) {
	if t == "cmd.exe" {
		cmd := exec.Command("cmd.exe")
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Run()
	}
	return
}

func completer(t prompt.Document) []prompt.Suggest {
	return []prompt.Suggest{
		{Text: "cmd.exe"},
	}
}

// https://github.com/wheelcomplex/go-prompt/blob/master/_example/exec-command/main.go
func execShell2(shell string, args []string, env []string) error {

	cmd := exec.Command(shell, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if len(env) > 0 {
		cmd.Env = env
	}

	executor := func(t string) {
		cmd.Run()
		return
	}

	if !sysQuiet {
		log.Printf("Info: Windows Shell running, pid %d, path %s, args %v\n", cmd.Process.Pid, shell, args)
	}

	p := prompt.New(
		executor,
		completer,
	)
	p.Run()

	return nil
}

func execShell(shell string, args []string, env []string) error {
	cmd := exec.Command(shell, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if len(env) > 0 {
		cmd.Env = env
	}

	if !sysQuiet {
		log.Printf("Info: Windows Shell running, pid %d, path %s, args %v\n", cmd.Process.Pid, shell, args)
	}

	cmd.Start()

	return cmd.Wait()
}
