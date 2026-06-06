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
	"os/user"
	"strings"
	"time"
)

// Podman talks to the Podman REST API over its unix socket.
// Supports both root (/run/podman/podman.sock) and
// rootless (/run/user/<uid>/podman/podman.sock).
type Podman struct {
	client     *http.Client
	socketPath string
}

// NewPodman returns a Podman runtime if a socket is accessible.
func NewPodman() *Podman {
	socket := findPodmanSocket()
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
	p := &Podman{client: client, socketPath: socket}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := p.Ping(ctx); err != nil {
		return nil
	}
	return p
}

func findPodmanSocket() string {
	// Root socket
	if _, err := os.Stat("/run/podman/podman.sock"); err == nil {
		return "/run/podman/podman.sock"
	}
	// Rootless: /run/user/<uid>/podman/podman.sock
	if u, err := user.Current(); err == nil {
		path := fmt.Sprintf("/run/user/%s/podman/podman.sock", u.Uid)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	// XDG_RUNTIME_DIR fallback
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		path := xdg + "/podman/podman.sock"
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func (p *Podman) Name() string { return "podman" }

func (p *Podman) url(path string) string {
	return "http://localhost/v4.0.0" + path
}

func (p *Podman) get(ctx context.Context, path string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, "GET", p.url(path), nil)
	if err != nil {
		return err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("podman API %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (p *Podman) post(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, "POST", p.url(path), nil)
	if err != nil {
		return err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("podman API %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (p *Podman) delete(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", p.url(path), nil)
	if err != nil {
		return err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("podman API %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (p *Podman) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", p.url("/libpod/_ping"), nil)
	if err != nil {
		return err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// Podman container list uses the same Docker-compatible endpoint
func (p *Podman) ListContainers(ctx context.Context, all bool) ([]Container, error) {
	path := "/containers/json?size=true"
	if all {
		path += "&all=true"
	}
	var raw []dockerContainer // Podman's compat API uses same shape
	if err := p.get(ctx, path, &raw); err != nil {
		return nil, err
	}

	out := make([]Container, len(raw))
	for i, r := range raw {
		name := r.Id[:12]
		if len(r.Names) > 0 {
			name = strings.TrimPrefix(r.Names[0], "/")
		}
		var ports []PortMapping
		for _, pm := range r.Ports {
			ports = append(ports, PortMapping{
				HostIP:        pm.IP,
				HostPort:      pm.PublicPort,
				ContainerPort: pm.PrivatePort,
				Protocol:      pm.Type,
			})
		}
		var mounts []Mount
		for _, m := range r.Mounts {
			mounts = append(mounts, Mount{
				Type: m.Type, Source: m.Source,
				Destination: m.Destination, Mode: m.Mode, RW: m.RW,
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
			ID: r.Id, ShortID: shortID, Name: name,
			Image: r.Image, ImageID: r.ImageID, Command: r.Command,
			State: normaliseState(r.State), Status: r.Status, Created: created,
			Ports: ports, Labels: r.Labels, Mounts: mounts, Networks: networks,
			SizeRW: r.SizeRw, SizeRootFS: r.SizeRootFs,
		}
	}
	return out, nil
}

func (p *Podman) InspectContainer(ctx context.Context, id string) (*Container, error) {
	all, err := p.ListContainers(ctx, true)
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

func (p *Podman) Start(ctx context.Context, id string) (string, error) {
	if err := p.post(ctx, "/containers/"+id+"/start"); err != nil {
		return "", err
	}
	return fmt.Sprintf("podman start %s", id), nil
}

func (p *Podman) Stop(ctx context.Context, id string, timeout int) (string, error) {
	path := fmt.Sprintf("/containers/%s/stop", id)
	if timeout > 0 {
		path += fmt.Sprintf("?t=%d", timeout)
	}
	if err := p.post(ctx, path); err != nil {
		return "", err
	}
	return fmt.Sprintf("podman stop %s", id), nil
}

func (p *Podman) Restart(ctx context.Context, id string) (string, error) {
	if err := p.post(ctx, "/containers/"+id+"/restart"); err != nil {
		return "", err
	}
	return fmt.Sprintf("podman restart %s", id), nil
}

func (p *Podman) Remove(ctx context.Context, id string, force bool) (string, error) {
	path := "/containers/" + id
	if force {
		path += "?force=true"
	}
	if err := p.delete(ctx, path); err != nil {
		return "", err
	}
	cmd := "podman rm"
	if force {
		cmd += " -f"
	}
	return cmd + " " + id, nil
}

func (p *Podman) Logs(ctx context.Context, id string, tail int) (io.ReadCloser, error) {
	path := fmt.Sprintf("/containers/%s/logs?stdout=true&stderr=true&tail=%d&timestamps=true", id, tail)
	req, err := http.NewRequestWithContext(ctx, "GET", p.url(path), nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		resp.Body.Close()
		return nil, fmt.Errorf("logs: HTTP %d", resp.StatusCode)
	}
	return resp.Body, nil
}

func (p *Podman) ListImages(ctx context.Context) ([]Image, error) {
	var raw []dockerImage
	if err := p.get(ctx, "/images/json", &raw); err != nil {
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
			ID: r.Id, ShortID: shortID,
			Tags:      r.RepoTags,
			Created:   time.Unix(r.Created, 0),
			SizeBytes: r.Size,
		}
	}
	return out, nil
}

func (p *Podman) PullImage(ctx context.Context, ref string, out chan<- string) error {
	req, err := http.NewRequestWithContext(ctx, "POST",
		p.url("/images/create?fromImage="+ref), nil)
	if err != nil {
		return err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		var msg map[string]interface{}
		if json.Unmarshal(scanner.Bytes(), &msg) == nil {
			if status, ok := msg["status"].(string); ok {
				select {
				case out <- status:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	}
	return scanner.Err()
}

func (p *Podman) RemoveImage(ctx context.Context, id string, force bool) (string, error) {
	path := "/images/" + id
	if force {
		path += "?force=true"
	}
	if err := p.delete(ctx, path); err != nil {
		return "", err
	}
	cmd := "podman rmi"
	if force {
		cmd += " -f"
	}
	return cmd + " " + id, nil
}

func (p *Podman) ContainerStats(ctx context.Context, id string) (*Stats, error) {
	req, err := http.NewRequestWithContext(ctx, "GET",
		p.url("/containers/"+id+"/stats?stream=false"), nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var raw dockerStats
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
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
	return &Stats{
		ContainerID: id, CPUPercent: cpuPct,
		MemUsage: raw.MemoryStats.Usage, MemLimit: raw.MemoryStats.Limit,
		MemPercent: memPct, NetRx: netRx, NetTx: netTx,
		PIDs: raw.PidsStats.Current,
	}, nil
}
