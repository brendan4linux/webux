package servicemap

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// ConntrackEntry represents one entry from the kernel's connection tracking table.
type ConntrackEntry struct {
	Protocol string `json:"protocol"`
	SrcIP    string `json:"src_ip"`
	SrcPort  int    `json:"src_port"`
	DstIP    string `json:"dst_ip"`
	DstPort  int    `json:"dst_port"`
	State    string `json:"state"` // ESTABLISHED, TIME_WAIT, etc. (empty for UDP)
	Bytes    int64  `json:"bytes"`
}

// readConntrack reads /proc/net/nf_conntrack (or /proc/net/ip_conntrack as
// fallback). Returns the entries and whether conntrack is available at all.
func readConntrack() ([]ConntrackEntry, bool) {
	paths := []string{"/proc/net/nf_conntrack", "/proc/net/ip_conntrack"}
	for _, p := range paths {
		entries, err := parseConntrackFile(p)
		if err == nil {
			return entries, true
		}
	}
	return nil, false
}

// parseConntrackFile parses a conntrack file.
// Format (TCP):
//
//	ipv4 2 tcp 6 431999 ESTABLISHED src=A dst=B sport=S dport=D ... bytes=N ...
//
// Format (UDP):
//
//	ipv4 2 udp 17 30 src=A dst=B sport=S dport=D ... bytes=N ...
func parseConntrackFile(path string) ([]ConntrackEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []ConntrackEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		e, ok := parseConntrackLine(sc.Text())
		if ok {
			out = append(out, e)
		}
	}
	return out, sc.Err()
}

func parseConntrackLine(line string) (ConntrackEntry, bool) {
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return ConntrackEntry{}, false
	}

	var e ConntrackEntry

	// Fields: layer3-proto, num, proto-name, proto-num, [timeout], [state], src=..., dst=..., ...
	e.Protocol = fields[2] // "tcp" or "udp"

	// Parse key=value pairs — only take the FIRST occurrence (forward direction)
	srcSeen, dstSeen, sportSeen, dportSeen := false, false, false, false

	for _, f := range fields {
		kv := strings.SplitN(f, "=", 2)
		if len(kv) != 2 {
			// Bare words can be state labels for TCP (e.g. ESTABLISHED)
			if e.Protocol == "tcp" && isTCPState(f) && e.State == "" {
				e.State = f
			}
			continue
		}
		k, v := kv[0], kv[1]
		switch k {
		case "src":
			if !srcSeen {
				e.SrcIP = v
				srcSeen = true
			}
		case "dst":
			if !dstSeen {
				e.DstIP = v
				dstSeen = true
			}
		case "sport":
			if !sportSeen {
				e.SrcPort, _ = strconv.Atoi(v)
				sportSeen = true
			}
		case "dport":
			if !dportSeen {
				e.DstPort, _ = strconv.Atoi(v)
				dportSeen = true
			}
		case "bytes":
			// Take the first bytes= (forward direction)
			if e.Bytes == 0 {
				e.Bytes, _ = strconv.ParseInt(v, 10, 64)
			}
		}
	}

	if e.SrcIP == "" || e.DstIP == "" || e.DstPort == 0 {
		return ConntrackEntry{}, false
	}
	return e, true
}

func isTCPState(s string) bool {
	switch s {
	case "ESTABLISHED", "SYN_SENT", "SYN_RECV", "FIN_WAIT", "FIN_WAIT1",
		"FIN_WAIT2", "TIME_WAIT", "CLOSE", "CLOSE_WAIT", "LAST_ACK",
		"LISTEN", "CLOSING", "NONE":
		return true
	}
	return false
}
