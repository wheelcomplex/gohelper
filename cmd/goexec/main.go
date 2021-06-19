// A Go util to keep a executable running

// Note: hotkey works on windows only

// thanks:
// https://github.com/wheelcomplex/go-cookbook/tree/master/advanced-exec
// https://blog.kowalczyk.info/article/wOYk/advanced-command-execution-in-go-with-osexec.html

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	ps "github.com/mitchellh/go-ps"
	gt "github.com/wheelcomplex/gotail"
)

var sysDebug = false
var sysQuiet = false
var sysEnv []string

// ntp server from https://dns.iui.im/ntp/
var defaultNTPServers = "time.windows.com,time.google.com,time.apple.com,time.pool.aliyun.com,ntp.ntsc.ac.cn,cn.ntp.org.cn,pool.ntp.org,time.cloudflare.com"

func main() {

	var err error

	var (
		shell            = ""
		crlf             = ""
		lf               = ""
		ignoreCase       = false
		clear            = false
		dos2unix         = false
		unix2dos         = false
		fileList         = ""
		toDaemon         = false
		timeout          = 0
		workDir          = "."
		loadEnvFile      = ""
		preExecFile      = ""
		preTimeout       = 30
		helpers          = ""
		helperDelay      = 0
		taskDelay        = 0
		startDelay       = 0
		logFile          = ""
		logtail          = 30
		tailA            = 0
		tailB            = 0
		tailC            = 0
		tailsearch       = ""
		tailfile         = ""
		tailff           = false
		errtail          = false
		execArgs         = []string{}
		postExecFile     = ""
		postTimeout      = 30
		threads          = -1
		fanout           = 0
		walkexec         = ""
		walkfile         = ""
		norecursive      = false
		walkregex        = ""
		walkpattern      = ""
		exitOnFanError   = false
		errorOnEmptyWalk = false
		runLoop          = false
		runOnce          = false
		synctime         = false
		ntpServers       = defaultNTPServers
		whitelist        = ""
		help             = false
	)

	// helpers = "D:\\ci\tools\\procdump.exe -accepteula -t burnFlashGui.exe d:\\ci\coredumps\.dump"

	flag.StringVar(&shell, "shell", "", "shell command with envs")
	flag.BoolVar(&ignoreCase, "ignoreCase", false, "ignore case in tail search")
	flag.StringVar(&helpers, "helpers", "", "command for auto procdump")
	flag.StringVar(&crlf, "crlf", "", "options shortcut for convert file ending to dos on directory")
	flag.StringVar(&lf, "lf", "", "options shortcut for convert file ending to unix on directory")
	flag.IntVar(&helperDelay, "helperDelay", 0, "delay N seconds after task launch")
	flag.IntVar(&taskDelay, "taskDelay", 0, "delay N seconds between task launch")
	flag.IntVar(&startDelay, "startDelay", 0, "delay N seconds before main task loop")
	flag.IntVar(&logtail, "tail", 0, "tail N lines of logfile on exit")
	flag.BoolVar(&errtail, "errtail", false, "only tail when task failed")
	flag.StringVar(&tailfile, "tailfile", "", "tail this file")
	flag.StringVar(&tailsearch, "tailsearch", "", "search string in tail text out put")
	flag.IntVar(&tailA, "tailA", 0, "tail N lines after tailsearch match, like grep -A")
	flag.IntVar(&tailB, "tailB", 0, "tail N lines before tailsearch match, like grep -B")
	flag.IntVar(&tailC, "tailC", 0, "tail N lines before and after tailsearch match, like grep -C")
	flag.BoolVar(&tailff, "tff", false, "tail -Ff this file in background")
	flag.BoolVar(&clear, "clear", false, "kill all new process which are created after start")
	flag.StringVar(&whitelist, "whitelist", "", "white list file for -clear")
	flag.BoolVar(&sysQuiet, "quiet", false, "do not show stdout/stderr from child")
	flag.BoolVar(&dos2unix, "dos2unix", false, "convert line ending ofr file in this directory to unix, filter by walkpattern/walkregex")
	flag.BoolVar(&unix2dos, "unix2dos", false, "convert line ending ofr file in this directory to dos, filter by walkpattern/walkregex")
	flag.StringVar(&fileList, "files", "", "file to be convert")
	flag.BoolVar(&toDaemon, "daemon", false, "run into daemon, default not")
	flag.IntVar(&timeout, "timeout", 0, "timeout to kill, will disable loop running, default disabled")
	flag.StringVar(&logFile, "logfile", "", "file to write log")
	flag.StringVar(&workDir, "workdir", ".", "directory to execute")
	flag.StringVar(&preExecFile, "pre", "", "file to execute before main script, ")
	flag.StringVar(&postExecFile, "post", "", "file to execute after main script")
	flag.IntVar(&preTimeout, "preTimeout", 30, "timeout for pre-script ")
	flag.IntVar(&postTimeout, "postTimeout", 30, "timeout for post-script")
	flag.IntVar(&threads, "threads", -1, "number of threads to run")
	flag.IntVar(&fanout, "fanout", 0, "number of fanout to run template jobs")
	flag.StringVar(&walkexec, "walkexec", "", "directory for walk(list file)")
	flag.StringVar(&walkfile, "walkfile", "", "it is a list file for walkexec")
	flag.StringVar(&walkregex, "walkregex", "", "regex(.*\\.bat) for matching file in dir walking(list file)")
	flag.StringVar(&walkpattern, "walkpattern", "", "file pattern (*.bat) for matching file in dir walking(list file)")
	flag.StringVar(&ntpServers, "ntpserver", defaultNTPServers, "ntp server for time sync")
	flag.StringVar(&loadEnvFile, "envfile", "", "load env from file, -: for remove, +: for add if not exist, =: for overwrite")
	flag.BoolVar(&exitOnFanError, "exitOnError", false, "exit when one task failed in fanout")
	flag.BoolVar(&errorOnEmptyWalk, "errorOnEmptyWalk", false, "exit with error code when nothing to walk")
	flag.BoolVar(&sysDebug, "debug", false, "enable debug logging")
	flag.BoolVar(&runLoop, "loop", false, "continue to run in loop")
	flag.BoolVar(&runOnce, "once", true, "run once(default)")
	flag.BoolVar(&synctime, "synctime", false, "sync time with ntp server")
	flag.BoolVar(&norecursive, "norecursive", false, "do not walk into sub-dir")
	flag.BoolVar(&help, "h", false, "show help")

	flag.Parse()

	crlf = gohelper.cleanArgString(crlf)

	curTaskArgs := os.Args
	if len(crlf) > 0 || len(lf) > 0 {
		// -quiet -timeout 120 -walkexec . -walkpattern "*.bat" -- .\goexec.exe -quiet -timeout 5 -unix2dos -files __FILE__
		// setup args
		if len(walkpattern) == 0 {
			walkpattern = "*.bat"
		}
		execpath, _ := filepath.Abs(os.Args[0])
		curTaskArgs = []string{
			"--",
			execpath,
			"-timeout",
			"15",
		}
		if sysQuiet {
			curTaskArgs = append(curTaskArgs, "-quiet")
		}
		if len(crlf) > 0 {
			walkexec = crlf
			curTaskArgs = append(curTaskArgs, "-unix2dos")
		} else {
			walkexec = lf
			curTaskArgs = append(curTaskArgs, "-dos2unix")
		}
		curTaskArgs = append(curTaskArgs, []string{"--files", "__FILE__"}...)
	}

	logFile = gohelper.cleanArgString(logFile)
	fileList = gohelper.cleanArgString(fileList)
	walkfile = gohelper.cleanArgString(walkfile)
	workDir = gohelper.cleanArgString(workDir)
	preExecFile = gohelper.cleanArgString(preExecFile)
	postExecFile = gohelper.cleanArgString(postExecFile)
	walkexec = gohelper.cleanArgString(walkexec)
	walkregex = gohelper.cleanArgString(walkregex)
	walkpattern = gohelper.cleanArgString(walkpattern)
	loadEnvFile = gohelper.cleanArgString(loadEnvFile)
	ntpServers = gohelper.cleanArgString(ntpServers)
	tailfile = gohelper.cleanArgString(tailfile)
	helpers = gohelper.cleanArgString(helpers)
	tailsearch = gohelper.cleanArgString(tailsearch)
	shell = gohelper.cleanArgString(shell)

	workDir = strings.Trim(workDir, "/\\")
	walkexec = strings.Trim(walkexec, "/\\")

	if help {
		log.Printf("usage: %s [-timeout n] [-workdir path] -- <exe file/script> [args for child]\n", os.Args[0])
		flag.Usage()
		os.Exit(0)
	}

	if sysQuiet {
		if sysDebug {
			log.Printf("INFO: QUIET mode enabled, task output will not be showed\n")
			log.Printf("WARNING: debug mode disabled by -quiet\n")
		}
		sysDebug = false
	}

	sysEnv = os.Environ()
	if len(loadEnvFile) > 0 {
		if !sysQuiet {
			log.Printf("Info: load env from %s\n", loadEnvFile)
		}
		if sysDebug {
			for _, v := range sysEnv {
				log.Printf("DEBUG: system env: %v\n", v)
			}
		}
		lines, _, err := readLines(loadEnvFile)
		if err != nil {
			log.Fatalf("read env file %s failed: %v\n", loadEnvFile, err)
		}
		sysEnv, err = sysEnvMerge(sysEnv, lines)

		if err != nil {
			log.Fatalf("merge env file %s failed: %v\n", loadEnvFile, err)
		}
		if sysDebug {
			for _, v := range sysEnv {
				log.Printf("DEBUG: merged system env: %v\n", v)
			}
		}
	}

	if ignoreCase {
		tailsearch = strings.ToLower(tailsearch)
	}
	tArr := strings.Split(tailsearch, ",")
	searchs := []string{}
	for _, v := range tArr {
		v = strings.TrimSpace(v)
		if len(v) == 0 {
			continue
		}
		searchs = append(searchs, v)
	}
	if len(tailfile) > 0 {
		if logtail <= 0 {
			logtail = 100
		}
		_, err := os.Stat(tailfile)
		if err == nil {
			log.Printf("Info: Tail %d lines/search %v from %s\n", logtail, searchs, tailfile)
			log.Printf("---E:0---------------\n")
			log.Printf("---")
			log.Printf("---")
			log.Printf("---")
			var cnt int
			cnt, err = tailFile(tailfile, searchs, logtail, tailA, tailB, tailC, 1, nil, ignoreCase)
			log.Printf("---")
			log.Printf("---")
			log.Printf("---")
			log.Printf("---E:%d---------------\n", cnt)
		}
		if err != nil {
			if !tailff {
				log.Println("----%%%%----")
				log.Printf("ERROR: Tail %d lines from %s: %v\n", logtail, tailfile, err)
				log.Println("----%%%%----")
				os.Exit(1)
			}
		} else {
			if !tailff {
				os.Exit(0)
			}
		}
	}

	// todo: loop run multi-task

	// glob exec
	isGlob := false

	globlines, taskOrder, _ := argsDoubleMarkParser(curTaskArgs, "--")

	if len(globlines) > 0 {
		// first task to execArgs
		execArgs = append(execArgs, globlines[taskOrder[0]]...)
	}

	if len(shell) > 0 {
		timeout = 0
		// shellPath := "/bin/bash"
		// if runtime.GOOS == "windows" {
		// 	shellPath = "cmd.exe"

		// }
		log.Printf("enter shell: %s\n", shell)
		err = execShell(shell, execArgs, sysEnv)
		if err == nil {
			log.Printf("Info: shell %s exited\n", shell)
			os.Exit(0)
		} else {
			log.Printf("Error: shell %s exit: %v\n", shell, err)
			os.Exit(1)
		}
	}

	cores := runtime.NumCPU()

	if threads < 1 {
		threads = cores
	}

	if fanout < 1 {
		fanout = cores
	}

	if threads < fanout {
		threads = fanout
	}

	// maximize CPU usage for maximum performance
	runtime.GOMAXPROCS(threads + 1)

	if dos2unix || unix2dos {

		runtime.GOMAXPROCS(fanout)

		// all file listed, maybe seplated by ,
		fileList = strings.ReplaceAll(fileList, ",", " ")
		list := strings.Split(fileList, " ")
		newlist := []string{}
		for _, v := range list {
			v = strings.TrimSpace(v)
			if len(v) == 0 {
				continue
			}
			newlist = append(newlist, v)
		}
		if unix2dos {
			err = fastConvert(newlist, timeout, true)
		} else {
			err = fastConvert(newlist, timeout, false)
		}

		if err != nil {
			if unix2dos {
				log.Printf("ERROR: convert to DOS ending (CRLF) failed: %v, %v\n", newlist, err)
			} else {
				log.Printf("ERROR: convert to UNIX ending (LF) failed: %v, %v\n", newlist, err)
			}
			os.Exit(1)
		}
		os.Exit(0)
	}

	log.Printf("This machine has %d CPU cores, threads %d, fan out %d. \n", cores, threads, fanout)

	if synctime {
		exitcode := 0
		oldTS := time.Now()
		ntpArr := strings.Split(ntpServers, ",")
		log.Printf("Info: try to sync time with ntp servers: %s\n", ntpArr)
		ntpinfo := setTimeByNTPServers(ntpArr)
		tm := ntpinfo.TS
		err := ntpinfo.Err
		log.Printf("TIME DIFF: %s\n", tm.Sub(oldTS))
		if err != nil {
			log.Printf("WARNING: sync time failed: %v\n", err.Error())
			exitcode = 1
		} else {
			log.Printf("Time successfully updated: %s, %v\n", ntpinfo.Srv, tm)
		}
		if len(walkexec) == 0 && len(execArgs) == 0 && len(globlines) == 0 {
			os.Exit(exitcode)
		}
	}

	if len(tailfile) > 0 && tailff {
		// https://pkg.go.dev/github.com/hpcloud/tail#Config
		t, err := gt.TailFile(tailfile, gt.Config{Follow: true, ReOpen: true, Poll: true})
		if err != nil {
			log.Println("----%%%%----")
			log.Printf("ERROR: Tail %d lines from %s: %v\n", logtail, tailfile, err)
			log.Println("----%%%%----")
			os.Exit(1)
		}
		log.Println("----%%%%----")
		log.Printf("Info: tail -Ff %s\n", tailfile)
		log.Println("----%%%%----")
		log.Printf("---")
		log.Printf("---")
		log.Printf("---")
		go func() {
			for line := range t.Lines {
				fmt.Println(line.Text)
			}
		}()
		if len(walkexec) == 0 && len(execArgs) == 0 && len(globlines) == 0 {
			// wait for ctrl-C
			select {}
		}
	}

	oldProcList := map[int]string{}
	ProcWhiteList := map[string]bool{}

	if clear {

		if len(whitelist) > 0 {
			newlist, _, err := readLines(whitelist)
			if err != nil {
				log.Fatalf("ERROR: read clear/proc white list file %q: %v\n", whitelist, err)
			}
			for _, v := range newlist {
				v = strings.ToLower(v)
				ProcWhiteList[v] = true
				if sysDebug {
					log.Printf("DEBUG: Proc white list: %s\n", v)
				}
			}
			log.Printf("Info: clear/proc white list file %q: %d\n", whitelist, len(ProcWhiteList))
		}

		oldSnapshot, err := ps.Processes()
		if err != nil {
			log.Fatalf("take process snapshot failed: %v\n", err)
		}
		for _, v := range oldSnapshot {
			oldProcList[v.Pid()] = v.Executable()
			if sysDebug {
				log.Printf("Process snapshot: %d, %s\n", v.Pid(), v.Executable())
			}
		}
		log.Printf("Info: take snapshot of processes: %d\n", len(oldProcList))
	}

	if len(workDir) == 0 {
		workDir = "."
	}

	workDir, err = filepath.Abs(workDir)
	if err != nil {
		log.Fatalf("invalid working directory: %s, %v\n", workDir, err)
	}

	err = os.Chdir(workDir)
	if err != nil {
		log.Fatalf("changes the current working directory: %s, %v\n", workDir, err)
	} else {
		log.Printf("current working directory: %s\n", workDir)
	}
	if len(walkfile) > 0 {
		walkexec = walkfile
	}
	if len(walkexec) > 0 && len(execArgs) > 0 && len(globlines) == 1 {

		log.Printf("Walk-exec template: %v\n", execArgs)
		log.Printf("Walk-exec walkregex: %v\n", walkregex)
		log.Printf("Walk-exec walkpattern: %v\n", walkpattern)

		// reset
		globlines = make(map[string][]string, 1)
		taskOrder = []string{}

		out := []string{}

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

		if len(walkfile) > 0 {
			log.Printf("Info: walking items in file: %s\n", walkexec)
			newlist, _, err := readLines(walkexec)
			if err != nil {
				log.Fatalf("ERROR: walking the file %q: %v\n", walkexec, err)
			}
			for _, v := range newlist {
				ok, _ := twoMatch(v, walkpattern, re)
				if ok {
					out = append(out, v)
				}
			}
		} else {
			walkexec, err = filepath.Abs(walkexec)
			if err != nil {
				log.Fatalf("ERROR: invalid walk/exec directory: %s, %v\n", walkexec, err)
			} else {
				log.Printf("Info: walking/exec directory: %s\n", walkexec)
			}

			out, err = walkByFilter(walkexec, walkregex, walkpattern, norecursive)
			if err != nil {
				log.Fatalf("ERROR: walking the path %q: %v\n", walkexec, err)
			}
		}
		if len(out) == 0 {
			if errorOnEmptyWalk {
				log.Printf("ERROR: invalid walk parametter, nothing matched.\n")
				// flag.Usage()
				os.Exit(1)
			} else {
				log.Printf("WARNING: please check walk parametter, nothing matched.\n")
				os.Exit(0)
			}
		}

		debugcount := 0

		for _, match := range out {
			if debugcount < 20 && sysDebug {
				log.Printf("one glob file: %s\n", match)
			}
			argArr := []string{}
			argKey := ""
			for _, v := range execArgs {
				if v == "__FILE__" {
					argArr = append(argArr, match)
					argKey = argKey + "-" + match
				} else {
					argArr = append(argArr, v)
					argKey = argKey + "-" + v
				}
			}
			argKey = strings.TrimSpace(argKey) + "_" + fmt.Sprintf("%d", len(taskOrder))
			globlines[argKey] = argArr

			taskOrder = append(taskOrder, argKey)

			if debugcount < 20 && sysDebug {
				log.Printf("one glob args: key %s, value: %v\n", match, globlines[argKey])
			}
			debugcount++
		}
		isGlob = true
	}

	if len(globlines) == 0 {
		log.Printf("invalid parameters, nothing to exec, usage: %s [-timeout n] [-workdir path] -- <exe file/script> [args for child]\n", os.Args[0])
		flag.Usage()
		os.Exit(1)
	} else {
		// log.Printf("FANOUT: %d\n", fanout)
		log.Printf("TASKS: %d\n", len(globlines))
	}
	if len(logFile) > 0 {
		log.Printf("Info: task stdout/stderr logging: %s\n", logFile)
	}
	if len(preExecFile) == 0 {
		preExecFile, err = exec.LookPath(execArgs[0] + ".pre.bat")
		if err == nil {
			log.Printf("default pre-exec script: %s\n", preExecFile)
		} else {
			preExecFile = ""
		}
	}
	if len(preExecFile) == 0 {
		preExecFile, err = exec.LookPath(preExecFile)
		if err == nil {
			log.Printf("pre-exec script not found: %s, %v\n", preExecFile, err)
		} else {
			preExecFile = ""
		}
	}

	if len(preExecFile) == 0 {
		postExecFile, err = exec.LookPath(execArgs[0] + ".post.bat")
		if err == nil {
			log.Printf("default post-exec script: %s\n", postExecFile)
		} else {
			postExecFile = ""
		}
	}
	if len(postExecFile) == 0 {
		postExecFile, err = exec.LookPath(postExecFile)
		if err == nil {
			log.Printf("post-exec script not found: %s, %v\n", postExecFile, err)
		} else {
			preExecFile = ""
		}
	}

	startTS := time.Now()

	if isGlob {
		log.Printf("main-exec(%d) template: %v\n", os.Getpid(), execArgs)
	} else {
		log.Printf("main-exec(%d) script: %v\n", os.Getpid(), execArgs)
	}
	if !runLoop || timeout > 0 || toDaemon || isGlob {
		runLoop = false
		log.Printf("Loop running: FALSE")
	} else {
		log.Printf("Loop running: TRUE")
	}

	if len(preExecFile) > 0 {
		log.Printf("pre-exec script: %s, log to %s\n", preExecFile, logFile)
		cmdinfo := foreExec([]string{preExecFile}, logFile, workDir, preTimeout)
		if cmdinfo.Err != nil {
			log.Printf("WARNING: pre-exec script: %s, failed, %v\n", preExecFile, cmdinfo.Err)
		} else {
			log.Printf("Info: pre-exec script: %s, done, %v\n", preExecFile, cmdinfo.Err)
		}
	}

	// signal handle
	sigs := make(chan os.Signal, 128)
	sigOut := make(chan os.Signal, 128)

	signal.Notify(sigs)

	go func() {
		sig := <-sigs
		if sysDebug {
			log.Printf("SIG CATCHED: %d, %v\n", sig, sig)
		}
		sigOut <- sig
	}()

	mainCmdChan := make(chan *CmdInfo, fanout)

	quit := false
	var recvSig os.Signal
	var mainCmdInfo *CmdInfo
	var running bool = false

	fanoutCount := 1

	fanInCount := 0

	errorCount := 0

	ctrlc := 0

	helperTasks, helperOrder, _ := argsDoubleMarkParser(strings.Split(helpers, " "), "^^")

	helperChan := make(chan *CmdInfo, len(helperOrder))
	helperPidChan := make(chan int, len(helperOrder))

	if len(helperOrder) > 0 {
		helperTimeout := timeout
		if timeout > 0 {
			timeout = timeout + helperDelay + 3
		}
		log.Printf("Info: background helper, delay %d: %v, log to %s\n", helperDelay, helpers, logFile)

		helperWait := make(chan bool, 1)
		go func() {
			if sysDebug {
				log.Printf("Debug: background helper, helperOrder(%d): %v\n", len(helperOrder), helperOrder)
				log.Printf("Debug: background helper, helperTasks(%d): %v\n", len(helperTasks), helperTasks)
			}

			if helperDelay > 0 {
				<-time.After(time.Duration(helperDelay) * time.Second)
			}
			for k, argKey := range helperOrder {
				cmdArr, ok := helperTasks[argKey]
				if !ok {
					continue
				}
				if !sysQuiet {
					log.Printf("Info: background helper#%d: %v\n", k, cmdArr)
				}
				helperPidChan <- loopExec(cmdArr, logFile, helperChan, workDir, helperTimeout)
			}
			helperWait <- true
		}()
		if helperDelay <= 0 {
			<-helperWait
		}
	}

	log.Printf("main task timeout: %d\n", timeout)

	os.Setenv("TASK_EXEC_TIMEOUT", fmt.Sprintf("%d", timeout))

	if startDelay > 0 {
		if !sysQuiet {
			log.Printf("Info: wait for %d seconds before main-exec\n", startDelay)
		}
		<-time.After(time.Duration(startDelay) * time.Second)
	}

	runLines := []string{} // key: file path
	for _, v := range taskOrder {
		runLines = append(runLines, v)
	}
	runPtr := 0

	var cmdPid int = -1
	for !quit {

		if running {
			select {
			case mainCmdInfo = <-mainCmdChan:
				fanInCount++
				if sysDebug {
					log.Printf("mainCmdInfo: recv from mainCmdChan#%d: %v\n", fanInCount, mainCmdInfo)
				}
				if mainCmdInfo.Err != nil {
					if !sysQuiet {
						log.Printf("WARNING: ,fanout %d/%d, task exited with error: %v\n", fanInCount, fanoutCount, mainCmdInfo.Err)
					}
					errorCount++
				}

				if fanoutCount == fanInCount || (mainCmdInfo.Err != nil && exitOnFanError) {
					// all task finished or exit on error
					cmdPid = mainCmdInfo.Pid
					running = false
					if !sysQuiet {
						log.Printf("Info: fanout batch exited: %d/%d\n", fanInCount, fanoutCount)
					}

					<-time.After(50 * time.Millisecond)
				} else {
					if sysDebug {
						log.Printf("task exited, fanout: %d/%d\n", fanInCount, fanoutCount)
					}
				}
			case recvSig = <-sigOut:
				more := true
				for more {
					if sysDebug {
						log.Printf("MAIN LOOP, SIG: %d, %v\n", recvSig, recvSig)
					}
					if recvSig == syscall.SIGINT {
						ctrlc++
						if ctrlc > 30 {
							log.Printf("FORCE EXIT\n")
							os.Exit(1)
						}
					}

					if !runLoop && recvSig == syscall.SIGTERM {
						// exit when we are in daemon mode
						if sigToPidTree(cmdPid, syscall.SIGTERM) == nil {
							<-time.After(100 * time.Millisecond)
							sigToPidTree(cmdPid, syscall.SIGKILL)
						}
					} else {
						if sigToPidTree(cmdPid, recvSig) == nil {
							<-time.After(50 * time.Millisecond)
						}
					}
					select {
					case recvSig = <-sigOut:
					default:
						more = false
					}
				}
			}
		} else {

			if fanoutCount > 0 && !runLoop {
				// run once
				if len(runLines) == 0 {
					if sysDebug {
						log.Printf("Info: exited for run once: %v\n", execArgs)
					}
					break
				}
				// continue to run pending task
			} else {
				// todo: test loop running
				// next round
				if len(runLines) == 0 {
					runLines = runLines[:0]
					for _, v := range taskOrder {
						runLines = append(runLines, v)
					}
					runPtr = 0
				}
			}

			fanoutCount = 0
			fanInCount = 0

			for pos, k := range runLines {
				// record executed position
				runPtr = pos
				v, ok := globlines[k]
				if !ok {
					continue
				}
				cmdPid = loopExec(v, logFile, mainCmdChan, workDir, timeout)
				running = true
				fanoutCount++
				if sysDebug {
					log.Printf("fanout exec#%d/%d, %v\n", fanoutCount, fanout, v)
				}
				if cmdPid == -1 {
					log.Printf("MAIN LOOP#%d, exec failed: %v\n", fanoutCount, v)
					// still wait for cmdinfo from channel
				}
				if fanoutCount == fanout {
					break
				}
				if taskDelay > 0 {
					if sysDebug {
						log.Printf("wait for %d seconds before next task ...\n", taskDelay)
					}
					<-time.After(time.Duration(taskDelay) * time.Second)
				}
			}
			runLines = runLines[runPtr+1:]
		}
	}

	// disable signal handling
	signal.Reset()
	postSig := make(chan os.Signal, 128)
	signal.Notify(postSig)

	go func() {
		sig := <-postSig
		if sysDebug {
			log.Printf("POST LOOP, SIG: %d, %v\n", sig, sig)
		}
	}()

	if len(postExecFile) > 0 {
		log.Printf("post-exec script: %s, log to %s\n", postExecFile, logFile)
		cmdinfo := foreExec([]string{postExecFile}, logFile, workDir, postTimeout)
		if cmdinfo.Err != nil {
			log.Printf("WARNING: post-exec script: %v, failed, %v\n", cmdinfo.Args, cmdinfo.Err)
		} else {
			log.Printf("Info: post-exec script: %v, done, %v\n", cmdinfo.Args, cmdinfo.Err)
		}
	}
	if clear {
		err := clearProc(oldProcList, ProcWhiteList)
		if err != nil {
			log.Printf("WARNING: clearProc, %v\n", err)
		}
	}
	// clear first then check helper
	if len(helperOrder) > 0 {
		helperPid := -255
		for helperPid != -1024 {
			select {
			case helperPid = <-helperPidChan:
				sigToPidTree(helperPid, syscall.SIGTERM)
				<-time.After(100 * time.Millisecond)
				sigToPidTree(helperPid, syscall.SIGKILL)
				cmdinfo := <-helperChan
				if cmdinfo.Err != nil {
					log.Printf("WARNING: background helper: %s, failed, %v\n", cmdinfo.Args, cmdinfo.Err)
				} else {
					if !sysQuiet {
						log.Printf("Info: background helper: %s, done, %v\n", cmdinfo.Args, cmdinfo.Err)
					}
				}
			default:
				helperPid = -1024
			}
		}
	}

	if len(globlines) == 1 {
		if mainCmdInfo.ExitCode != 0 {
			log.Printf("Error: main-exec(%d) %v, failed, exit code %d, %v\n", os.Getpid(), mainCmdInfo.Args, mainCmdInfo.ExitCode, mainCmdInfo.Err)
		} else {
			log.Printf("Info: main-exec(%d) script, exit code %d: %v\n", os.Getpid(), mainCmdInfo.ExitCode, execArgs)
			errtail = false
		}
	} else {
		if errorCount > 0 {
			mainCmdInfo.ExitCode = errorCount
		}
		if mainCmdInfo.ExitCode != 0 {
			log.Printf("Error: main-exec(%d) batch task %v, failed, exit code %d, %v\n", os.Getpid(), mainCmdInfo.Args, mainCmdInfo.ExitCode, mainCmdInfo.Err)
		} else {
			log.Printf("Info: main-exec(%d) batch task %v,  exit code %d\n", os.Getpid(), execArgs, mainCmdInfo.ExitCode)
			errtail = false
		}
	}
	if logtail > 0 && len(logFile) > 0 && errtail {
		log.Printf("Info: Tail %d lines/search %v from %s\n", logtail, searchs, logFile)
		log.Printf("---TAIL-START---------------\n")
		log.Printf("---")
		log.Printf("---")
		log.Printf("---")
		cnt, err := tailFile(logFile, searchs, logtail, tailA, tailB, tailC, 1, nil, ignoreCase)
		log.Printf("---")
		log.Printf("---")
		log.Printf("---")
		log.Printf("---TAIL-DONE:%d---------------\n", cnt)
		if err != nil {
			log.Println("----%%%%----")
			log.Printf("ERROR: Tail %d lines from %s: %v\n", logtail, logFile, err)
			log.Println("----%%%%----")
		}
	}
	ShowExecuteTime(startTS)
	log.Printf("---\n")
	os.Exit(mainCmdInfo.ExitCode)
}
