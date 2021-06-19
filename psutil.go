//
package main

import (
	"bufio"
	"crypto/md5"
	"fmt"
	"io"
	"io/fs"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	ps "github.com/mitchellh/go-ps"
)

// DevNull is NULL device name
var DevNull = "/dev/null"

func init() {
	if runtime.GOOS == "windows" {
		DevNull = "NUL" // /dev/null in windows
	}
}

type CmdInfo struct {
	Args     []string
	Pid      int
	ExitCode int
	Cmd      *exec.Cmd
	Err      error
}

// signal a pid
func sigToPid(cmdPid int, recvSig os.Signal) error {
	if cmdPid < 10 {
		return fmt.Errorf("sigToPid, can not send sig %d to %d(< 10)", recvSig, cmdPid)
	}
	proc, err := os.FindProcess(cmdPid)
	if err == nil && proc != nil {
		if recvSig == syscall.SIGTERM && runtime.GOOS == "windows" {
			err = sendWindowsCtrlBreak(cmdPid)
			proc.Signal(recvSig)
			if sysDebug {
				if err != nil {
					log.Printf("sigToPid, sendWindowsCtrlBreak, send sig %d to child#%d, %v\n", recvSig, cmdPid, err)
				} else {
					log.Printf("sigToPid, sendWindowsCtrlBreak, send sig %d to child#%d, %v\n", recvSig, cmdPid, err)
				}
			}
		} else {
			err = proc.Signal(recvSig)
			if sysDebug {
				if err != nil {
					log.Printf("sent sig %d to child#%d, %v", recvSig, cmdPid, err)
				} else {
					log.Printf("sent sig %d to child#%d", recvSig, cmdPid)
				}
			}
		}
		if err != nil {
			return fmt.Errorf("sigToPid, sigal %d to child#%d, %v", recvSig, cmdPid, err)
		} else {
			return nil
		}
	}
	return fmt.Errorf("proc(%d) not found to send sigal %d/%v", cmdPid, recvSig, recvSig)
}

// signal a pid and all sub-pid
func sigToPidTree(cmdPid int, recvSig os.Signal) error {
	if cmdPid < 10 {
		return fmt.Errorf("can not tree send sig %d to %d(< 10)", recvSig, cmdPid)
	}

	var allpids []ps.Process

	var err error

	rootproc, perr := ps.FindProcess(cmdPid)
	if perr != nil {
		return fmt.Errorf("sigToPidTree, proc not found, root pid %d, %v", cmdPid, perr)
	}

	if sysDebug {
		log.Printf("sigToPidTree, sending, %v/%d root pid %d, proc %v\n", recvSig, recvSig, cmdPid, rootproc)
	}

	allpids, err = ps.Processes()
	if err != nil {
		return fmt.Errorf("sigToPidTree, sigal %d to child#%d, %v", recvSig, cmdPid, err)
	}
	childs := map[int]ps.Process{}
	for _, v := range allpids {
		if v.PPid() == cmdPid {
			childs[v.Pid()] = v
		} else {
			for _, v := range childs {
				if v.PPid() == v.Pid() {
					childs[v.Pid()] = v
				}
			}
		}
	}

	childs[cmdPid] = rootproc
	if sysDebug {
		log.Printf("sigToPidTree, sending %d, %v to root pid %d: %v\n", recvSig, recvSig, cmdPid, childs)
	}
	var ret error
	for pid := range childs {
		err := sigToPid(pid, recvSig)
		errStr := fmt.Sprintf("sigToPidTree, signal %v/%d, child pid %d: %v\n", recvSig, recvSig, pid, err)
		if sysDebug {
			log.Printf("%s", errStr)
		}
		if err != nil {
			if ret == nil {
				ret = err
			} else {
				// append
				ret = fmt.Errorf(ret.Error() + ", " + errStr)
			}
		}
	}
	return ret
}

