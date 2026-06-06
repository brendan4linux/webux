// Package containers provides a unified interface over Docker and Podman.
// Communication is direct to the unix socket — no CLI subprocess needed.
package containers

import (
	"context"
	"io"
	"time"
)

// State represents container runtime state.
type State string

const (
	StateRunning  State = "running"
	StateStopped  State = "stopped"
	StateExited   State = "exited"
	StatePaused   State = "paused"
	StateCreated  State = "created"
	StateRemoving State = "removing"
	StateUnknown  State = "unknown"
)

// Container is a normalised container record across Docker and Podman.
type Container struct {
	ID         string            `json:"id"`
	ShortID    string            `json:"short_id"`
	Name       string            `json:"name"`
	Image      string            `json:"image"`
	ImageID    string            `json:"image_id"`
	Command    string            `json:"command"`
	State      State             `json:"state"`
	Status     string            `json:"status"`
	Created    time.Time         `json:"created"`
	Started    *time.Time        `json:"started,omitempty"`
	Ports      []PortMapping     `json:"ports"`
	Labels     map[string]string `json:"labels"`
	Mounts     []Mount           `json:"mounts"`
	Networks   []string          `json:"networks"`
	SizeRW     int64             `json:"size_rw"`
	SizeRootFS int64             `json:"size_rootfs"`
}

// PortMapping is a host→container port binding.
type PortMapping struct {
	HostIP        string `json:"host_ip"`
	HostPort      int    `json:"host_port"`
	ContainerPort int    `json:"container_port"`
	Protocol      string `json:"protocol"`
}

// Mount is a volume or bind mount.
type Mount struct {
	Type        string `json:"type"`   // bind | volume | tmpfs
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Mode        string `json:"mode"`
	RW          bool   `json:"rw"`
}

// Image is a local container image.
type Image struct {
	ID       string    `json:"id"`
	ShortID  string    `json:"short_id"`
	Tags     []string  `json:"tags"`
	Created  time.Time `json:"created"`
	SizeBytes int64    `json:"size_bytes"`
}

// Stats is a point-in-time container resource snapshot.
type Stats struct {
	ContainerID string  `json:"container_id"`
	CPUPercent  float64 `json:"cpu_percent"`
	MemUsage    uint64  `json:"mem_usage"`
	MemLimit    uint64  `json:"mem_limit"`
	MemPercent  float64 `json:"mem_percent"`
	NetRx       uint64  `json:"net_rx"`
	NetTx       uint64  `json:"net_tx"`
	BlockRead   uint64  `json:"block_read"`
	BlockWrite  uint64  `json:"block_write"`
	PIDs        uint64  `json:"pids"`
}

// Runtime is the interface both Docker and Podman implement.
type Runtime interface {
	// Name returns "docker" or "podman"
	Name() string

	// Ping checks connectivity to the daemon.
	Ping(ctx context.Context) error

	// ListContainers returns all containers (running and stopped).
	ListContainers(ctx context.Context, all bool) ([]Container, error)

	// InspectContainer returns full details for one container.
	InspectContainer(ctx context.Context, id string) (*Container, error)

	// Start starts a stopped container. Returns CLI equivalent.
	Start(ctx context.Context, id string) (string, error)

	// Stop stops a running container. Returns CLI equivalent.
	Stop(ctx context.Context, id string, timeout int) (string, error)

	// Restart restarts a container. Returns CLI equivalent.
	Restart(ctx context.Context, id string) (string, error)

	// Remove removes a container. Returns CLI equivalent.
	Remove(ctx context.Context, id string, force bool) (string, error)

	// Logs streams container logs. Caller must close the returned reader.
	Logs(ctx context.Context, id string, tail int) (io.ReadCloser, error)

	// ListImages returns local images.
	ListImages(ctx context.Context) ([]Image, error)

	// PullImage pulls an image. Streams progress lines to out channel.
	PullImage(ctx context.Context, ref string, out chan<- string) error

	// RemoveImage removes a local image. Returns CLI equivalent.
	RemoveImage(ctx context.Context, id string, force bool) (string, error)

	// ContainerStats returns a single resource snapshot.
	ContainerStats(ctx context.Context, id string) (*Stats, error)
}

// Detect returns available container runtimes.
// Returns Docker first if both are present.
func Detect() []Runtime {
	var runtimes []Runtime
	if d := NewDocker(); d != nil {
		runtimes = append(runtimes, d)
	}
	if p := NewPodman(); p != nil {
		runtimes = append(runtimes, p)
	}
	return runtimes
}
