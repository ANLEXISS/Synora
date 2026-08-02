//go:build !linux

package shadowworkflow

import (
	"runtime"
)

func readProcessSample(_ bool) ProcessSample {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return ProcessSample{HeapAlloc: stats.HeapAlloc, HeapInuse: stats.HeapInuse, HeapObjects: stats.HeapObjects, HeapReleased: stats.HeapReleased, Sys: stats.Sys, TotalAlloc: stats.TotalAlloc, Mallocs: stats.Mallocs, Frees: stats.Frees, NumGC: stats.NumGC, PauseTotalNS: stats.PauseTotalNs, Goroutines: runtime.NumGoroutine()}
}
