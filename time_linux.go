//go:build linux && cgo
// +build linux,cgo

package main

/*
#include <sys/time.h>
#include <time.h>
#include <errno.h>

int set_time_of_day(size_t sec, size_t usec) {
	struct timeval tv;
	tv.tv_sec = (time_t)sec;
	tv.tv_usec = (time_t)usec;

	int err = settimeofday(&tv, NULL);
	if (err) {
		switch (errno) {
			case EPERM:
				return 1;
			case EFAULT:
				return 2;
			case EINVAL:
				return 3;
			default:
				return 4;
		}
	}
	return 0;
}
*/
import "C"

import (
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

const (
	nsecPerUsec = 1000
	usecPerSec  = 1000000
)

var (
	unixEpoch = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
)

func setPlatformTime(t time.Time) error {
	usec := int64(t.Sub(unixEpoch).Nanoseconds() / nsecPerUsec)
	sec := usec / usecPerSec
	usec -= sec * usecPerSec
	errno := C.set_time_of_day(C.size_t(sec), C.size_t(usec))
	switch errno {
	case 0:
		return nil
	case 1:
		return errors.New("insufficient permissions")
	case 2:
		return errors.New("error getting information from user space")
	case 3:
		return errors.New("invalid data")
	default:
		return errors.New("unknown error")
	}
}

// for windows only
func sendWindowsCtrlBreak(pid int) error {
	return nil
}

func cmdSetup(cmd *exec.Cmd) error {
	return nil
}

func execShell(shell string, args []string, env []string) error {

	// Create arbitrary command.
	cmd := exec.Command(shell, args...)
	if len(env) > 0 {
		cmd.Env = env
	}

	cmd.Env = append(cmd.Env, "PS1='${debian_chroot:+($debian_chroot)}\\u@\\h:\\w gohelper[$$]\\$'")

	// Start the command with a pty.
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return err
	}
	// Make sure to close the pty at the end.
	defer func() { _ = ptmx.Close() }() // Best effort.

	if !sysQuiet {
		log.Printf("Info: Linux Shell running, pid %d, path %s, args %v\n", cmd.Process.Pid, shell, args)
	}

	// Handle pty size.
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	go func() {
		for range ch {
			if err := pty.InheritSize(os.Stdin, ptmx); err != nil {
				log.Printf("error resizing pty: %s", err)
			}
		}
	}()
	ch <- syscall.SIGWINCH                        // Initial resize.
	defer func() { signal.Stop(ch); close(ch) }() // Cleanup signals when done.

	// Set stdin in raw mode.
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		panic(err)
	}
	defer func() { _ = term.Restore(int(os.Stdin.Fd()), oldState) }() // Best effort.

	// Copy stdin to the pty and the pty to stdout.
	// NOTE: The goroutine will keep reading until the next keystroke before returning.
	go func() { _, _ = io.Copy(ptmx, os.Stdin) }()

	// fmt.Fprintf(cmd.Stdin, "PS1='${debian_chroot:+($debian_chroot)}\\u@\\h:\\w gohelper[$$]\\$'\n")

	_, _ = io.Copy(os.Stdout, ptmx)

	return nil
}
