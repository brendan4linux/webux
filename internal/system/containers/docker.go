package containers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

const dockerVersion = "v1.43"

// dockerSockets lists candidate paths in preference order.
var dockerSockets = []string{
	"/var/run/docker.sock",
	"/run/docker.sock",
}

// Docker talks to the Docker Engine API over its unix socket.
type Docker struct {
	client *http.Client
}

// NewDocker returns a Docker runtime if any known socket is accessible.
func NewDocker() *Docker {
	socket := ""
	for _, s := range dockerSockets {
		if _, err := os.Stat(s); err == nil {
			socket = s
			break
		}
	}
	if socket == "" {
		return nil
	}
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socket)
			},
		},
		Timeout: 30 * time.Second,
	}
	d := &Docker{client: client}
	// Quick ping to confirm daemon is alive
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := d.Ping(ctx); err != nil {
		return nil
	}
	return d
}

func (d *Docker) Name() string { return "docker" }

func (d *Docker) url(path string) string {
	return "http://localhost/" + dockerVersion + path
}

func (d *Docker) get(ctx context.Context, path string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, "GET", d.url(path), nil)
	if err != nil {
		return err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("docker API %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (d *Docker) post(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, "POST", d.url(path), nil)
	if err != nil {
		return err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("docker API %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (d *Docker) delete(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", d.url(path), nil)
	if err != nil {
		return err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("docker API %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (d *Docker) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", d.url("/_ping"), nil)
	if err != nil {
		return err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// dockerContainer is the raw Docker API container list entry.
type dockerContainer struct {
	Id      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	ImageID string            `json:"ImageID"`
	Command string            `json:"Command"`
	Created int64             `json:"Created"`
	State   string            `json:"State"`
	Status  string            `json:"Status"`
	Ports   []struct {
		IP          string `json:"IP"`
		PrivatePort int    `json:"PrivatePort"`
		PublicPort  int    `json:"PublicPort"`
		Type        string `json:"Type"`
	} `json:"Ports"`
	Labels  map[string]string `json:"Labels"`
	Mounts  []struct {
		Type        string `json:"Type"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		Mode        string `json:"Mode"`
		RW          bool   `json:"RW"`
	} `json:"Mounts"`
	NetworkSettings struct {
		Networks map[string]interface{} `json:"Networks"`
	} `json:"NetworkSettings"`
	SizeRw     int64 `json:"SizeRw"`
	SizeRootFs int64 `json:"SizeRootFs"`
}

func (d *Docker) ListContainers(ctx context.Context, all bool) ([]Container, error) {
	path := "/containers/json?size=true"
	if all {
		path += "&all=true"
	}
	var raw []dockerContainer
	if err := d.get(ctx, path, &raw); err != nil {
		return nil, err
	}

	out := make([]Container, len(raw))
	for i, r := range raw {
		name := r.Id[:12]
		if len(r.Names) > 0 {
			name = strings.TrimPrefix(r.Names[0], "/")
		}

		var ports []PortMapping
		for _, p := range r.Ports {
			ports = append(ports, PortMapping{
				HostIP:        p.IP,
				HostPort:      p.PublicPort,
				ContainerPort: p.PrivatePort,
				Protocol:      p.Type,
			})
		}

		var mounts []Mount
		for _, m := range r.Mounts {
			mounts = append(mounts, Mount{
				Type:        m.Type,
				Source:      m.Source,
				Destination: m.Destination,
				Mode:        m.Mode,
				RW:          m.RW,
			})
		}

		var networks []string
		for k := range r.NetworkSettings.Networks {
			networks = append(networks, k)
		}

		shortID := r.Id
		if len(shortID) > 12 {
			shortID = shortID[:12]
		}

		created := time.Unix(r.Created, 0)
		out[i] = Container{
			ID:         r.Id,
			ShortID:    shortID,
			Name:       name,
			Image:      r.Image,
			ImageID:    r.ImageID,
			Command:    r.Command,
			State:      normaliseState(r.State),
			Status:     r.Status,
			Created:    created,
			Ports:      ports,
			Labels:     r.Labels,
			Mounts:     mounts,
			Networks:   networks,
			SizeRW:     r.SizeRw,
			SizeRootFS: r.SizeRootFs,
		}
	}
	return out, nil
}

func (d *Docker) InspectContainer(ctx context.Context, id string) (*Container, error) {
	// For simplicity, list all and find matching — inspect endpoint returns different shape
	all, err := d.ListContainers(ctx, true)
	if err != nil {
		return nil, err
	}
	for _, c := range all {
		if c.ID == id || c.ShortID == id || c.Name == id {
			return &c, nil
		}
	}
	return nil, fmt.Errorf("container %q not found", id)
}

func (d *Docker) Start(ctx context.Context, id string) (string, error) {
	if err := d.post(ctx, "/containers/"+id+"/start"); err != nil {
		return "", err
	}
	return fmt.Sprintf("docker start %s", id), nil
}

func (d *Docker) Stop(ctx context.Context, id string, timeout int) (string, error) {
	path := fmt.Sprintf("/containers/%s/stop", id)
	if timeout > 0 {
		path += fmt.Sprintf("?t=%d", timeout)
	}
	if err := d.post(ctx, path); err != nil {
		return "", err
	}
	return fmt.Sprintf("docker stop %s", id), nil
}

func (d *Docker) Restart(ctx context.Context, id string) (string, error) {
	if err := d.post(ctx, "/containers/"+id+"/restart"); err != nil {
		return "", err
	}
	return fmt.Sprintf("docker restart %s", id), nil
}

func (d *Docker) Remove(ctx context.Context, id string, force bool) (string, error) {
	path := "/containers/" + id
	if force {
		path += "?force=true"
	}
	if err := d.delete(ctx, path); err != nil {
		return "", err
	}
	cmd := "docker rm"
	if force {
		cmd += " -f"
	}
	return cmd + " " + id, nil
}

func (d *Docker) Logs(ctx context.Context, id string, tail int) (io.ReadCloser, error) {
	path := fmt.Sprintf("/containers/%s/logs?stdout=true&stderr=true&tail=%d&timestamps=true", id, tail)
	req, err := http.NewRequestWithContext(ctx, "GET", d.url(path), nil)
	if err != nil {
		return nil, err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		resp.Body.Close()
		return nil, fmt.Errorf("logs: HTTP %d", resp.StatusCode)
	}
	return resp.Body, nil
}

type dockerImage struct {
	Id          string   `json:"Id"`
	RepoTags    []string `json:"RepoTags"`
	Created     int64    `json:"Created"`
	Size        int64    `json:"Size"`
}

func (d *Docker) ListImages(ctx context.Context) ([]Image, error) {
	var raw []dockerImage
	if err := d.get(ctx, "/images/json", &raw); err != nil {
		return nil, err
	}
	out := make([]Image, len(raw))
	for i, r := range raw {
		shortID := r.Id
		if strings.HasPrefix(shortID, "sha256:") {
			shortID = shortID[7:]
		}
		if len(shortID) > 12 {
			shortID = shortID[:12]
		}
		out[i] = Image{
			ID:        r.Id,
			ShortID:   shortID,
			Tags:      r.RepoTags,
			Created:   time.Unix(r.Created, 0),
			SizeBytes: r.Size,
		}
	}
	return out, nil
}

func (d *Docker) PullImage(ctx context.Context, ref string, out chan<- string) error {
	req, err := http.NewRequestWithContext(ctx, "POST",
		d.url("/images/create?fromImage="+ref), nil)
	if err != nil {
		return err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		var msg map[string]interface{}
		if json.Unmarshal(scanner.Bytes(), &msg) == nil {
			if status, ok := msg["status"].(string); ok {
				line := status
				if prog, ok := msg["progress"].(string); ok {
					line += " " + prog
				}
				select {
				case out <- line:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	}
	return scanner.Err()
}

func (d *Docker) RemoveImage(ctx context.Context, id string, force bool) (string, error) {
	path := "/images/" + id
	if force {
		path += "?force=true"
	}
	if err := d.delete(ctx, path); err != nil {
		return "", err
	}
	cmd := "docker rmi"
	if force {
		cmd += " -f"
	}
	return cmd + " " + id, nil
}

type dockerStats struct {
	CPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs     int    `json:"online_cpus"`
	} `json:"cpu_stats"`
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
	MemoryStats struct {
		Usage uint64 `json:"usage"`
		Limit uint64 `json:"limit"`
	} `json:"memory_stats"`
	Networks map[string]struct {
		RxBytes uint64 `json:"rx_bytes"`
		TxBytes uint64 `json:"tx_bytes"`
	} `json:"networks"`
	BlkioStats struct {
		IOServiceBytesRecursive []struct {
			Op    string `json:"op"`
			Value uint64 `json:"value"`
		} `json:"io_service_bytes_recursive"`
	} `json:"blkio_stats"`
	PidsStats struct {
		Current uint64 `json:"current"`
	} `json:"pids_stats"`
}

func (d *Docker) ContainerStats(ctx context.Context, id string) (*Stats, error) {
	req, err := http.NewRequestWithContext(ctx, "GET",
		d.url("/containers/"+id+"/stats?stream=false"), nil)
	if err != nil {
		return nil, err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var raw dockerStats
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	// CPU% calculation
	cpuDelta := float64(raw.CPUStats.CPUUsage.TotalUsage - raw.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(raw.CPUStats.SystemCPUUsage - raw.PreCPUStats.SystemCPUUsage)
	cpuPct := 0.0
	if sysDelta > 0 {
		cpuPct = (cpuDelta / sysDelta) * float64(raw.CPUStats.OnlineCPUs) * 100
	}

	memPct := 0.0
	if raw.MemoryStats.Limit > 0 {
		memPct = float64(raw.MemoryStats.Usage) / float64(raw.MemoryStats.Limit) * 100
	}

	var netRx, netTx uint64
	for _, n := range raw.Networks {
		netRx += n.RxBytes
		netTx += n.TxBytes
	}

	var blkRead, blkWrite uint64
	for _, b := range raw.BlkioStats.IOServiceBytesRecursive {
		switch b.Op {
		case "Read":
			blkRead += b.Value
		case "Write":
			blkWrite += b.Value
		}
	}

	return &Stats{
		ContainerID: id,
		CPUPercent:  cpuPct,
		MemUsage:    raw.MemoryStats.Usage,
		MemLimit:    raw.MemoryStats.Limit,
		MemPercent:  memPct,
		NetRx:       netRx,
		NetTx:       netTx,
		BlockRead:   blkRead,
		BlockWrite:  blkWrite,
		PIDs:        raw.PidsStats.Current,
	}, nil
}

func normaliseState(s string) State {
	switch strings.ToLower(s) {
	case "running":
		return StateRunning
	case "stopped":
		return StateStopped
	case "exited":
		return StateExited
	case "paused":
		return StatePaused
	case "created":
		return StateCreated
	case "removing":
		return StateRemoving
	default:
		return StateUnknown
	}
}
