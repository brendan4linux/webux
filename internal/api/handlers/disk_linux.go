package handlers

import "syscall"

// readDiskUsage returns (used bytes, total bytes) for the filesystem
// containing the given path, using a direct syscall — no df required.
func readDiskUsage(path string) (used, total uint64, err error) {
	var stat syscall.Statfs_t
	if err = syscall.Statfs(path, &stat); err != nil {
		return
	}
	total = stat.Blocks * uint64(stat.Bsize)
	avail := stat.Bavail * uint64(stat.Bsize)
	used = total - avail
	return
}
