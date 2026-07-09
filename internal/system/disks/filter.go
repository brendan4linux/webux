package disks

import "strings"

// isLoopDevice returns true if the block device is a loop device (loop0-9 etc.)
// These are used by snap packages, flatpak, systemd-sysext etc. and are
// almost never useful to show in a disk management UI.
func isLoopDevice(name string) bool {
	return strings.HasPrefix(name, "loop")
}

// isRamDevice returns true for RAM block devices (ram0, ram1 etc.)
func isRamDevice(name string) bool {
	return strings.HasPrefix(name, "ram")
}

// shouldShowDevice returns true if the device is worth showing to the user.
func shouldShowDevice(d BlockDevice) bool {
	if isLoopDevice(d.Name) { return false }
	if isRamDevice(d.Name)  { return false }
	return true
}
