// Package system detects host capabilities at startup.
package system

import (
	"os"
	"os/exec"
	"strings"
)

// HostInfo contains detected properties of the host.
type HostInfo struct {
	Distro     string
	Arch       string
	Kernel     string
	InitSystem string // "systemd" | "openrc" | "sysvinit" | "unknown"

	// Optional tools present on PATH
	HasDocker        bool
	HasPodman        bool
	HasAnsible       bool
	HasPuppet        bool
	HasFacter        bool
	HasUFW           bool
	HasNFTables      bool
	HasIPTables      bool
}

// Detect probes the host and returns a HostInfo.
func Detect() (*HostInfo, error) {
	h := &HostInfo{}

	// Distro
	if data, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				h.Distro = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
			}
		}
	}
	if h.Distro == "" {
		h.Distro = "unknown"
	}

	// Kernel
	if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		h.Kernel = strings.TrimSpace(string(b))
	}

	// Arch
	if out, err := exec.Command("uname", "-m").Output(); err == nil {
		h.Arch = strings.TrimSpace(string(out))
	}

	// Init system
	h.InitSystem = detectInitSystem()

	// Optional tools
	h.HasDocker = commandExists("docker")
	h.HasPodman = commandExists("podman")
	h.HasAnsible = commandExists("ansible-playbook")
	h.HasPuppet = commandExists("puppet")
	h.HasFacter = commandExists("facter")
	h.HasUFW = commandExists("ufw")
	h.HasNFTables = commandExists("nft")
	h.HasIPTables = commandExists("iptables")

	return h, nil
}

func detectInitSystem() string {
	// Systemd: PID 1 is systemd or /run/systemd/private exists
	if _, err := os.Stat("/run/systemd/private"); err == nil {
		return "systemd"
	}
	if out, err := os.ReadFile("/proc/1/comm"); err == nil {
		comm := strings.TrimSpace(string(out))
		if comm == "systemd" {
			return "systemd"
		}
		if comm == "init" {
			// Could be sysvinit or openrc
			if commandExists("rc-status") {
				return "openrc"
			}
			return "sysvinit"
		}
	}
	return "unknown"
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
