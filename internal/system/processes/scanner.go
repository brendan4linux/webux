// Package processes reads live process data directly from /proc.
// No external tools (ps, top, htop) are required.
package processes

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Process represents a single running process.
type Process struct {
	PID        int     `json:"pid"`
	PPID       int     `json:"ppid"`
	Name       string  `json:"name"`        // from /proc/<pid>/comm
	Cmdline    string  `json:"cmdline"`     // from /proc/<pid>/cmdline
	State      string  `json:"state"`       // R, S, D, Z, T, etc.
	StateName  string  `json:"state_name"`  // human-readable
	Username   string  `json:"username"`
	UID        int     `json:"uid"`
	CPUPercent float64 `json:"cpu_percent"`
	MemRSS     uint64  `json:"mem_rss_kb"`  // resident set size in KB
	MemVSZ     uint64  `json:"mem_vsz_kb"`  // virtual memory in KB
	Threads    int     `json:"threads"`
	Priority   int     `json:"priority"`
	Nice       int     `json:"nice"`
	StartTime  string  `json:"start_time"`
	OpenFDs    int     `json:"open_fds"`
}

var stateNames = map[string]string{
	"R": "Running",
	"S": "Sleeping",
	"D": "Waiting (uninterruptible)",
	"Z": "Zombie",
	"T": "Stopped",
	"t": "Tracing stop",
	"X": "Dead",
	"I": "Idle",
}

// Scanner reads process info from /proc.
type Scanner struct {
	userCache map[int]string // UID → username cache
	bootTime  uint64         // seconds since epoch at boot (for start time calc)
	hertz     float64        // clock ticks per second (usually 100)
}

// NewScanner creates a Scanner, reading /proc/stat for boot time.
func NewScanner() *Scanner {
	s := &Scanner{
		userCache: buildUserCache(),
		hertz:     100, // Linux default; could read from sysconf but 100 is universal
	}
	s.bootTime = readBootTime()
	return s
}

// List returns all visible processes.
func (s *Scanner) List() ([]Process, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("read /proc: %w", err)
	}

	// Two-pass: first read raw ticks, then compute CPU% using a 200ms delta
	type rawProc struct {
		pid      int
		utime    uint64
		stime    uint64
		elapsed  float64 // seconds since proc started
	}

	var raws []rawProc
	var procs []Process

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		p, raw, err := s.readProc(pid)
		if err != nil {
			continue // process may have exited
		}
		procs = append(procs, p)
		raws = append(raws, rawProc{pid: pid, utime: raw[0], stime: raw[1], elapsed: float64(raw[2])})
	}

	// Short sleep then re-read CPU ticks for a real delta
	time.Sleep(250 * time.Millisecond)
	totalDelta := 0.25 * s.hertz // ticks in 250ms

	for i := range procs {
		pid := raws[i].pid
		p2, raw2, err := s.readProc(pid)
		if err != nil {
			continue // process exited during measurement
		}
		tickDelta := float64((raw2[0] + raw2[1]) - (raws[i].utime + raws[i].stime))
		procs[i].CPUPercent = math.Round((tickDelta/totalDelta)*1000) / 10
		_ = p2 // we only need the tick delta
	}

	return procs, nil
}

// readProc reads a single process from /proc/<pid>/.
// Returns the Process and [utime, stime, elapsedSeconds] for CPU calc.
func (s *Scanner) readProc(pid int) (Process, [3]uint64, error) {
	base := fmt.Sprintf("/proc/%d", pid)
	var raw [3]uint64

	// /proc/<pid>/stat — the main info source
	statBytes, err := os.ReadFile(base + "/stat")
	if err != nil {
		return Process{}, raw, err
	}
	stat, err := parseStat(string(statBytes))
	if err != nil {
		return Process{}, raw, err
	}

	// /proc/<pid>/status — gives UID, threads, memory
	status, _ := parseStatus(base + "/status")

	// /proc/<pid>/cmdline
	cmdline := readCmdline(base + "/cmdline")

	// Resolve username
	uid := status["Uid"]
	username := s.userCache[uid]
	if username == "" {
		username = strconv.Itoa(uid)
	}

	// Start time: convert ticks-since-boot to wall clock
	startTicks, _ := strconv.ParseUint(stat["starttime"], 10, 64)
	startSecs := s.bootTime + startTicks/uint64(s.hertz)
	startTime := time.Unix(int64(startSecs), 0).Format("Jan02 15:04")

	// Open file descriptors count
	fds, _ := countFDs(base + "/fd")

	// RSS and VSZ from /proc/<pid>/statm: pages
	rssKB, vszKB := readStatm(base + "/statm")

	utime, _ := strconv.ParseUint(stat["utime"], 10, 64)
	stime, _ := strconv.ParseUint(stat["stime"], 10, 64)
	raw = [3]uint64{utime, stime, startTicks}

	stateChar := stat["state"]
	p := Process{
		PID:       pid,
		PPID:      atoi(stat["ppid"]),
		Name:      stat["comm"],
		Cmdline:   cmdline,
		State:     stateChar,
		StateName: stateNames[stateChar],
		Username:  username,
		UID:       uid,
		MemRSS:    rssKB,
		MemVSZ:    vszKB,
		Threads:   status["Threads"],
		Priority:  atoi(stat["priority"]),
		Nice:      atoi(stat["nice"]),
		StartTime: startTime,
		OpenFDs:   fds,
	}
	return p, raw, nil
}

