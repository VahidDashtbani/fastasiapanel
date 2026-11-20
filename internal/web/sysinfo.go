package web

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type SysStats struct {
	CPU        float64 `json:"cpu"`
	CPUCores   int     `json:"cpu_cores"`
	MemUsed    uint64  `json:"mem_used"`
	MemTotal   uint64  `json:"mem_total"`
	DiskUsed   uint64  `json:"disk_used"`
	DiskTotal  uint64  `json:"disk_total"`
	NetUp      uint64  `json:"net_up"`
	NetDown    uint64  `json:"net_down"`
	NetUpSpd   uint64  `json:"net_up_speed"`
	NetDownSpd uint64  `json:"net_down_speed"`
	TCPCount   int     `json:"tcp_count"`
	UDPCount   int     `json:"udp_count"`
	Loads      string  `json:"loads"`
	OS         string  `json:"os"`
	Arch       string  `json:"arch"`
}

var sysCollector = struct {
	mu          sync.Mutex
	lastTime    time.Time
	lastIdle    uint64
	lastTotal   uint64
	lastNetUp   uint64
	lastNetDown uint64
}{}

func readCPUTimes() (idle, total uint64, ok bool) {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)
			if len(fields) < 5 {
				return 0, 0, false
			}
			for i := 1; i < len(fields); i++ {
				v, _ := strconv.ParseUint(fields[i], 10, 64)
				total += v
				if i == 4 || i == 5 { // idle + iowait
					idle += v
				}
			}
			return idle, total, true
		}
	}
	return 0, 0, false
}

func readMemInfo() (used, total uint64) {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	var memTotal, memAvailable, memFree, buffers, cached uint64
	var hasAvailable bool
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		v, _ := strconv.ParseUint(fields[1], 10, 64)
		v *= 1024 // kB to bytes
		switch fields[0] {
		case "MemTotal:":
			memTotal = v
		case "MemAvailable:":
			memAvailable = v
			hasAvailable = true
		case "MemFree:":
			memFree = v
		case "Buffers:":
			buffers = v
		case "Cached:":
			cached = v
		}
	}
	if !hasAvailable {
		memAvailable = memFree + buffers + cached
	}
	if memTotal >= memAvailable {
		return memTotal - memAvailable, memTotal
	}
	return 0, memTotal
}

func readNetDev() (rx, tx uint64) {
	b, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0, 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		iface := strings.TrimSpace(line[:idx])
		if iface == "lo" || strings.HasPrefix(iface, "docker") || strings.HasPrefix(iface, "veth") {
			continue
		}
		fields := strings.Fields(line[idx+1:])
		if len(fields) >= 9 {
			r, _ := strconv.ParseUint(fields[0], 10, 64)
			t, _ := strconv.ParseUint(fields[8], 10, 64)
			rx += r
			tx += t
		}
	}
	return rx, tx
}

func countSockets(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) <= 1 {
		return 0
	}
	return len(lines) - 1
}

func collectSysStats() SysStats {
	st := SysStats{
		CPUCores: runtime.NumCPU(),
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
	}

	var fsStat syscall.Statfs_t
	if err := syscall.Statfs("/", &fsStat); err == nil {
		st.DiskTotal = fsStat.Blocks * uint64(fsStat.Bsize)
		st.DiskUsed = (fsStat.Blocks - fsStat.Bfree) * uint64(fsStat.Bsize)
	}

	st.MemUsed, st.MemTotal = readMemInfo()
	rx, tx := readNetDev()
	st.NetDown = rx
	st.NetUp = tx

	st.TCPCount = countSockets("/proc/net/tcp") + countSockets("/proc/net/tcp6")
	st.UDPCount = countSockets("/proc/net/udp") + countSockets("/proc/net/udp6")

	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(b))
		if len(fields) >= 3 {
			st.Loads = fields[0] + " " + fields[1] + " " + fields[2]
		}
	}

	sysCollector.mu.Lock()
	defer sysCollector.mu.Unlock()
	now := time.Now()
	idle, total, ok := readCPUTimes()
	if !sysCollector.lastTime.IsZero() {
		dt := now.Sub(sysCollector.lastTime).Seconds()
		if dt > 0 {
			if tx >= sysCollector.lastNetUp {
				st.NetUpSpd = uint64(float64(tx-sysCollector.lastNetUp) / dt)
			}
			if rx >= sysCollector.lastNetDown {
				st.NetDownSpd = uint64(float64(rx-sysCollector.lastNetDown) / dt)
			}
		}
		if ok && total > sysCollector.lastTotal {
			dTotal := float64(total - sysCollector.lastTotal)
			dIdle := float64(idle - sysCollector.lastIdle)
			st.CPU = (1.0 - dIdle/dTotal) * 100.0
			if st.CPU < 0 {
				st.CPU = 0
			}
		}
	}
	sysCollector.lastTime = now
	if ok {
		sysCollector.lastIdle = idle
		sysCollector.lastTotal = total
	}
	sysCollector.lastNetUp = tx
	sysCollector.lastNetDown = rx

	return st
}