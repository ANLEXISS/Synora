//go:build linux

package shadowworkflow

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
)

func readProcessSample(include bool) ProcessSample {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	out := ProcessSample{HeapAlloc: stats.HeapAlloc, HeapInuse: stats.HeapInuse, HeapObjects: stats.HeapObjects, HeapReleased: stats.HeapReleased, Sys: stats.Sys, TotalAlloc: stats.TotalAlloc, Mallocs: stats.Mallocs, Frees: stats.Frees, NumGC: stats.NumGC, PauseTotalNS: stats.PauseTotalNs, Goroutines: runtime.NumGoroutine()}
	if stats.NumGC > 0 {
		out.LastGCPauseNS = stats.PauseNs[(stats.NumGC-1)%uint32(len(stats.PauseNs))]
	}
	if !include {
		return out
	}
	if file, err := os.Open("/proc/self/status"); err == nil {
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			parts := strings.SplitN(scanner.Text(), ":", 2)
			if len(parts) != 2 {
				continue
			}
			value := strings.TrimSpace(parts[1])
			switch parts[0] {
			case "VmRSS":
				out.RSSBytes = parseProcKB(value)
			case "Threads":
				out.Threads, _ = strconv.Atoi(value)
			}
		}
		_ = file.Close()
	}
	if file, err := os.Open("/proc/self/stat"); err == nil {
		data, readErr := os.ReadFile("/proc/self/stat")
		_ = file.Close()
		if readErr == nil {
			if close := strings.LastIndex(string(data), ")"); close >= 0 {
				fields := strings.Fields(string(data)[close+2:])
				if len(fields) > 12 {
					user, userErr := strconv.ParseInt(fields[11], 10, 64)
					system, systemErr := strconv.ParseInt(fields[12], 10, 64)
					if userErr == nil && systemErr == nil {
						out.CPUUserNS = user * 10000000
						out.CPUSystemNS = system * 10000000
					}
				}
			}
		}
	}
	if entries, err := os.ReadDir("/proc/self/fd"); err == nil {
		out.FileDescriptors = len(entries)
	}
	return out
}

func parseProcKB(value string) int64 {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return 0
	}
	kilobytes, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return 0
	}
	return kilobytes * 1024
}