func loopExec(execArgs []string, logfile string, cmdChan chan *CmdInfo, workdir string, timeout int, env []string) int {
	// todo: multi-loopExec log to background logger by channel
	if sysDebug {
		log.Printf("loopExec, %v\n", execArgs)
	}

	var err error

	cmdinfo := &CmdInfo{
		Args:     execArgs,
		ExitCode: -100,
		Pid:      -1,
		Err:      err,
	}

	if len(execArgs) == 0 {
		err = fmt.Errorf("loopExec, empty args")
		if sysDebug {
			log.Printf("%s\n", err)
		}
		cmdinfo.Err = err
		cmdChan <- cmdinfo
		return -1
	}
	arg0 := 0
	if runtime.GOOS == "windows" {
		execArgs[arg0] = strings.ToLower(execArgs[arg0])
	}
	if strings.HasPrefix(execArgs[arg0], "-logdst") {
		if len(execArgs) < 3 {
			err = fmt.Errorf("loopExec, no enough args for -logdst, %v", execArgs)
			if sysDebug {
				log.Printf("%s\n", err)
			}
			cmdinfo.Err = err
			cmdChan <- cmdinfo
			return -1
		}
		logfile = execArgs[1]
		arg0 = 2
		log.Printf("Info: loopExec, lazy logging to %s, %v\n", logfile, execArgs[arg0:])
	}

	var cmd *exec.Cmd
	if strings.HasSuffix(execArgs[arg0], ".bat") {
		// run by cmd.exe /c
		batArgs := []string{"/c"}
		batArgs = append(batArgs, execArgs[arg0:]...)
		cmd = exec.Command("cmd.exe", batArgs...)
	} else {
		if len(execArgs) > arg0+1 {
			cmd = exec.Command(execArgs[arg0], execArgs[arg0+1:]...)
		} else {
			cmd = exec.Command(execArgs[arg0])
		}
	}

	cmdSetup(cmd)

	if len(env) > 0 {
		cmd.Env = env
	}
	cmd.Dir = workdir

	cmdinfo.Cmd = cmd

	if sysDebug {
		log.Printf("loopExec, try to run, timeout %d: %v ...\n", timeout, cmd)
	}
	if len(logfile) == 0 {
		logfile = DevNull
	} else {
		if sysDebug {
			log.Printf("loopExec, logging to %s ...\n", logfile)
		}
	}
	logFD, err := os.OpenFile(logfile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		if sysDebug {
			log.Printf("loopExec, log open failed: %v\n", err)
		}
		cmdinfo.Err = err
		cmdChan <- cmdinfo
		return -1
	}
	stdoutIn, _ := cmd.StdoutPipe()
	stderrIn, _ := cmd.StderrPipe()

	var errStdout, errStderr error
	var stdout, stderr io.Writer

	var nullFD *os.File
	if sysQuiet {
		nullFD, err = os.OpenFile(DevNull, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
		if err != nil {
			if sysDebug {
				log.Printf("loopExec, DevNull %s open failed: %v\n", DevNull, err)
			}
			cmdinfo.Err = err
			cmdChan <- cmdinfo
			return -1
		}
		stdout = io.MultiWriter(nullFD, logFD)
		stderr = io.MultiWriter(nullFD, logFD)
	} else {
		stdout = io.MultiWriter(os.Stdout, logFD)
		stderr = io.MultiWriter(os.Stderr, logFD)
	}

	err = cmd.Start()
	if err != nil {
		if sysDebug {
			log.Printf("loopExec, cmd.Start() failed with '%s'\n", err)
		}
		cmdinfo.Err = err
		if nullFD != nil {
			nullFD.Close()
		}
		logFD.Close()
		cmdChan <- cmdinfo
		return -1
	}

	cmdinfo.Pid = cmd.Process.Pid

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		_, errStdout = io.Copy(stdout, stdoutIn)
		wg.Done()
	}()

	go func() {
		_, errStderr = io.Copy(stderr, stderrIn)
		wg.Done()
	}()

	if timeout < 0 {
		timeout = 0
	}

	timeoutNotify := make(chan error, 1)

	if timeout > 0 {
		go func() {
			cmdPid := cmd.Process.Pid
			timer := time.After(time.Duration(timeout) * time.Second)
			if !sysQuiet {
				log.Printf("TIMER: %d, PID %d, %v \n", timeout, cmdPid, cmd)
			}

			<-timer

			err := sigToPidTree(cmdPid, syscall.SIGTERM)
			if err == nil {
				<-time.After(50 * time.Millisecond)
				log.Printf("TIMER: %d, pid %d, %v, SIGTERM by TIMEOUT\n", timeout, cmdPid, cmd)
			} else {
				log.Printf("TIMER: %d, pid %d, %v, SIGKILL by TIMEOUT\n", timeout, cmdPid, cmd)
				if !sysQuiet {
					log.Printf("TIMER: reached, and allready exited: %d, PID %d, %v \n", timeout, cmdPid, cmd)
				}
			}
			sigToPidTree(cmdPid, syscall.SIGKILL)
			timeoutNotify <- fmt.Errorf("timeout %d, SIGTERM/SIGKILL pid %d, %v", timeout, cmdPid, err)
		}()
	}

	if sysDebug {
		log.Printf("loopExec, running pid %d, timeout %d: %v ...\n", cmdinfo.Pid, timeout, cmd)
	}

	cmdNotify := make(chan error, 1)
	go func() {
		cmdNotify <- cmd.Wait()
	}()

	ioNotify := make(chan error, 1)
	go func() {
		var err error
		wg.Wait()
		if errStdout != nil || errStderr != nil {
			err = fmt.Errorf("loopExec, ioCopy, failed to capture stdout or stderr")
		}
		ioNotify <- err
	}()

	go func() {
		var err error
		cmdinfo.Err = nil
		select {
		case err = <-timeoutNotify:
			if err != nil {
				log.Printf("ERROR: TIMEOUT: %v\n", err)
				cmdinfo.Err = err
			}
		case err = <-cmdNotify:
			if err != nil {
				log.Printf("loopExec, cmd.Wait() failed: %v\n", err)
				cmdinfo.Err = err
			}
		case err = <-ioNotify:
			if err != nil {
				if sysDebug {
					log.Printf("%v\n", err)
				}
				// cmdinfo.Err = err
			}
		}
		if nullFD != nil {
			nullFD.Close()
		}
		logFD.Close()

		sigToPidTree(cmdinfo.Pid, syscall.SIGTERM)
		<-time.After(50 * time.Millisecond)
		sigToPidTree(cmdinfo.Pid, syscall.SIGKILL)

		if sysDebug {
			log.Printf("loopExec, %v, exit code %d, %v\n", cmd, cmd.ProcessState.ExitCode(), cmd.ProcessState)
		}
		cmdinfo.ExitCode = cmd.ProcessState.ExitCode()
		cmdChan <- cmdinfo
	}()
	return cmdinfo.Pid
}

