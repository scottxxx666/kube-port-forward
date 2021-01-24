package main

import (
	"fmt"
	"runtime"
	"time"
)

func monitor() {
	for true {
		fmt.Println("Goroutine num: ", runtime.NumGoroutine())
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		fmt.Printf("Alloc = %v MiB  ", bToMb(m.Alloc))
		fmt.Printf("TotalAlloc = %v MiB  ", bToMb(m.TotalAlloc))
		fmt.Printf("Sys = %v MiB  ", bToMb(m.Sys))
		fmt.Printf("NumGC = %v \n", m.NumGC)

		time.Sleep(10 * time.Second)
	}
}

func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}
