// Package vms provides KVM/QEMU and Proxmox VM listing and management.
package vms

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type Backend string

const (
	BackendProxmox Backend = "proxmox"
	BackendLibvirt Backend = "libvirt"
	BackendNone    Backend = ""
)

type VM struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	State    string  `json:"state"`
	MemoryMB int64   `json:"memory_mb"`
	VCPUs    int     `json:"vcpus"`
	Disk     string  `json:"disk,omitempty"`
	Backend  Backend `json:"backend"`
}

type Info struct {
	Available bool    `json:"available"`
	Backend   Backend `json:"backend"`
	Version   string  `json:"version,omitempty"`
}

func Detect() Info {
	if _, err := exec.LookPath("qm"); err == nil {
		return Info{Available: true, Backend: BackendProxmox}
	}
	if _, err := exec.LookPath("virsh"); err == nil {
		return Info{Available: true, Backend: BackendLibvirt}
	}
	return Info{Available: false, Backend: BackendNone}
}

func List() ([]VM, error) {
	info := Detect()
	switch info.Backend {
	case BackendProxmox:
		return listProxmox()
	case BackendLibvirt:
		return listLibvirt()
	}
	return []VM{}, nil
}

func Action(id, action string) error {
	info := Detect()
	switch info.Backend {
	case BackendProxmox:
		return actionProxmox(id, action)
	case BackendLibvirt:
		return actionLibvirt(id, action)
	}
	return fmt.Errorf("no hypervisor available")
}

// ── Proxmox ───────────────────────────────────────────────────────────────────