func foreExec(execArgs []string, logfile string, workdir string, timeout int, env []string) *CmdInfo {

	cmdChan := make(chan *CmdInfo, 1)

	if sysDebug {
		log.Printf("foreExec, %v\n", execArgs)
	}

	loopExec(execArgs, logfile, cmdChan, workdir, timeout, env)

	return <-cmdChan

}

func ShowExecuteTime(startTS time.Time) {
	log.Printf("EXECUTE TIME: %s\n", time.Since(startTS))
}

// var emptyTime time.Time

// func setTime(host string) (time.Time, error) {
// 	r, err := ntp.Query(host)
// 	if err != nil {
// 		return emptyTime, err
// 	}

// 	err = r.Validate()
// 	if err != nil {
// 		return emptyTime, err
// 	}

// 	t := time.Now().Add(r.ClockOffset)
// 	err = setPlatformTime(t)
// 	if err != nil {
// 		return emptyTime, err
// 	}
// 	return t, err
// }

type fanoutInfo struct {
	Err  error
	Text string
	Path string
}

func fastConvert(list []string, timeout int, toDos bool) error {
	var err error
	if sysDebug {

		if toDos {
			log.Printf("convert to DOS ending (CRLF): %v\n", list)
		} else {
			log.Printf("walk and convert to UNIX ending (LF): %v\n", list)
		}
	}
	fanout := runtime.GOMAXPROCS(-1)
	fanchan := make(chan *fanoutInfo, fanout)

	go func() {
		// feed in background
		jobCount := 0
		for _, match := range list {
			go func(filename string) {
				ending2Dos(filename, toDos, fanchan)
			}(match)
			jobCount++
			if sysDebug {
				log.Printf("one dos2unix/unix2dos(%v) file(%d/%d): %s\n", toDos, jobCount, fanout, match)
			}
		}
	}()

	more := true
	if timeout < 1 {
		timeout = 1800
	}

	timer := time.After(time.Duration(timeout) * time.Second)

	exitCount := 0
	var lasterr error
	for more && exitCount < len(list) {
		var info *fanoutInfo
		select {
		case info = <-fanchan:
			exitCount++
			if sysDebug {
				log.Printf("dos2unix/unix2dos exit(%d/%d): %s, %v\n", exitCount, fanout, info.Path, info.Err)
			}
			if len(info.Text) > 0 {
				log.Printf("Info: %s\n", info.Text)
			}
			if info.Err != nil {
				lasterr = err
			}
		case <-timer:
			more = false
			lasterr = fmt.Errorf("TIMEOUT: fastConvert run more then %d seconds: %v", timeout, list)
		}
	}
	return lasterr
}

