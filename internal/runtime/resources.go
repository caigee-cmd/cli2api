package runtime

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ResourceSnapshot is a point-in-time view of host process usage for the
// server and each managed worker process. CPU percent follows the ps/top
// convention: 100 == one core fully busy.
type ResourceSnapshot struct {
	SampledAt time.Time         `json:"sampled_at"`
	Server    ProcessResources  `json:"server"`
	Workers   []WorkerResources `json:"workers"`
}

type ProcessResources struct {
	PID        int     `json:"pid"`
	RSSBytes   int64   `json:"rss_bytes"`
	HeapBytes  int64   `json:"heap_bytes,omitempty"`
	Goroutines int     `json:"goroutines,omitempty"`
	CPUPercent float64 `json:"cpu_percent"`
}

type WorkerResources struct {
	AccountID  string  `json:"account_id"`
	PID        int     `json:"pid"`
	RSSBytes   int64   `json:"rss_bytes"`
	CPUPercent float64 `json:"cpu_percent"`
}

// WorkerPIDs maps account ID -> OS PID for managed child processes. In-process
// providers never appear here. Safe for concurrent use.
func (m *Manager) WorkerPIDs() map[string]int {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]int, len(m.processes))
	for id, p := range m.processes {
		if pid := p.PID(); pid > 0 {
			out[id] = pid
		}
	}
	return out
}

// Resources returns the most recent resource snapshot. The maintenance loop
// refreshes it on every tick; if it has never run (tests, minimal embedding)
// a sample is taken synchronously.
func (m *Manager) Resources() ResourceSnapshot {
	if snap := m.resources.Load(); snap != nil {
		return *snap
	}
	return m.sampleResources()
}

// sampleResources gathers a fresh snapshot and publishes it. Concurrent
// callers are serialized; the second one reuses the fresh snapshot.
func (m *Manager) sampleResources() ResourceSnapshot {
	m.resMu.Lock()
	defer m.resMu.Unlock()
	if snap := m.resources.Load(); snap != nil && time.Since(snap.SampledAt) < time.Second {
		return *snap
	}
	snap := collectResources(m.WorkerPIDs(), &m.cpuPrev)
	m.resources.Store(&snap)
	return snap
}

// procTime is a previous utime+stime sample used to derive CPU percent.
type procTime struct {
	jiffies uint64
	at      time.Time
}

func collectResources(workers map[string]int, prev *map[int]procTime) ResourceSnapshot {
	now := time.Now().UTC()
	snap := ResourceSnapshot{SampledAt: now}

	pids := []int{os.Getpid()}
	pidToAccount := map[int]string{}
	for id, pid := range workers {
		pids = append(pids, pid)
		pidToAccount[pid] = id
	}

	stats := readProcStats(pids)
	if stats == nil {
		stats = readPSStats(pids)
	}

	next := map[int]procTime{}
	selfPID := os.Getpid()
	for pid, st := range stats {
		cpu := st.cpuPercent
		if st.jiffies > 0 {
			if p, ok := (*prev)[pid]; ok {
				if dt := now.Sub(p.at).Seconds(); dt > 0 {
					cpu = float64(st.jiffies-p.jiffies) / clkTck() / dt * 100
				}
			}
			next[pid] = procTime{jiffies: st.jiffies, at: now}
		}
		wr := WorkerResources{
			AccountID:  pidToAccount[pid],
			PID:        pid,
			RSSBytes:   st.rssBytes,
			CPUPercent: round2(cpu),
		}
		if pid == selfPID {
			snap.Server = ProcessResources{PID: pid, RSSBytes: st.rssBytes, CPUPercent: round2(cpu)}
			continue
		}
		snap.Workers = append(snap.Workers, wr)
	}
	*prev = next

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	if snap.Server.PID == 0 {
		snap.Server.PID = selfPID
	}
	snap.Server.HeapBytes = int64(ms.HeapAlloc)
	snap.Server.Goroutines = runtime.NumGoroutine()
	return snap
}

type procStat struct {
	rssBytes   int64
	jiffies    uint64  // utime+stime; 0 when CPU came from ps directly
	cpuPercent float64 // filled by the ps path
}

// readProcStats reads /proc/<pid>/stat + status on Linux. Returns nil when
// procfs is unavailable (macOS, etc.) so callers can fall back to ps.
func readProcStats(pids []int) map[int]procStat {
	if runtime.GOOS != "linux" {
		return nil
	}
	out := map[int]procStat{}
	for _, pid := range pids {
		raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		if err != nil {
			continue
		}
		line := string(raw)
		// comm is in parens and may contain spaces; fields after ')'.
		end := strings.LastIndex(line, ")")
		if end < 0 {
			continue
		}
		fields := strings.Fields(line[end+1:])
		// fields[0] is state; utime=11, stime=12, rss( pages )=21 relative to state.
		if len(fields) < 22 {
			continue
		}
		utime, _ := strconv.ParseUint(fields[11], 10, 64)
		stime, _ := strconv.ParseUint(fields[12], 10, 64)
		pages, _ := strconv.ParseInt(fields[21], 10, 64)
		out[pid] = procStat{
			jiffies:  utime + stime,
			rssBytes: pages * int64(os.Getpagesize()),
		}
	}
	return out
}

// readPSStats shells out to ps for RSS (KB) and lifetime CPU percent. Used on
// non-Linux hosts and as a fallback when /proc reads fail.
func readPSStats(pids []int) map[int]procStat {
	args := make([]string, 0, len(pids))
	for _, pid := range pids {
		args = append(args, strconv.Itoa(pid))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-o", "pid=,rss=,%cpu=", "-p", strings.Join(args, ",")).Output()
	if err != nil {
		return nil
	}
	stats := map[int]procStat{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) != 3 {
			continue
		}
		pid, _ := strconv.Atoi(f[0])
		rssKB, _ := strconv.ParseInt(f[1], 10, 64)
		cpu, _ := strconv.ParseFloat(f[2], 64)
		if pid > 0 {
			stats[pid] = procStat{rssBytes: rssKB * 1024, cpuPercent: cpu}
		}
	}
	return stats
}

var (
	clkTckOnce sync.Once
	clkTckVal  float64
)

// clkTck returns USER_HZ/CLK_TCK (usually 100) for jiffies->seconds math.
func clkTck() float64 {
	clkTckOnce.Do(func() {
		clkTckVal = 100
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(ctx, "getconf", "CLK_TCK").Output(); err == nil {
			if v, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64); err == nil && v > 0 {
				clkTckVal = v
			}
		}
	})
	return clkTckVal
}

func round2(v float64) float64 {
	if v < 0 {
		return 0
	}
	return float64(int(v*100+0.5)) / 100
}