// parseStat parses /proc/<pid>/stat into a map.
// The comm field (process name) may contain spaces and is wrapped in parens.
func parseStat(content string) (map[string]string, error) {
	// Format: pid (comm) state ppid ...
	// Find the last ')' to handle comm fields with spaces or parens
	start := strings.Index(content, "(")
	end := strings.LastIndex(content, ")")
	if start < 0 || end < 0 || end <= start {
		return nil, fmt.Errorf("malformed stat")
	}

	pidStr := strings.TrimSpace(content[:start])
	comm := content[start+1 : end]
	rest := strings.Fields(content[end+1:])

	m := map[string]string{
		"pid":  pidStr,
		"comm": comm,
	}

	// Fields after comm: state ppid pgrp session tty_nr tpgid flags
	//   minflt cminflt majflt cmajflt utime stime cutime cstime
	//   priority nice num_threads itrealvalue starttime ...
	fields := []string{
		"state", "ppid", "pgrp", "session", "tty_nr", "tpgid", "flags",
		"minflt", "cminflt", "majflt", "cmajflt",
		"utime", "stime", "cutime", "cstime",
		"priority", "nice", "num_threads", "itrealvalue", "starttime",
	}
	for i, f := range fields {
		if i < len(rest) {
			m[f] = rest[i]
		}
	}
	return m, nil
}

// parseStatus parses /proc/<pid>/status into int fields we care about.
func parseStatus(path string) (map[string]int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	m := make(map[string]int)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "Uid":
			// "real effective saved fs" — we want real (first)
			fields := strings.Fields(val)
			if len(fields) > 0 {
				m["Uid"], _ = strconv.Atoi(fields[0])
			}
		case "Threads":
			m["Threads"], _ = strconv.Atoi(val)
		case "VmRSS":
			// "1234 kB"
			fields := strings.Fields(val)
			if len(fields) > 0 {
				m["VmRSS"], _ = strconv.Atoi(fields[0])
			}
		}
	}
	return m, nil
}

func readCmdline(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	// NUL-separated args
	for i, c := range b {
		if c == 0 {
			b[i] = ' '
		}
	}
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

func readStatm(path string) (rssKB, vszKB uint64) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	fields := strings.Fields(string(b))
	if len(fields) < 2 {
		return
	}
	pageSize := uint64(os.Getpagesize())
	vsz, _ := strconv.ParseUint(fields[0], 10, 64)
	rss, _ := strconv.ParseUint(fields[1], 10, 64)
	vszKB = vsz * pageSize / 1024
	rssKB = rss * pageSize / 1024
	return
}

func countFDs(fdDir string) (int, error) {
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		return 0, err
	}
	return len(entries), nil
}

func readBootTime() uint64 {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "btime ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				t, _ := strconv.ParseUint(fields[1], 10, 64)
				return t
			}
		}
	}
	return 0
}

func buildUserCache() map[int]string {
	m := make(map[int]string)
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return m
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), ":")
		if len(parts) < 3 {
			continue
		}
		uid, err := strconv.Atoi(parts[2])
		if err != nil {
			continue
		}
		m[uid] = parts[0]
	}
	return m
}

// CLIEquivalent returns the shell command a user would run for similar output.
func CLIEquivalent() string {
	return "ps aux --sort=-%cpu"
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// SortBy sorts processes in-place by a field name.
func SortBy(procs []Process, field string, asc bool) {
	// Simple insertion sort — process lists are rarely > 500 entries
	for i := 1; i < len(procs); i++ {
		for j := i; j > 0; j-- {
			if less(procs[j-1], procs[j], field) == asc {
				break
			}
			procs[j-1], procs[j] = procs[j], procs[j-1]
		}
	}
}

func less(a, b Process, field string) bool {
	switch field {
	case "pid":
		return a.PID < b.PID
	case "cpu":
		return a.CPUPercent < b.CPUPercent
	case "mem":
		return a.MemRSS < b.MemRSS
	case "name":
		return a.Name < b.Name
	case "user":
		return a.Username < b.Username
	default:
		return a.PID < b.PID
	}
}

// suppress unused import warning for filepath
var _ = filepath.Join