// readLines reads a whole file into memory
// and returns a slice of its lines.
func readLines(path string) (lines []string, md5hex string, err error) {

	file, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()

	h := md5.New()
	if _, err := io.Copy(h, file); err != nil {
		return nil, "", err
	}

	md5hex = fmt.Sprintf("%x", h.Sum(nil))

	// re-wind the file
	file.Seek(0, io.SeekStart)

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, md5hex, scanner.Err()
}

// writeLines writes the lines to the given file, trim white space at end of line, limit empty line to one continue.
func writeLines(lines []string, path string, toDos bool) (md5hex string, err error) {
	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	h := md5.New()

	mw := bufio.NewWriter(h)

	w := bufio.NewWriter(file)
	emptyCount := 0
	for _, line := range lines {
		// buffered , io calling is not a issue
		line = strings.TrimRightFunc(line, unicode.IsSpace)
		if len(line) == 0 {
			emptyCount++
		} else {
			emptyCount = 0
		}
		if emptyCount > 1 {
			log.Printf("SKIP EMPTY LINE: %d\n", emptyCount)
			continue
		}
		fmt.Fprint(w, line)
		fmt.Fprint(mw, line)
		if toDos {
			fmt.Fprint(w, "\r\n")
			fmt.Fprint(mw, "\r\n")
		} else {
			fmt.Fprint(w, "\n")
			fmt.Fprint(mw, "\n")
		}
	}
	mw.Flush()

	err = w.Flush()
	if err != nil {
		return "", err
	}

	md5hex = fmt.Sprintf("%x", h.Sum(nil))

	return
}

// convert to dos ending, or to unix
func ending2Dos(path string, toDOS bool, fanchan chan *fanoutInfo) {
	info := &fanoutInfo{
		Err:  nil,
		Path: path,
		Text: "",
	}
	if len(path) == 0 {
		fanchan <- info
		return
	}
	lines, srcMd5, err := readLines(path)
	if err != nil {
		info.Err = err
		fanchan <- info
		return
	}
	dstMd5, err := writeLines(lines, path, toDOS)
	if err != nil {
		info.Err = err
		fanchan <- info
		return
	}
	if srcMd5 != dstMd5 {
		info.Text = fmt.Sprintf("%s: %s => %s", path, srcMd5, dstMd5)
	}
	fanchan <- info
}

func walkByFilter(walkdir, walkregex, walkpattern string, norecursive bool) (out []string, err error) {
	out = []string{}
	if sysDebug {
		log.Printf("walkByFilter: input walking directory: %s\n", walkdir)
	}
	walkdir, err = filepath.Abs(walkdir)
	if err != nil {
		err = fmt.Errorf("invalid walk directory: %s, %v", walkdir, err)
		return
	} else {
		if sysDebug {
			log.Printf("walkByFilter: walking directory: %s\n", walkdir)
		}
	}
	walkbase := filepath.Base(walkdir)

	var re *regexp.Regexp

	if len(walkregex) > 0 {
		re, err = regexp.Compile(walkregex)
		if err != nil {
			err = fmt.Errorf("invalid walk regex: %s, %v", walkregex, err)
			return
		} else {
			if sysDebug {
				log.Printf("Info: walking regex: %s\n", walkregex)
			}
		}
	}
	if len(walkpattern) > 0 {
		if runtime.GOOS == "windows" {
			walkpattern = strings.ToLower(walkpattern)
		}
		if sysDebug {
			log.Printf("Info: walking pattern: %s\n", walkpattern)
		}
	}

	debugcount := 0

	err = filepath.Walk(walkdir, func(match string, info fs.FileInfo, err error) error {
		if sysDebug {
			log.Printf("walking got file or dir: %q\n", match)
		}
		if err != nil {
			err = fmt.Errorf("prevent panic by handling failure accessing a path %q: %v", match, err)
			return err
		}
		if info.IsDir() {
			if norecursive && walkbase != info.Name() {
				if sysDebug {
					log.Printf("skipped dir: %+v \n", info.Name())
				}
				return filepath.SkipDir
			} else {
				if sysDebug {
					log.Printf("walk into dir: %+v \n", info.Name())
				}
				return nil
			}

		}
		matched, err := twoMatch(match, walkpattern, re)
		if !matched {
			return nil
		}
		if sysDebug {
			if debugcount < 20 && sysDebug {
				log.Printf("walk, visited file or dir: %q\n", match)
			}
		}
		out = append(out, match)
		debugcount++
		return nil
	})
	if err != nil {
		err = fmt.Errorf("error walking the path %q: %v", walkdir, err)
	}
	return
}

