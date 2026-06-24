// Package disks provides disk, partition, and LVM information and operations.
package disks

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

type BlockDevice struct {
	Name       string        `json:"name"`
	Path       string        `json:"path"`
	Type       string        `json:"type"`
	Size       int64         `json:"size"`
	SizeHuman  string        `json:"size_human"`
	FSType     string        `json:"fstype"`
	MountPoint string        `json:"mountpoint"`
	Label      string        `json:"label"`
	Model      string        `json:"model"`
	Rota       bool          `json:"rota"`
	Children   []BlockDevice `json:"children"`
}

type MountUsage struct {
	MountPoint string  `json:"mountpoint"`
	Total      int64   `json:"total"`
	Used       int64   `json:"used"`
	Free       int64   `json:"free"`
	UsePercent float64 `json:"use_percent"`
	TotalHuman string  `json:"total_human"`
	UsedHuman  string  `json:"used_human"`
	FreeHuman  string  `json:"free_human"`
	FSType     string  `json:"fstype"`
	Device     string  `json:"device"`
}

type PhysicalVolume struct {
	Name  string `json:"name"`
	VGName string `json:"vg_name"`
	Size  int64  `json:"size"`
	Free  int64  `json:"free"`
	SizeH string `json:"size_human"`
}

type LogicalVolume struct {
	Name       string      `json:"name"`
	FullName   string      `json:"full_name"`
	VGName     string      `json:"vg_name"`
	Size       int64       `json:"size"`
	SizeH      string      `json:"size_human"`
	MountPoint string      `json:"mountpoint"`
	FSType     string      `json:"fstype"`
	Usage      *MountUsage `json:"usage,omitempty"`
	Extendable bool        `json:"extendable"`
}

type VolumeGroup struct {
	Name    string          `json:"name"`
	Size    int64           `json:"size"`
	Free    int64           `json:"free"`
	Used    int64           `json:"used"`
	SizeH   string          `json:"size_human"`
	FreeH   string          `json:"free_human"`
	UsedH   string          `json:"used_human"`
	PVCount int             `json:"pv_count"`
	LVCount int             `json:"lv_count"`
	Volumes []LogicalVolume `json:"volumes"`
}

type DiskSummary struct {
	Devices         []BlockDevice    `json:"devices"`
	Mounts          []MountUsage     `json:"mounts"`
	VolumeGroups    []VolumeGroup    `json:"volume_groups"`
	PhysicalVols    []PhysicalVolume `json:"physical_volumes"`
	LVMAvailable    bool             `json:"lvm_available"`
	TotalDisks      int              `json:"total_disks"`
	TotalPartitions int              `json:"total_partitions"`
}

type ExtendOptions struct {
	LVPath string  `json:"lv_path"`
	SizeGB float64 `json:"size_gb"`
	FSType string  `json:"fstype"`
}

// Scan returns a full disk summary.
func Scan() (*DiskSummary, error) {
	summary := &DiskSummary{
		Devices:      []BlockDevice{},
		Mounts:       []MountUsage{},
		VolumeGroups: []VolumeGroup{},
		PhysicalVols: []PhysicalVolume{},
	}

	devices, err := listBlockDevices()
	if err == nil {
		summary.Devices = devices
		for _, d := range devices {
			if d.Type == "disk" {
				summary.TotalDisks++
			}
			summary.TotalPartitions += countParts(d)
		}
	}

	summary.Mounts = listMounts()

	summary.LVMAvailable = commandExists("lvs")
	if summary.LVMAvailable {
		pvs, _ := listPVs()
		summary.PhysicalVols = pvs

		vgs, _ := listVGs()
		lvs, _ := listLVs()

		// Build mount lookup by device path and mount point
		mountMap := make(map[string]*MountUsage)
		for i := range summary.Mounts {
			m := &summary.Mounts[i]
			mountMap[m.Device] = m
			mountMap[m.MountPoint] = m
		}

		// Enrich LVs with usage data
		lvsByVG := make(map[string][]LogicalVolume)
		for _, lv := range lvs {
			if m, ok := mountMap[lv.FullName]; ok {
				lv.Usage = m
				lv.FSType = m.FSType
				lv.MountPoint = m.MountPoint
			} else if mp := getMountPoint(lv.FullName); mp != "" {
				lv.MountPoint = mp
				if m2, ok := mountMap[mp]; ok {
					lv.Usage = m2
					lv.FSType = m2.FSType
				}
			}
			if lv.FSType == "" {
				lv.FSType = detectFSType(lv.FullName)
			}
			lvsByVG[lv.VGName] = append(lvsByVG[lv.VGName], lv)
		}

		for i := range vgs {
			vgs[i].Volumes = lvsByVG[vgs[i].Name]
			if vgs[i].Free > 0 {
				for j := range vgs[i].Volumes {
					vgs[i].Volumes[j].Extendable = true
				}
			}
		}
		summary.VolumeGroups = vgs
	}

	return summary, nil
}

