//go:build linux

package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const madvCollapse = 25 // MADV_COLLAPSE, Linux 6.1

// collapseHeap asks the kernel to back the process's anonymous memory with
// transparent huge pages now (MADV_COLLAPSE), as a machine with THP enabled
// for every process would have. The blog's runs used mimalloc with "THP as
// the machine has them"; Go's heap only gets huge pages where THP is set to
// "always", so the harness does this for every map alike, after building it
// and before timing it. Without it, every map pays a TLB miss on most
// accesses to a 10M-entry tree, and that cost, not the maps, dominates.
func collapseHeap() {
	f, err := os.Open("/proc/self/maps")
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 5 || fields[1] != "rw-p" {
			continue
		}
		// Anonymous memory: unnamed, the C heap, or the Go heap, which Go
		// 1.25+ names "[anon: Go: heap]".
		if name := strings.Join(fields[5:], " "); name != "" && name != "[heap]" && name != "[anon: Go: heap]" {
			continue
		}
		lo, hi, ok := strings.Cut(fields[0], "-")
		if !ok {
			continue
		}
		start, err1 := strconv.ParseUint(lo, 16, 64)
		end, err2 := strconv.ParseUint(hi, 16, 64)
		if err1 != nil || err2 != nil || end-start < 2<<20 {
			continue
		}
		syscall.Syscall(syscall.SYS_MADVISE, uintptr(start), uintptr(end-start), madvCollapse)
	}
}

// hugePagesKB reports the process's anonymous memory in huge pages.
func hugePagesKB() int {
	b, err := os.ReadFile("/proc/self/smaps_rollup")
	if err != nil {
		return -1
	}
	for _, line := range strings.Split(string(b), "\n") {
		if rest, ok := strings.CutPrefix(line, "AnonHugePages:"); ok {
			n, _ := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rest), "kB")))
			return n
		}
	}
	return -1
}

// powerSource reports whether the machine runs on mains power. A laptop on
// battery may cap its CPU's power, and every number with it, so the results
// header records it: "AC", "battery", or "unknown" when the machine reports no
// mains supply.
func powerSource() string {
	dirs, _ := filepath.Glob("/sys/class/power_supply/*")
	mains := false
	for _, d := range dirs {
		t, _ := os.ReadFile(filepath.Join(d, "type"))
		if strings.TrimSpace(string(t)) != "Mains" {
			continue
		}
		mains = true
		if on, _ := os.ReadFile(filepath.Join(d, "online")); strings.TrimSpace(string(on)) == "1" {
			return "AC"
		}
	}
	if mains {
		return "battery"
	}
	return "unknown"
}