func twoMatch(match, walkpattern string, re *regexp.Regexp) (matched bool, err error) {
	matched = true

	if runtime.GOOS == "windows" {
		match = filepath.Base(strings.ToLower(match))
	} else {
		match = filepath.Base(match)
	}

	if len(walkpattern) > 0 || re != nil {
		var err error
		if len(walkpattern) > 0 {
			matched, err = filepath.Match(walkpattern, match)
			if err != nil {
				err = fmt.Errorf("error when file match %s, %q: %v", walkpattern, match, err)
				return false, err
			}
			if !matched {
				if sysDebug {
					log.Printf("skipping a file mismatch pattern: %s, %s, %+v \n", walkpattern, match, match)
				}
			} else {
				if sysDebug {
					log.Printf("a file match pattern: %s, %s, %+v \n", walkpattern, match, match)
				}
			}
		}
		if !matched && re != nil {
			if runtime.GOOS == "windows" {
				matched = re.MatchString(strings.ToLower(match))
			} else {
				matched = re.MatchString(match)
			}
			if !matched {
				if sysDebug {
					log.Printf("skipping a file mismatch regex: %s\n", match)
				}
			} else {
				if sysDebug {
					log.Printf("a file match regex: %s\n", match)
				}
			}
		}
	}
	if !matched {
		return false, nil
	}
	if sysDebug {
		log.Printf("twoMatch: %s\n", match)
	}
	return true, nil
}

func clearProc(oldProcList map[int]string, whitelist map[string]bool) error {

	if len(oldProcList) == 0 {
		return nil
	}

	var err error
	var pidSnapshot []ps.Process
	termList := []ps.Process{}
	pidSnapshot, err = ps.Processes()
	if err != nil {
		return fmt.Errorf("take process snapshot failed: %v", err)
	}
	for _, v := range pidSnapshot {
		livename := v.Executable()
		if runtime.GOOS == "windows" {
			livename = strings.ToLower(v.Executable())
		}
		if _, ok := whitelist[livename]; ok {
			if !sysQuiet {
				log.Printf("WARNING: clear proc, white list skipped: %d, %s\n", v.Pid(), livename)
			}
			continue
		}

		execname, ok := oldProcList[v.Pid()]
		if runtime.GOOS == "windows" {
			execname = strings.ToLower(execname)
		}
		if !ok || (execname != livename) {
			// not a old pid or pid re-used
			err = sigToPidTree(v.Pid(), syscall.SIGTERM)
			if err == nil {
				log.Printf("WARNING: Clear/SIGTERM DONE, %d, %s/%s\n", v.Pid(), v.Executable(), execname)
				termList = append(termList, v)
			} else {
				log.Printf("WARNING: Clear/SIGTERM FAILED, %d, %s/%s, %v\n", v.Pid(), v.Executable(), execname, err)
			}
		} else {
			// if sysDebug {
			// 	log.Printf("Clear/SIGTERM %d, %s/%s, old process, skipped\n", v.Pid(), v.Executable(), execname)
			// }
		}
	}
	<-time.After(100 * time.Millisecond)
	for _, v := range termList {
		err = sigToPidTree(v.Pid(), syscall.SIGKILL)
		if err == nil {
			log.Printf("WARNING: Clear/SIGKILL DONE, %d, %s\n", v.Pid(), v.Executable())
		} else if sysDebug {
			log.Printf("Clear/SIGKILL FAILED, %d, %s, %v\n", v.Pid(), v.Executable(), err)
		}
	}
	return nil
}