// Extend extends an LV and resizes its filesystem, streaming output.
func Extend(ctx context.Context, opts ExtendOptions, out chan<- string) error {
	if opts.LVPath == "" || opts.SizeGB <= 0 {
		return fmt.Errorf("lv_path and size_gb are required")
	}
	if opts.FSType == "" {
		opts.FSType = detectFSType(opts.LVPath)
	}

	switch opts.FSType {
	case "ext2", "ext3", "ext4", "xfs", "btrfs":
	default:
		return fmt.Errorf("unsupported filesystem %q — supported: ext3, ext4, xfs, btrfs", opts.FSType)
	}

	sizeStr := fmt.Sprintf("+%.2fG", opts.SizeGB)
	out <- fmt.Sprintf("[webux] Extending %s by %s (fstype: %s)", opts.LVPath, sizeStr, opts.FSType)

	// Step 1: lvextend
	out <- fmt.Sprintf("[webux] $ lvextend -L %s %s", sizeStr, opts.LVPath)
	if err := streamCmd(ctx, out, "lvextend", "-L", sizeStr, opts.LVPath); err != nil {
		return fmt.Errorf("lvextend: %w", err)
	}

	// Step 2: resize filesystem
	switch opts.FSType {
	case "ext2", "ext3", "ext4":
		out <- fmt.Sprintf("[webux] $ resize2fs %s", opts.LVPath)
		if err := streamCmd(ctx, out, "resize2fs", opts.LVPath); err != nil {
			return fmt.Errorf("resize2fs: %w", err)
		}
	case "xfs":
		mp := getMountPoint(opts.LVPath)
		if mp == "" {
			return fmt.Errorf("cannot find mount point for %s (must be mounted for xfs_growfs)", opts.LVPath)
		}
		out <- fmt.Sprintf("[webux] $ xfs_growfs %s", mp)
		if err := streamCmd(ctx, out, "xfs_growfs", mp); err != nil {
			return fmt.Errorf("xfs_growfs: %w", err)
		}
	case "btrfs":
		mp := getMountPoint(opts.LVPath)
		if mp == "" {
			return fmt.Errorf("cannot find mount point for %s", opts.LVPath)
		}
		out <- fmt.Sprintf("[webux] $ btrfs filesystem resize max %s", mp)
		if err := streamCmd(ctx, out, "btrfs", "filesystem", "resize", "max", mp); err != nil {
			return fmt.Errorf("btrfs resize: %w", err)
		}
	}

	out <- fmt.Sprintf("[webux] ✓ %s extended and filesystem resized successfully", opts.LVPath)
	return nil
}

// ── lsblk ─────────────────────────────────────────────────────────────────

type lsblkDev struct {
	Name        string      `json:"name"`
	Path        string      `json:"path"`
	Type        string      `json:"type"`
	Size        interface{} `json:"size"`
	FSType      *string     `json:"fstype"`
	Mountpoint  *string     `json:"mountpoint"`   // older lsblk
	Mountpoints []string    `json:"mountpoints"`  // newer lsblk (array)
	Label       *string     `json:"label"`
	Model       *string     `json:"model"`
	Rota        interface{} `json:"rota"`
	Children    []lsblkDev  `json:"children"`
}

