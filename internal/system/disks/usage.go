package disks

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"syscall"
)

const DefaultUsageTopN = 20

// DirUsage is one directory's size within a partition.
type DirUsage struct {
	Path    string  `json:"path"`
	Bytes   int64   `json:"bytes"`
	Human   string  `json:"human"`
	SizePct float64 `json:"size_pct"` // % of partition total
}

// SubDirs returns the largest immediate subdirectories of target,
// each sized with a full recursive walk. The walk is device-locked to
// the filesystem containing root — it will never cross into other mounts.
func SubDirs(root, target string, n int, partitionTotal int64) ([]DirUsage, error) {
	if n <= 0 {
		n = DefaultUsageTopN
	}

	rootDev, err := deviceOf(root)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(target)
	if err != nil {
		return nil, err
	}

	type result struct {
		path  string
		bytes int64
	}
	var results []result

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		childPath := filepath.Join(target, e.Name())
		dev, devErr := deviceOf(childPath)
		if devErr != nil || dev != rootDev {
			continue // different filesystem — skip
		}
		size, _ := dirSize(childPath, rootDev) // errors → 0, still include
		results = append(results, result{path: childPath, bytes: size})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].bytes > results[j].bytes
	})
	if len(results) > n {
		results = results[:n]
	}

	out := make([]DirUsage, len(results))
	for i, r := range results {
		var pct float64
		if partitionTotal > 0 {
			pct = float64(r.bytes) / float64(partitionTotal) * 100
		}
		out[i] = DirUsage{
			Path:    r.path,
			Bytes:   r.bytes,
			Human:   fmtBytes(r.bytes),
			SizePct: pct,
		}
	}
	return out, nil
}

// deviceOf returns the device ID of the given path.
func deviceOf(path string) (uint64, error) {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return 0, err
	}
	return st.Dev, nil
}

// dirSize returns the total byte count of all regular files under dir
// that live on the same device — never crosses mount boundaries.
func dirSize(dir string, dev uint64) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return filepath.SkipDir
		}
		info, err := d.Info()
		if err != nil {
			return filepath.SkipDir
		}
		sys, ok := info.Sys().(*syscall.Stat_t)
		if !ok || sys.Dev != dev {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}