func sysEnvMerge(oldEnv, newEnv []string) ([]string, error) {
	var err error
	mergeEnv := []string{}

	kvEnv := map[string]string{}
	for _, v := range oldEnv {
		vArr := strings.Split(v, "=")
		envKey := ""
		envVal := ""
		for _, ev := range vArr {
			ev = strings.TrimSpace(ev)
			if len(ev) == 0 {
				continue
			} else if len(envKey) == 0 {
				if runtime.GOOS == "windows" {
					envKey = strings.ToUpper(ev)
				} else {
					envKey = ev
				}
			} else if len(envVal) == 0 {
				envVal = ev
			}
		}
		// duplicate key will be overwrite
		if len(envKey) > 0 {
			kvEnv[envKey] = envVal
		}
	}
	for lineNum, line := range newEnv {
		line = strings.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if line[0] == '#' {
			continue
		}
		if runtime.GOOS == "windows" {
			line = strings.ToUpper(line)
		}
		// format: <+++-^>|<key>=<val>
		// +|CI_TIMEOUT=1200
		lineArr := strings.Split(line, "|")
		if len(lineArr) < 2 {
			err = fmt.Errorf("invalid format from env config(%d): %s, : missing", lineNum, line)
			return nil, err
		}
		op := lineArr[0]
		if len(op) == 0 {
			err = fmt.Errorf("invalid format from env config(%d): %s, OP missing", lineNum, line)
			return nil, err
		}
		kv := lineArr[1]
		enArr := strings.Split(kv, "=")
		envKey := ""
		envVal := ""
		if len(enArr) > 0 {
			envKey = enArr[0]
		}
		if len(enArr) > 1 {
			envVal = enArr[1]
		}
		if len(envKey) == 0 {
			err = fmt.Errorf("invalid format from env config(%d): %s, key missing", lineNum, line)
			return nil, err
		}
		sp := ":"
		if runtime.GOOS == "windows" {
			sp = ";"
		}
		val := ""
		ok := false
		if runtime.GOOS == "windows" {
			val, ok = kvEnv[strings.ToUpper(envKey)]
		} else {
			val, ok = kvEnv[envKey]
		}
		if op == "+++" {
			// append to head
			if ok {
				// existed key
				kvEnv[envKey] = envVal + sp + val
			} else {
				kvEnv[envKey] = envVal
			}
			if sysDebug {
				if ok {
					log.Printf("sysEnvMerge, head add env: %s = %s\n", envKey, envVal)
				} else {
					log.Printf("sysEnvMerge, new/ head add env: %s = %s\n", envKey, envVal)
				}
			}
		} else if op == "++" {
			// append to tail
			if ok {
				// existed key
				kvEnv[envKey] = val + sp + envVal
			} else {
				kvEnv[envKey] = envVal
			}
			if sysDebug {
				if ok {
					log.Printf("sysEnvMerge, tail add env: %s = %s\n", envKey, envVal)
				} else {
					log.Printf("sysEnvMerge, new/ tail add env: %s = %s\n", envKey, envVal)
				}
			}
		} else if op == "+" {
			// optional add
			if !ok {
				// not existed key
				kvEnv[envKey] = envVal
			}
			if sysDebug {
				if ok {
					log.Printf("sysEnvMerge, exist/ not optional add env: %s = %s\n", envKey, envVal)
				} else {
					log.Printf("sysEnvMerge, optional add env: %s = %s\n", envKey, envVal)
				}
			}
		} else if op == "-" {
			// remove
			if ok {
				// existed key
				// remove is set to empty
				kvEnv[envKey] = ""
			}
			if sysDebug {
				if ok {
					log.Printf("sysEnvMerge, remove env: %s = %s\n", envKey, envVal)
				} else {
					log.Printf("sysEnvMerge, not exist/remove env: %s = %s\n", envKey, envVal)
				}
			}
		} else {
			// ^
			// force add
			kvEnv[envKey] = envVal
			if sysDebug {
				if ok {
					log.Printf("sysEnvMerge, overwrite env: %s = %s\n", envKey, envVal)
				} else {
					log.Printf("sysEnvMerge, new env: %s = %s\n", envKey, envVal)
				}
			}
		}
	}
	for k, v := range kvEnv {
		mergeEnv = append(mergeEnv, k+"="+v)
	}
	return mergeEnv, nil
}

func cleanArgString(arg string) string {
	arg = strings.Trim(arg, "'\"")
	arg = strings.ReplaceAll(arg, "+", " ")
	return arg
}

func tailOut(w io.Writer, text string) error {
	_, err := w.Write([]byte(text))
	if err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		_, err = w.Write([]byte("\r\n"))
	} else {
		_, err = w.Write([]byte("\n"))
	}
	return err
}

type tailInfo struct {
	n   int
	err error
}

