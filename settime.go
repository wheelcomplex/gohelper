package main

import (
	"fmt"
	"log"
	"time"

	"github.com/beevik/ntp"
)

type NtpInfo struct {
	Err error
	Srv string
	TS  time.Time
}

var emptyTime time.Time

func setTimeByNTPServers(ntpArr []string) *NtpInfo {
	synchan := make(chan *NtpInfo, len(ntpArr))
	donechan := make(chan *NtpInfo, len(ntpArr))
	for k, v := range ntpArr {
		if sysDebug {
			log.Printf("DEBUG: try to sync time with ntp server#%d: %s\n", k, v)
		}
		go func(host string) {
			ntpinfo := &NtpInfo{
				Srv: host,
			}
			defer func() {
				if synchan == nil {
					return
				}
				ntpinfo.TS = time.Now()
				synchan <- ntpinfo
			}()

			r, err := ntp.Query(host)
			ntpinfo.Err = err
			if err != nil {
				return
			}

			err = r.Validate()
			ntpinfo.Err = err
			if err != nil {
				return
			}
			t := time.Now().Add(r.ClockOffset)
			select {
			case done := <-donechan:
				ntpinfo.Err = fmt.Errorf("time sync with %s cancelled(%v), allready done by %s, %v", host, t, done.Srv, done.TS)
				// if sysDebug {
				// 	log.Printf("%s\n", ntpinfo.Err)
				// }
			default:
				err = setPlatformTime(t)
				ntpinfo.Err = err
			}
			return
		}(v)
	}
	recvCount := 0
	ok := false
	ntpinfo := &NtpInfo{}
	for {
		ntpinfo = <-synchan
		recvCount++
		if ntpinfo.Err == nil {
			log.Printf("Time successfully updated: %s, %v\n", ntpinfo.Srv, ntpinfo.Err)
			ok = true
			for i := 0; i < len(ntpArr); i++ {
				// cancel others
				donechan <- ntpinfo
			}
			// return first ok
			break
		} else {
			log.Printf("WARNING: sync time failed: %s, %v\n", ntpinfo.Srv, ntpinfo.Err)
		}
		if recvCount >= len(ntpArr) {
			break
		}
	}
	if !ok {
		ntpinfo.Err = fmt.Errorf("all ntp server failed")
		ntpinfo.TS = time.Now()
		ntpinfo.Srv = "err.no.ntp.server"
	}
	return ntpinfo
}