func listBlockDevices() ([]BlockDevice, error) {
	// Try with --bytes first, fall back without if it fails
	out, err := exec.Command("lsblk", "--json",
		"--output", "NAME,PATH,TYPE,SIZE,FSTYPE,MOUNTPOINT,MOUNTPOINTS,LABEL,MODEL,ROTA",
	).Output()
	if err != nil {
		return nil, err
	}
	var raw struct {
		Blockdevices []lsblkDev `json:"blockdevices"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	var devs []BlockDevice
	for _, d := range raw.Blockdevices {
		devs = append(devs, toLsblk(d))
	}
	return devs, nil
}

func toLsblk(d lsblkDev) BlockDevice {
	bd := BlockDevice{Name: d.Name, Path: d.Path, Type: d.Type, Children: []BlockDevice{}}

	// Size: handle both "953.9G" strings and raw byte integers
	switch v := d.Size.(type) {
	case float64:
		bd.Size = int64(v)
		bd.SizeHuman = fmtBytes(bd.Size)
	case string:
		// Try parsing as integer bytes first
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			bd.Size = n
			bd.SizeHuman = fmtBytes(n)
		} else {
			// Human-readable like "953.9G" — keep as-is for display
			bd.SizeHuman = v
		}
	}

	if d.FSType != nil { bd.FSType = *d.FSType }

	// mountpoints (array, newer lsblk) takes priority over mountpoint (string)
	if len(d.Mountpoints) > 0 {
		for _, mp := range d.Mountpoints {
			if mp != "" && mp != "[SWAP]" {
				bd.MountPoint = mp
				break
			}
		}
	} else if d.Mountpoint != nil {
		bd.MountPoint = *d.Mountpoint
	}

	if d.Label != nil { bd.Label = *d.Label }
	if d.Model != nil { bd.Model = strings.TrimSpace(*d.Model) }

	switch v := d.Rota.(type) {
	case bool:   bd.Rota = v
	case string: bd.Rota = v == "1" || v == "true"
	}

	for _, c := range d.Children {
		bd.Children = append(bd.Children, toLsblk(c))
	}
	return bd
}

func countParts(d BlockDevice) int {
	n := 0
	for _, c := range d.Children {
		if c.Type == "part" { n++ }
		n += countParts(c)
	}
	return n
}

// ── df ────────────────────────────────────────────────────────────────────

func listMounts() []MountUsage {
	out, err := exec.Command("df", "-B1",
		"--output=source,fstype,size,used,avail,pcent,target").Output()
	if err != nil {
		return nil
	}
	var mounts []MountUsage
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	first := true
	for scanner.Scan() {
		if first { first = false; continue }
		f := strings.Fields(scanner.Text())
		if len(f) < 7 || isVirtualFS(f[1]) { continue }
		total, _ := strconv.ParseInt(f[2], 10, 64)
		used, _  := strconv.ParseInt(f[3], 10, 64)
		free, _  := strconv.ParseInt(f[4], 10, 64)
		pct := 0.0
		if total > 0 { pct = float64(used) / float64(total) * 100 }
		mounts = append(mounts, MountUsage{
			Device: f[0], FSType: f[1],
			Total: total, Used: used, Free: free, UsePercent: pct,
			TotalHuman: fmtBytes(total), UsedHuman: fmtBytes(used), FreeHuman: fmtBytes(free),
			MountPoint: f[6],
		})
	}
	return mounts
}

func isVirtualFS(fs string) bool {
	for _, v := range []string{
		"tmpfs","devtmpfs","sysfs","proc","devpts","cgroup","cgroup2",
		"pstore","efivarfs","bpf","tracefs","debugfs","hugetlbfs",
		"mqueue","fusectl","overlay","squashfs","securityfs","autofs",
	} {
		if fs == v { return true }
	}
	return false
}

// ── LVM ───────────────────────────────────────────────────────────────────

func listPVs() ([]PhysicalVolume, error) {
	out, err := exec.Command("pvs",
		"--reportformat", "json", "--units", "b", "--nosuffix",
		"-o", "pv_name,vg_name,pv_size,pv_free",
	).Output()
	if err != nil { return nil, err }

	var raw struct {
		Report []struct {
			PV []struct {
				PVName string `json:"pv_name"`
				VGName string `json:"vg_name"`
				PVSize string `json:"pv_size"`
				PVFree string `json:"pv_free"`
			} `json:"pv"`
		} `json:"report"`
	}
	if err := json.Unmarshal(out, &raw); err != nil { return nil, err }

	var pvs []PhysicalVolume
	for _, r := range raw.Report {
		for _, p := range r.PV {
			size := parseBytes(p.PVSize)
			pvs = append(pvs, PhysicalVolume{
				Name: p.PVName, VGName: p.VGName,
				Size: size, Free: parseBytes(p.PVFree), SizeH: fmtBytes(size),
			})
		}
	}
	return pvs, nil
}

func listVGs() ([]VolumeGroup, error) {
	out, err := exec.Command("vgs",
		"--reportformat", "json", "--units", "b", "--nosuffix",
		"-o", "vg_name,vg_size,vg_free,pv_count,lv_count",
	).Output()
	if err != nil { return nil, err }

	var raw struct {
		Report []struct {
			VG []struct {
				VGName  string `json:"vg_name"`
				VGSize  string `json:"vg_size"`
				VGFree  string `json:"vg_free"`
				PVCount string `json:"pv_count"`
				LVCount string `json:"lv_count"`
			} `json:"vg"`
		} `json:"report"`
	}
	if err := json.Unmarshal(out, &raw); err != nil { return nil, err }

	var vgs []VolumeGroup
	for _, r := range raw.Report {
		for _, v := range r.VG {
			size := parseBytes(v.VGSize)
			free := parseBytes(v.VGFree)
			pvc, _ := strconv.Atoi(v.PVCount)
			lvc, _ := strconv.Atoi(v.LVCount)
			vgs = append(vgs, VolumeGroup{
				Name: v.VGName, Size: size, Free: free, Used: size - free,
				SizeH: fmtBytes(size), FreeH: fmtBytes(free), UsedH: fmtBytes(size-free),
				PVCount: pvc, LVCount: lvc,
			})
		}
	}
	return vgs, nil
}

func listLVs() ([]LogicalVolume, error) {
	out, err := exec.Command("lvs",
		"--reportformat", "json", "--units", "b", "--nosuffix",
		"-o", "lv_name,vg_name,lv_size,lv_dm_path",
	).Output()
	if err != nil { return nil, err }

	var raw struct {
		Report []struct {
			LV []struct {
				LVName   string `json:"lv_name"`
				VGName   string `json:"vg_name"`
				LVSize   string `json:"lv_size"`
				LVDMPath string `json:"lv_dm_path"`
			} `json:"lv"`
		} `json:"report"`
	}
	if err := json.Unmarshal(out, &raw); err != nil { return nil, err }

	var lvs []LogicalVolume
	for _, r := range raw.Report {
		for _, l := range r.LV {
			size := parseBytes(l.LVSize)
			lvs = append(lvs, LogicalVolume{
				Name: l.LVName, VGName: l.VGName,
				FullName: "/dev/" + l.VGName + "/" + l.LVName,
				Size: size, SizeH: fmtBytes(size),
			})
		}
	}
	return lvs, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────

func streamCmd(ctx context.Context, out chan<- string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", name, err)
	}
	scanner := bufio.NewScanner(io.MultiReader(stdout, stderr))
	for scanner.Scan() {
		select {
		case out <- scanner.Text():
		case <-ctx.Done():
			cmd.Process.Kill()
			return ctx.Err()
		}
	}
	return cmd.Wait()
}

func detectFSType(device string) string {
	out, err := exec.Command("blkid", "-o", "value", "-s", "TYPE", device).Output()
	if err != nil { return "" }
	return strings.TrimSpace(string(out))
}

func getMountPoint(device string) string {
	out, err := exec.Command("findmnt", "-n", "-o", "TARGET", device).Output()
	if err == nil { return strings.TrimSpace(string(out)) }
	// fallback: /proc/mounts
	out2, err2 := exec.Command("grep", device, "/proc/mounts").Output()
	if err2 != nil { return "" }
	if f := strings.Fields(string(out2)); len(f) >= 2 { return f[1] }
	return ""
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func parseBytes(s string) int64 {
	s = strings.TrimRight(strings.TrimSpace(s), "B ")
	v, _ := strconv.ParseFloat(s, 64)
	return int64(v)
}

func fmtBytes(b int64) string {
	if b <= 0 { return "0 B" }
	const u = 1024
	if b < u { return fmt.Sprintf("%d B", b) }
	div, exp := int64(u), 0
	for n := b / u; n >= u; n /= u { div *= u; exp++ }
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