// sendto, 0 for stdout, 1 for stderr, 2 for logger
func tailffSearch(searchs []string, tailN, tailA, tailB, tailC int, sendto int, logger *log.Logger, ignoreCase bool, inChan chan string) <-chan *tailInfo {

	info := &tailInfo{}
	infochan := make(chan *tailInfo, 2)

	go func() {

		defer func(info *tailInfo) {
			// double out
			infochan <- info
			infochan <- info
		}(info)

		if sendto == 2 && logger == nil {
			info.n = 0
			info.err = fmt.Errorf("unable send to logger while logger == nil")
			return
		}

		if tailC > 0 {
			tailA = tailC
			tailB = tailC
		}

		if tailN <= 0 {
			tailN = math.MaxInt32
		}

		if sysDebug {
			log.Printf("DEBUG: tailffSearch, TailABC %d %d %d\n", tailA, tailB, tailC)
			log.Printf("DEBUG: tailffSearch, searchs %v\n", searchs)
		}

		searchLines := map[int]string{}
		searchCount := -1
		deletePtr := 0
		needA := 0
		foundA := ""
		foundPtr := 0
		lastAppendPtr := 0

		outCount := 0

		text := ""
		textOk := true

		stdout := bufio.NewWriter(os.Stdout)
		defer stdout.Flush()

		stderr := bufio.NewWriter(os.Stderr)
		defer stderr.Flush()

		var w io.Writer
		if sendto == 0 {
			w = stdout
		} else if sendto == 1 {
			w = stderr
		} else if sendto == 2 {
			w = logger.Writer()
		}

		var err error
		for {
			if outCount >= tailN {
				info.n = outCount
				info.err = nil
				return
			}
			text, textOk = <-inChan
			if !textOk {
				info.n = outCount
				info.err = nil
				return
			}
			if len(searchs) == 0 {
				// direct write
				err = tailOut(w, text)
				if err != nil {
					info.n = outCount
					info.err = err
					return
				}
				outCount++
			} else {
				searchCount++
				searchLines[searchCount] = text
				found := false
				var text string
				if ignoreCase {
					text = strings.ToLower(searchLines[searchCount])
				} else {
					text = searchLines[searchCount]
				}
				for _, v := range searchs {
					// if sysDebug {
					// 	log.Printf("search %s, %s\n", v, text)
					// }
					if strings.Index(text, v) != -1 {
						// found
						if tailB > 0 && !found {
							// first tailB only
							revPtr := searchCount - tailB - 1
							if revPtr < 0 {
								revPtr = 0
							}
							// if sysDebug {
							// 	log.Printf("found %s, TailB: %d, %d - %d\n", v, tailB, revPtr, searchCount)
							// }
							err = tailOut(w, fmt.Sprintf(">>>>>>>>> %s:%d, %d - %d >>>>>>>>>", v, tailB, revPtr+1, searchCount+1))
							if err != nil {
								info.n = outCount
								info.err = err
								return
							}
							for ; revPtr < searchCount; revPtr++ {
								if revPtr <= lastAppendPtr {
									continue
								}
								err = tailOut(w, fmt.Sprintf("%d: %s", revPtr+1, searchLines[revPtr]))
								if err != nil {
									info.n = outCount
									info.err = err
									return
								}
								outCount++
								lastAppendPtr = revPtr
								// if sysDebug {
								// 	log.Printf("found %s, TailB: %d, %d - %d: %s\n", v, tailB, revPtr, searchCount, searchLines[revPtr])
								// }
							}
						}
						if tailA > 0 {
							// maybe overwite by next found
							needA = tailA + 1
							foundA = v
							// if sysDebug {
							// 	log.Printf("found %s, tailA: %d, %d - %d\n", v, tailA, searchCount+1, searchCount+needA+1)
							// }
							if found {
								if sysDebug {
									log.Printf("more found %s\n", v)
								}
							}
						}
						foundPtr = searchCount
						found = true
					}
				}
				if found {
					err = tailOut(w, fmt.Sprintf("%d: %s", searchCount+1, searchLines[searchCount]))
					if err != nil {
						info.n = outCount
						info.err = err
						return
					}
					outCount++
					// if sysDebug {
					// 	log.Printf("tail search found(%d): %s\n", searchCount+1, searchLines[searchCount])
					// }
					lastAppendPtr = searchCount
				}
				if needA > 0 {
					needA--
					if !found {
						err = tailOut(w, fmt.Sprintf("%d: %s", searchCount+1, searchLines[searchCount]))
						if err != nil {
							info.n = outCount
							info.err = err
							return
						}
						outCount++
						lastAppendPtr = searchCount
					}
					// if sysDebug {
					// 	log.Printf("tailA: %d, %d - %d, %s\n", tailA, needA, searchCount, searchLines[searchCount])
					// }
					if needA == 0 {
						err = tailOut(w, fmt.Sprintf("<<<<<<<<< %s:%d, %d - %d <<<<<<<<<", foundA, tailA, foundPtr+1, foundPtr+tailA+1))
						if err != nil {
							info.n = outCount
							info.err = err
							return
						}
					}
				}
				if len(searchLines) > tailB {
					// clear useless lines
					endptr := len(searchLines) - tailB
					for ; deletePtr < endptr; deletePtr++ {
						// safe to delete not exist element
						// if sysDebug {
						// 	log.Printf("searchLines: deleted %d, %s\n", deletePtr, searchLines[deletePtr])
						// }
						delete(searchLines, deletePtr)
					}
				}
			}
		}
	}()
	return infochan
}