func listProxmox() ([]VM, error) {
	node, _ := os.Hostname()

	// Single pvesh call returns vmid, name, status, maxmem (bytes), cpus — no per-VM follow-up needed.
	out, err := exec.Command("pvesh", "get", "/nodes/"+node+"/qemu",
		"--output-format", "json").Output()
	if err != nil {
		// pvesh unavailable or failed — fall back to qm list (no vCPU info)
		return listProxmoxFallback()
	}

	var raw []struct {
		VMID   int    `json:"vmid"`
		Name   string `json:"name"`
		Status string `json:"status"`
		MaxMem int64  `json:"maxmem"` // bytes
		CPUs   int    `json:"cpus"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return listProxmoxFallback()
	}

	vms := make([]VM, 0, len(raw))
	for _, r := range raw {
		vms = append(vms, VM{
			ID:       strconv.Itoa(r.VMID),
			Name:     r.Name,
			State:    normalizeState(r.Status),
			MemoryMB: r.MaxMem / 1024 / 1024,
			VCPUs:    r.CPUs,
			Backend:  BackendProxmox,
		})
	}
	return vms, nil
}

// qm list columns: VMID NAME STATUS MEM(MB) BOOTDISK(GB) PID
var qmListRe = regexp.MustCompile(`^\s*(\d+)\s+(\S+)\s+(\S+)\s+(\d+)\s+(\S+)\s+(\d+)`)

func listProxmoxFallback() ([]VM, error) {
	out, err := exec.Command("qm", "list").Output()
	if err != nil {
		return nil, fmt.Errorf("qm list: %w", err)
	}
	var vms []VM
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		m := qmListRe.FindStringSubmatch(scanner.Text())
		if m == nil {
			continue
		}
		mem, _ := strconv.ParseInt(m[4], 10, 64)
		vms = append(vms, VM{
			ID:       m[1],
			Name:     m[2],
			State:    normalizeState(m[3]),
			MemoryMB: mem,
			Backend:  BackendProxmox,
		})
	}
	return vms, nil
}

func actionProxmox(id, action string) error {
	var args []string
	switch action {
	case "start":
		args = []string{"start", id}
	case "stop":
		args = []string{"stop", id}
	case "reboot":
		args = []string{"reboot", id}
	case "force-stop":
		args = []string{"stop", id, "--skiplock"}
	case "suspend":
		args = []string{"suspend", id}
	case "resume":
		args = []string{"resume", id}
	default:
		return fmt.Errorf("unknown action: %s", action)
	}
	cmd := exec.Command("qm", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ── Libvirt / KVM ─────────────────────────────────────────────────────────────

func listLibvirt() ([]VM, error) {
	out, err := exec.Command("virsh", "list", "--all").Output()
	if err != nil {
		return nil, fmt.Errorf("virsh list: %w", err)
	}
	var names []string
	var states []string
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		// skip header and separator
		if strings.HasPrefix(line, " Id") || strings.HasPrefix(line, "---") || strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		// fields: Id  Name  State...
		nameIdx := 1
		stateIdx := 2
		names = append(names, fields[nameIdx])
		// state can be two words "shut off"
		state := strings.Join(fields[stateIdx:], " ")
		states = append(states, state)
	}

	vms := make([]VM, 0, len(names))
	for i, name := range names {
		vm := VM{
			Name:    name,
			ID:      name,
			State:   normalizeState(states[i]),
			Backend: BackendLibvirt,
		}
		// enrich from dominfo
		if info, err := exec.Command("virsh", "dominfo", name).Output(); err == nil {
			for _, line := range strings.Split(string(info), "\n") {
				kv := strings.SplitN(line, ":", 2)
				if len(kv) != 2 {
					continue
				}
				key := strings.TrimSpace(kv[0])
				val := strings.TrimSpace(kv[1])
				switch key {
				case "Max memory":
					// "2097152 KiB"
					parts := strings.Fields(val)
					if len(parts) >= 2 && parts[1] == "KiB" {
						kb, _ := strconv.ParseInt(parts[0], 10, 64)
						vm.MemoryMB = kb / 1024
					}
				case "CPU(s)":
					vm.VCPUs, _ = strconv.Atoi(val)
				}
			}
		}
		vms = append(vms, vm)
	}
	return vms, nil
}

func actionLibvirt(name, action string) error {
	var args []string
	switch action {
	case "start":
		args = []string{"start", name}
	case "stop":
		args = []string{"shutdown", name}
	case "reboot":
		args = []string{"reboot", name}
	case "force-stop":
		args = []string{"destroy", name}
	case "suspend":
		args = []string{"suspend", name}
	case "resume":
		args = []string{"resume", name}
	default:
		return fmt.Errorf("unknown action: %s", action)
	}
	cmd := exec.Command("virsh", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ── Proxmox cluster ───────────────────────────────────────────────────────────

// ClusterVM represents a VM reported by the Proxmox cluster API.
type ClusterVM struct {
	ID     string `json:"id"`     // e.g. "qemu/100"
	VMID   int    `json:"vmid"`
	Name   string `json:"name"`
	Node   string `json:"node"`
	State  string `json:"state"`
	Local  bool   `json:"local"` // true when node == this host
}

// ListCluster calls pvesh to get all VMs/CTs across the cluster.
// Returns nil if pvesh is not available or the call fails.
func ListCluster() ([]ClusterVM, error) {
	if _, err := exec.LookPath("pvesh"); err != nil {
		return nil, fmt.Errorf("pvesh not available")
	}
	// pvesh outputs one JSON object per line (JSONL) with --output-format=json-pretty or
	// a JSON array with --output-format=json
	out, err := exec.Command("pvesh", "get", "/cluster/resources",
		"--type", "vm", "--output-format", "json").Output()
	if err != nil {
		return nil, fmt.Errorf("pvesh: %w", err)
	}

	// Parse JSON array: each element has vmid, name, node, status, type (qemu|lxc)
	var raw []struct {
		ID     string `json:"id"`
		VMID   int    `json:"vmid"`
		Name   string `json:"name"`
		Node   string `json:"node"`
		Status string `json:"status"`
		Type   string `json:"type"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	hostname, _ := os.Hostname()
	var vms []ClusterVM
	for _, r := range raw {
		vms = append(vms, ClusterVM{
			ID:    r.ID,
			VMID:  r.VMID,
			Name:  r.Name,
			Node:  r.Node,
			State: normalizeState(r.Status),
			Local: r.Node == hostname,
		})
	}
	return vms, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func normalizeState(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "running":
		return "running"
	case "stopped", "shut off", "shutoff":
		return "stopped"
	case "paused":
		return "paused"
	case "suspended":
		return "suspended"
	case "prelaunch":
		return "starting"
	default:
		return strings.ToLower(raw)
	}
}
