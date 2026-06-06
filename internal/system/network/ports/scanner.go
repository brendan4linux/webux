// Package ports reads open TCP/UDP ports directly from /proc/net/* and
// cross-references /proc/<pid>/fd and /proc/<pid>/cmdline to identify
// the owning process. No external tools (ss, netstat, lsof) are required.
package ports

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Proto represents the transport protocol of a socket.
type Proto string

const (
	ProtoTCP  Proto = "tcp"
	ProtoTCP6 Proto = "tcp6"
	ProtoUDP  Proto = "udp"
	ProtoUDP6 Proto = "udp6"
)

// SocketState maps the hex state codes in /proc/net/tcp to human-readable names.
var SocketState = map[string]string{
	"01": "ESTABLISHED",
	"02": "SYN_SENT",
	"03": "SYN_RECV",
	"04": "FIN_WAIT1",
	"05": "FIN_WAIT2",
	"06": "TIME_WAIT",
	"07": "CLOSE",
	"08": "CLOSE_WAIT",
	"09": "LAST_ACK",
	"0A": "LISTEN",
	"0B": "CLOSING",
}

// PortInfo describes a single open port / socket.
type PortInfo struct {
	Proto         Proto  `json:"proto"`
	LocalIP       string `json:"local_ip"`
	LocalPort     uint16 `json:"local_port"`
	State         string `json:"state"`
	PID           int    `json:"pid"`
	ProcessName   string `json:"process_name"`
	Cmdline       string `json:"cmdline"`
	Inode         uint64 `json:"inode"`
	SystemdSocket string `json:"systemd_socket,omitempty"`
}

// Scanner collects all open ports on the host.
type Scanner struct{}

// NewScanner returns a ready Scanner.
func NewScanner() *Scanner { return &Scanner{} }

// Scan returns all listening/open ports discovered from /proc/net/*.
// Uses a 5-second timeout to prevent hanging on stuck processes.
func (s *Scanner) Scan() ([]PortInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Build inode→pid map with timeout and parallelism
	inodePID, err := buildInodePIDMap(ctx)
	if err != nil {
		// Don't fail the whole scan — just return ports without process names
		inodePID = make(map[uint64]int)
	}

	var all []PortInfo

	protos := []struct {
		proto Proto
		path  string
		ipv6  bool
	}{
		{ProtoTCP, "/proc/net/tcp", false},
		{ProtoTCP6, "/proc/net/tcp6", true},
		{ProtoUDP, "/proc/net/udp", false},
		{ProtoUDP6, "/proc/net/udp6", true},
	}

	for _, p := range protos {
		entries, err := parseProcNet(p.path, p.proto, p.ipv6)
		if err != nil {
			continue // file may not exist on all kernels
		}
		for i := range entries {
			if pid, ok := inodePID[entries[i].Inode]; ok {
				entries[i].PID = pid
				entries[i].ProcessName, entries[i].Cmdline = readProcessName(pid)
			}
		}
		all = append(all, entries...)
	}

	return all, nil
}

// ScanListening returns only LISTEN-state TCP ports and all UDP ports.
func (s *Scanner) ScanListening() ([]PortInfo, error) {
	all, err := s.Scan()
	if err != nil {
		return nil, err
	}
	var out []PortInfo
	for _, p := range all {
		if p.State == "LISTEN" || p.Proto == ProtoUDP || p.Proto == ProtoUDP6 {
			out = append(out, p)
		}
	}
	return out, nil
}

// CLIEquivalent returns the shell command a user would run to get similar output.
func CLIEquivalent() string {
	return "ss -tulpn"
}

// --- internal helpers -------------------------------------------------------

func parseProcNet(path string, proto Proto, ipv6 bool) ([]PortInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []PortInfo
	scanner := bufio.NewScanner(f)
	scanner.Scan() // skip header

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}

		localHex := fields[1]
		stateHex := strings.ToUpper(fields[3])
		inodeStr := fields[9]

		localIP, localPort, err := parseHexAddr(localHex, ipv6)
		if err != nil {
			continue
		}

		inode, _ := strconv.ParseUint(inodeStr, 10, 64)

		state := SocketState[stateHex]
		if state == "" {
			state = stateHex
		}
		if (proto == ProtoUDP || proto == ProtoUDP6) && state == "CLOSE" {
			state = "UNCONN"
		}

		out = append(out, PortInfo{
			Proto:     proto,
			LocalIP:   localIP,
			LocalPort: localPort,
			State:     state,
			Inode:     inode,
		})
	}
	return out, scanner.Err()
}

func parseHexAddr(hexAddr string, ipv6 bool) (ip string, port uint16, err error) {
	parts := strings.SplitN(hexAddr, ":", 2)
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("invalid addr: %q", hexAddr)
	}

	portVal, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return "", 0, err
	}
	port = uint16(portVal)

	rawIP, err := hex.DecodeString(parts[0])
	if err != nil {
		return "", 0, err
	}

	if ipv6 {
		if len(rawIP) != 16 {
			return "", 0, fmt.Errorf("unexpected ipv6 len: %d", len(rawIP))
		}
		for i := 0; i < 16; i += 4 {
			rawIP[i], rawIP[i+3] = rawIP[i+3], rawIP[i]
			rawIP[i+1], rawIP[i+2] = rawIP[i+2], rawIP[i+1]
		}
		ip = net.IP(rawIP).String()
	} else {
		if len(rawIP) != 4 {
			return "", 0, fmt.Errorf("unexpected ipv4 len: %d", len(rawIP))
		}
		ip = fmt.Sprintf("%d.%d.%d.%d", rawIP[3], rawIP[2], rawIP[1], rawIP[0])
	}

	return ip, port, nil
}

// buildInodePIDMap scans /proc/<pid>/fd/* symlinks in parallel with a
// per-process timeout so a single stuck process can't hang the whole scan.
func buildInodePIDMap(ctx context.Context) (map[uint64]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}

	type result struct {
		inode uint64
		pid   int
	}

	results := make(chan result, 1024)
	var wg sync.WaitGroup
	// Limit concurrency — don't open thousands of dirs simultaneously
	sem := make(chan struct{}, 32)

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}

		wg.Add(1)
		go func(pid int, name string) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			fdDir := filepath.Join("/proc", name, "fd")
			fds, err := os.ReadDir(fdDir)
			if err != nil {
				return
			}

			for _, fd := range fds {
				select {
				case <-ctx.Done():
					return
				default:
				}

				link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
				if err != nil {
					continue
				}
				if !strings.HasPrefix(link, "socket:[") {
					continue
				}
				inodeStr := strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")
				inode, err := strconv.ParseUint(inodeStr, 10, 64)
				if err != nil {
					continue
				}
				select {
				case results <- result{inode: inode, pid: pid}:
				case <-ctx.Done():
					return
				}
			}
		}(pid, e.Name())
	}

	// Close results when all goroutines finish
	go func() {
		wg.Wait()
		close(results)
	}()

	m := make(map[uint64]int)
	for r := range results {
		m[r.inode] = r.pid
	}
	return m, nil
}

func readProcessName(pid int) (name, cmdline string) {
	commBytes, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err == nil {
		name = strings.TrimSpace(string(commBytes))
	}
	cmdBytes, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err == nil {
		for i, b := range cmdBytes {
			if b == 0 {
				cmdBytes[i] = ' '
			}
		}
		full := strings.TrimSpace(string(cmdBytes))
		if len(full) > 256 {
			full = full[:256] + "…"
		}
		cmdline = full
	}
	return
}