// Average size of line
var LineAvgSize int64 = 256

// sendto, 0 for stdout, 1 for stderr, 2 for logger
func tailFile(logFile string, searchs []string, tailN, tailA, tailB, tailC int, sendto int, logger *log.Logger, ignoreCase bool) (int, error) {
	var err error
	if sendto == 2 && logger == nil {
		return 0, fmt.Errorf("unable send to logger while logger == nil")
	}
	if tailN <= 0 {
		tailN = math.MaxInt32
	}

	if tailC > 0 {
		tailA = tailC
		tailB = tailC
	}

	if sysDebug {
		log.Printf("DEBUG: tailFile, TailABC %d %d %d\n", tailA, tailB, tailC)
		log.Printf("DEBUG: tailFile, searchs %v\n", searchs)
	}

	stat, err := os.Stat(logFile)
	if err != nil {
		return 0, err
	}

	fd, err := os.Open(logFile)
	if err != nil {
		return 0, err
	}
	defer fd.Close()

	filesize := stat.Size()

	var start int64

	if len(searchs) == 0 {
		start = filesize - (int64(tailN) * LineAvgSize)
		if start < 0 {
			start = 0
		}
	} else {
		// search from start
		start = 0
		tailN = math.MaxInt32
	}

	inChan := make(chan string, 128)

	infoChan := tailffSearch(searchs, tailN, tailA, tailB, tailC, sendto, logger, ignoreCase, inChan)

	var info *tailInfo

	for {

		if sysDebug {
			log.Printf("DEBUG: try to seek/start from %d, file size %d\n", start, filesize)
		}

		// seek from start
		_, err = fd.Seek(start, io.SeekStart)
		if err != nil {
			break
		}

		scanner := bufio.NewScanner(fd)

		for scanner.Scan() {
			inChan <- scanner.Text()
		}
		err = scanner.Err()
		if err != nil {
			break
		}
		if start <= 0 {
			if sysDebug {
				log.Printf("DEBUG: tailFile, not more to read.\n")
			}
			break
		}
		start -= LineAvgSize
		select {
		case info = <-infoChan:
			break
		default:
		}
	}
	close(inChan)
	info = <-infoChan
	if sysDebug {
		log.Printf("DEBUG: tailffSearch return: %d/%d, %v\n", tailN, info.n, info.err)
	}
	return info.n, err
}

func argsDoubleMarkParser(inputArr []string, mark string) (tasks map[string][]string, taskOrder []string, leading []string) {
	if sysDebug {
		log.Printf("argsDoubleMarkParser, parsing: %v\n", inputArr)
	}
	tasks = make(map[string][]string, 1) // key: file path, value: real args

	leading = []string{}

	argArr := []string{}
	argKey := ""

	argNow := false
	taskOrder = []string{}

	if len(mark) < 2 {
		log.Printf("ERROR: argsDoubleMarkParser, invalid mark, miniual mark lenght is 2: %s\n", mark)
		return
	}

	for _, arg := range inputArr {
		arg = cleanArgString(arg)
		if arg == mark {
			argNow = true
			if len(argArr) > 0 {
				argKey = strings.TrimSpace(argKey) + "_" + fmt.Sprintf("%d", len(taskOrder))
				tasks[argKey] = argArr

				taskOrder = append(taskOrder, argKey)

				argArr = []string{}
				argKey = ""
			}
			continue
		} else if argNow {
			argArr = append(argArr, arg)
			argKey = argKey + "-" + arg
		} else {
			leading = append(leading, arg)
			if sysDebug {
				log.Printf("argsDoubleMarkParser, leading args: %s\n", arg)
			}
		}
	}
	if len(argArr) > 0 {
		argKey = strings.TrimSpace(argKey) + "_" + fmt.Sprintf("%d", len(taskOrder))
		tasks[argKey] = argArr
		taskOrder = append(taskOrder, argKey)
	}
	return
}
