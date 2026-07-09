package containers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// RunConfig holds the options for deploying a new container.
type RunConfig struct {
	Image  string
	Name   string
	Ports  []RunPort
	Mounts []RunMount
	Env    []string // "KEY=VALUE"
}

type RunPort  struct{ Host, Container string }
type RunMount struct{ Host, Container string }

// postJSON sends a JSON POST via an existing *http.Client and decodes the response.
// Uses the same pattern as the existing get/post/delete methods.
func postJSON(ctx context.Context, client *http.Client, url string, body interface{}, out interface{}) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(buf)))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func buildCreateBody(cfg RunConfig) map[string]interface{} {
	portBindings := map[string]interface{}{}
	exposedPorts := map[string]interface{}{}
	for _, p := range cfg.Ports {
		if p.Host == "" || p.Container == "" {
			continue
		}
		key := p.Container + "/tcp"
		exposedPorts[key] = struct{}{}
		portBindings[key] = []map[string]string{{"HostPort": p.Host}}
	}
	var binds []string
	for _, m := range cfg.Mounts {
		if m.Host != "" && m.Container != "" {
			binds = append(binds, m.Host+":"+m.Container)
		}
	}
	env := cfg.Env
	if env == nil {
		env = []string{}
	}
	return map[string]interface{}{
		"Image":        cfg.Image,
		"Env":          env,
		"ExposedPorts": exposedPorts,
		"HostConfig": map[string]interface{}{
			"PortBindings": portBindings,
			"Binds":        binds,
		},
	}
}

// RunContainer creates and starts a container via the Docker socket API.
func (d *Docker) RunContainer(ctx context.Context, cfg RunConfig) (string, error) {
	path := "/containers/create"
	if cfg.Name != "" {
		path += "?name=" + cfg.Name
	}
	var resp struct {
		ID string `json:"Id"`
	}
	if err := postJSON(ctx, d.client, d.url(path), buildCreateBody(cfg), &resp); err != nil {
		return "", fmt.Errorf("create: %w", err)
	}
	if resp.ID == "" {
		return "", fmt.Errorf("no container ID returned")
	}
	if err := d.post(ctx, fmt.Sprintf("/containers/%s/start", resp.ID)); err != nil {
		return resp.ID, fmt.Errorf("start: %w", err)
	}
	return resp.ID, nil
}

// RunContainer creates and starts a container via the Podman socket API.
func (p *Podman) RunContainer(ctx context.Context, cfg RunConfig) (string, error) {
	path := "/containers/create"
	if cfg.Name != "" {
		path += "?name=" + cfg.Name
	}
	var resp struct {
		ID string `json:"Id"`
	}
	if err := postJSON(ctx, p.client, p.url(path), buildCreateBody(cfg), &resp); err != nil {
		return "", fmt.Errorf("create: %w", err)
	}
	if resp.ID == "" {
		return "", fmt.Errorf("no container ID returned")
	}
	if err := p.post(ctx, fmt.Sprintf("/containers/%s/start", resp.ID)); err != nil {
		return resp.ID, fmt.Errorf("start: %w", err)
	}
	return resp.ID, nil
}

// RunCLIPreview returns the equivalent CLI command for Learn Mode.
func RunCLIPreview(runtime string, cfg RunConfig) string {
	var sb strings.Builder
	sb.WriteString(runtime + " run -d")
	if cfg.Name != "" {
		sb.WriteString(" --name " + cfg.Name)
	}
	for _, p := range cfg.Ports {
		if p.Host != "" && p.Container != "" {
			sb.WriteString(fmt.Sprintf(" -p %s:%s", p.Host, p.Container))
		}
	}
	for _, m := range cfg.Mounts {
		if m.Host != "" && m.Container != "" {
			sb.WriteString(fmt.Sprintf(" -v %s:%s", m.Host, m.Container))
		}
	}
	for _, e := range cfg.Env {
		sb.WriteString(" -e " + e)
	}
	sb.WriteString(" " + cfg.Image)
	return sb.String()
}
