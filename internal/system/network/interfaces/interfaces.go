// Package interfaces reads network interface data from /sys/class/net and
// /proc/net/dev. Uses netlink syscalls for address/route data where possible,
// falling back to /proc files. Zero external dependencies.
package interfaces

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Interface represents a network interface with all its properties.
type Interface struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`         // ethernet, loopback, bridge, bond, vlan, veth, tun, dummy
	State       string   `json:"state"`        // up, down, unknown
	Flags       []string `json:"flags"`        // UP, BROADCAST, RUNNING, MULTICAST, etc.
	MAC         string   `json:"mac"`
	MTU         int      `json:"mtu"`
	Addresses   []Addr   `json:"addresses"`
	RxBytes     uint64   `json:"rx_bytes"`
	TxBytes     uint64   `json:"tx_bytes"`
	RxPackets   uint64   `json:"rx_packets"`
	TxPackets   uint64   `json:"tx_packets"`
	RxErrors    uint64   `json:"rx_errors"`
	TxErrors    uint64   `json:"tx_errors"`
	Speed       string   `json:"speed"`        // e.g. "1000" Mbps, "" if unknown
	Duplex      string   `json:"duplex"`       // full, half, unknown
	Driver      string   `json:"driver"`       // kernel module name
	MasterIface string   `json:"master,omitempty"` // bond/bridge master
	VLANOf      string   `json:"vlan_of,omitempty"`
	VLANID      int      `json:"vlan_id,omitempty"`
}

// Addr is an IP address assigned to an interface.
type Addr struct {
	IP      string `json:"ip"`
	Prefix  int    `json:"prefix"`
	Family  string `json:"family"` // inet | inet6
	Scope   string `json:"scope"`  // global | link | host
}

// Route represents a kernel routing table entry.
type Route struct {
	Destination string `json:"destination"`
	Gateway     string `json:"gateway"`
	Iface       string `json:"iface"`
	Metric      int    `json:"metric"`
	Flags       string `json:"flags"`
	Family      string `json:"family"` // inet | inet6
}

// Manager handles interface reads and writes.
type Manager struct{}

func NewManager() *Manager { return &Manager{} }

// List returns all network interfaces on the host.
func (m *Manager) List() ([]Interface, error) {
	// Get interface list from the kernel via net.Interfaces()
	// This uses a netlink GETLINK call internally — no subprocess.
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list interfaces: %w", err)
	}

	// Read stats from /proc/net/dev for all interfaces at once
	statsMap, _ := readProcNetDev()

	var result []Interface
	for _, iface := range ifaces {
		ni := Interface{
			Name:  iface.Name,
			MAC:   iface.HardwareAddr.String(),
			MTU:   iface.MTU,
			Flags: parseFlags(iface.Flags),
			State: ifaceState(iface.Flags),
			Type:  detectType(iface.Name),
		}

		// IP addresses via netlink (net.Interface.Addrs)
		addrs, err := iface.Addrs()
		if err == nil {
			for _, a := range addrs {
				ipNet, ok := a.(*net.IPNet)
				if !ok {
					continue
				}
				prefix, _ := ipNet.Mask.Size()
				family := "inet"
				if ipNet.IP.To4() == nil {
					family = "inet6"
				}
				ni.Addresses = append(ni.Addresses, Addr{
					IP:     ipNet.IP.String(),
					Prefix: prefix,
					Family: family,
					Scope:  addrScope(ipNet.IP),
				})
			}
		}

		// Stats from /proc/net/dev
		if s, ok := statsMap[iface.Name]; ok {
			ni.RxBytes   = s[0]
			ni.RxPackets = s[1]
			ni.RxErrors  = s[2]
			ni.TxBytes   = s[8]
			ni.TxPackets = s[9]
			ni.TxErrors  = s[10]
		}

		// Extra info from /sys/class/net/<name>/
		ni.Speed  = readSysStr(iface.Name, "speed")
		// Kernel reports -1 for virtual/unknown interfaces — treat as unknown
		if ni.Speed == "-1" || ni.Speed == "4294967295" {
			ni.Speed = ""
		}
		ni.Duplex = readSysStr(iface.Name, "duplex")
		ni.Driver = readDriver(iface.Name)
		ni.MasterIface = readSysStr(iface.Name, "master")
		ni.VLANOf, ni.VLANID = readVLANInfo(iface.Name)

		result = append(result, ni)
	}
	return result, nil
}

// Routes returns the kernel routing table.
func (m *Manager) Routes() ([]Route, error) {
	var routes []Route

	// IPv4 from /proc/net/route
	v4, err := readProcNetRoute()
	if err == nil {
		routes = append(routes, v4...)
	}

	// IPv6 from /proc/net/ipv6_route
	v6, err := readProcNetIPv6Route()
	if err == nil {
		routes = append(routes, v6...)
	}

	return routes, nil
}

// SetUp brings an interface up. Returns CLI equivalent.
func (m *Manager) SetUp(name string) (string, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return "", fmt.Errorf("interface %s not found: %w", name, err)
	}
	_ = iface
	// Use ip link set — this is the standard tool, universally available
	cmd := fmt.Sprintf("ip link set %s up", name)
	if err := exec.Command("ip", "link", "set", name, "up").Run(); err != nil {
		return "", fmt.Errorf("ip link set up: %w", err)
	}
	return cmd, nil
}

// SetDown brings an interface down. Returns CLI equivalent.
func (m *Manager) SetDown(name string) (string, error) {
	cmd := fmt.Sprintf("ip link set %s down", name)
	if err := exec.Command("ip", "link", "set", name, "down").Run(); err != nil {
		return "", fmt.Errorf("ip link set down: %w", err)
	}
	return cmd, nil
}

// AddAddress assigns an IP address to an interface. Returns CLI equivalent.
func (m *Manager) AddAddress(name, cidr string) (string, error) {
	cmd := fmt.Sprintf("ip addr add %s dev %s", cidr, name)
	if err := exec.Command("ip", "addr", "add", cidr, "dev", name).Run(); err != nil {
		return "", fmt.Errorf("ip addr add: %w", err)
	}
	return cmd, nil
}

// DelAddress removes an IP address from an interface. Returns CLI equivalent.
func (m *Manager) DelAddress(name, cidr string) (string, error) {
	cmd := fmt.Sprintf("ip addr del %s dev %s", cidr, name)
	if err := exec.Command("ip", "addr", "del", cidr, "dev", name).Run(); err != nil {
		return "", fmt.Errorf("ip addr del: %w", err)
	}
	return cmd, nil
}

// SetMTU changes the MTU of an interface. Returns CLI equivalent.
func (m *Manager) SetMTU(name string, mtu int) (string, error) {
	cmd := fmt.Sprintf("ip link set %s mtu %d", name, mtu)
	if err := exec.Command("ip", "link", "set", name, "mtu", strconv.Itoa(mtu)).Run(); err != nil {
		return "", fmt.Errorf("ip link set mtu: %w", err)
	}
	return cmd, nil
}

// --- /proc and /sys readers -------------------------------------------------

// readProcNetDev parses /proc/net/dev into a map of iface → []uint64 counters.
// Counter order: rx_bytes rx_packets rx_errs rx_drop rx_fifo rx_frame
//                rx_compressed rx_multicast
//                tx_bytes tx_packets tx_errs tx_drop tx_fifo tx_colls tx_carrier tx_compressed
func readProcNetDev() (map[string][]uint64, error) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	m := make(map[string][]uint64)
	scanner := bufio.NewScanner(f)
	scanner.Scan() // header line 1
	scanner.Scan() // header line 2

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}
		name := strings.TrimSpace(line[:colonIdx])
		fields := strings.Fields(line[colonIdx+1:])
		var vals []uint64
		for _, f := range fields {
			v, _ := strconv.ParseUint(f, 10, 64)
			vals = append(vals, v)
		}
		m[name] = vals
	}
	return m, nil
}

func readProcNetRoute() ([]Route, error) {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var routes []Route
	scanner := bufio.NewScanner(f)
	scanner.Scan() // header

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 11 {
			continue
		}
		iface := fields[0]
		dest := hexToIPv4(fields[1])
		gw := hexToIPv4(fields[2])
		flags := fields[3]
		metric, _ := strconv.Atoi(fields[6])
		mask := hexToIPv4(fields[7])

		prefix := maskToPrefix(mask)
		destination := dest + "/" + strconv.Itoa(prefix)
		if dest == "0.0.0.0" {
			destination = "default"
		}

		routes = append(routes, Route{
			Destination: destination,
			Gateway:     gw,
			Iface:       iface,
			Metric:      metric,
			Flags:       flags,
			Family:      "inet",
		})
	}
	return routes, nil
}

func readProcNetIPv6Route() ([]Route, error) {
	f, err := os.Open("/proc/net/ipv6_route")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var routes []Route
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 {
			continue
		}
		dest := hexToIPv6(fields[0])
		prefixLen, _ := strconv.ParseInt(fields[1], 16, 32)
		gw := hexToIPv6(fields[4])
		metric, _ := strconv.ParseInt(fields[5], 16, 32)
		iface := fields[9]

		destination := fmt.Sprintf("%s/%d", dest, prefixLen)
		if dest == "::" && prefixLen == 0 {
			destination = "default"
		}

		routes = append(routes, Route{
			Destination: destination,
			Gateway:     gw,
			Iface:       iface,
			Metric:      int(metric),
			Family:      "inet6",
		})
	}
	return routes, nil
}

func readSysStr(iface, key string) string {
	b, err := os.ReadFile(fmt.Sprintf("/sys/class/net/%s/%s", iface, key))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func readDriver(iface string) string {
	// /sys/class/net/<iface>/device/driver is a symlink to the kernel module
	link, err := os.Readlink(fmt.Sprintf("/sys/class/net/%s/device/driver", iface))
	if err != nil {
		return ""
	}
	parts := strings.Split(link, "/")
	return parts[len(parts)-1]
}

func readVLANInfo(iface string) (parent string, vid int) {
	// VLAN interfaces expose their info in /proc/net/vlan/<name>
	f, err := os.Open(fmt.Sprintf("/proc/net/vlan/%s", iface))
	if err != nil {
		return "", 0
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "VID:") {
			fields := strings.Fields(line)
			for i, f := range fields {
				if f == "VID:" && i+1 < len(fields) {
					vid, _ = strconv.Atoi(fields[i+1])
				}
				if f == "Device:" && i+1 < len(fields) {
					parent = fields[i+1]
				}
			}
		}
	}
	return parent, vid
}

// --- helpers ----------------------------------------------------------------

func detectType(name string) string {
	switch {
	case name == "lo":
		return "loopback"
	case strings.HasPrefix(name, "eth") || strings.HasPrefix(name, "en"):
		return "ethernet"
	case strings.HasPrefix(name, "wlan") || strings.HasPrefix(name, "wl"):
		return "wireless"
	case strings.HasPrefix(name, "br"):
		return "bridge"
	case strings.HasPrefix(name, "bond"):
		return "bond"
	case strings.HasPrefix(name, "veth"):
		return "veth"
	case strings.HasPrefix(name, "tun") || strings.HasPrefix(name, "tap"):
		return "tun"
	case strings.HasPrefix(name, "docker") || strings.HasPrefix(name, "virbr"):
		return "bridge"
	case strings.Contains(name, "."):
		return "vlan"
	case strings.HasPrefix(name, "dummy"):
		return "dummy"
	default:
		return "unknown"
	}
}

func ifaceState(flags net.Flags) string {
	if flags&net.FlagUp != 0 && flags&net.FlagRunning != 0 {
		return "up"
	}
	if flags&net.FlagUp != 0 {
		return "no-carrier"
	}
	return "down"
}

func parseFlags(flags net.Flags) []string {
	var out []string
	if flags&net.FlagUp != 0 { out = append(out, "UP") }
	if flags&net.FlagBroadcast != 0 { out = append(out, "BROADCAST") }
	if flags&net.FlagLoopback != 0 { out = append(out, "LOOPBACK") }
	if flags&net.FlagPointToPoint != 0 { out = append(out, "POINTTOPOINT") }
	if flags&net.FlagMulticast != 0 { out = append(out, "MULTICAST") }
	return out
}

func addrScope(ip net.IP) string {
	if ip.IsLoopback() { return "host" }
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() { return "link" }
	return "global"
}

func hexToIPv4(hexStr string) string {
	val, err := strconv.ParseUint(hexStr, 16, 32)
	if err != nil { return "0.0.0.0" }
	return fmt.Sprintf("%d.%d.%d.%d",
		val&0xff, (val>>8)&0xff, (val>>16)&0xff, (val>>24)&0xff)
}

func hexToIPv6(hexStr string) string {
	if len(hexStr) != 32 { return "::" }
	b := make([]byte, 16)
	for i := 0; i < 16; i++ {
		v, _ := strconv.ParseUint(hexStr[i*2:i*2+2], 16, 8)
		b[i] = byte(v)
	}
	return net.IP(b).String()
}

func maskToPrefix(mask string) int {
	parts := strings.Split(mask, ".")
	if len(parts) != 4 { return 0 }
	var n int
	for _, p := range parts {
		v, _ := strconv.Atoi(p)
		for v > 0 {
			n += v & 1
			v >>= 1
		}
	}
	return n
}
