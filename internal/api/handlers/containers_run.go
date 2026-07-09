package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/brendan4linux/webux/internal/learn"
	"github.com/brendan4linux/webux/internal/system/containers"
)

type runPortReq  struct { Host string `json:"host"`;  Container string `json:"container"` }
type runMountReq struct { Host string `json:"host"`;  Container string `json:"container"` }
type runEnvReq   struct { Key  string `json:"key"`;   Value     string `json:"value"` }

type runRequest struct {
	Image  string       `json:"image"`
	Name   string       `json:"name"`
	Ports  []runPortReq  `json:"ports"`
	Mounts []runMountReq `json:"mounts"`
	Env    []runEnvReq   `json:"env"`
}

// Run handles POST /api/containers/run
func (h *ContainersHandler) Run(w http.ResponseWriter, r *http.Request) {
	var body runRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if body.Image == "" {
		http.Error(w, "image is required", http.StatusBadRequest)
		return
	}

	rt, err := h.runtimeFor(r.URL.Query().Get("runtime"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	cfg := containers.RunConfig{
		Image: body.Image,
		Name:  body.Name,
	}
	for _, p := range body.Ports {
		cfg.Ports = append(cfg.Ports, containers.RunPort{Host: p.Host, Container: p.Container})
	}
	for _, m := range body.Mounts {
		cfg.Mounts = append(cfg.Mounts, containers.RunMount{Host: m.Host, Container: m.Container})
	}
	for _, e := range body.Env {
		if e.Key != "" {
			cfg.Env = append(cfg.Env, fmt.Sprintf("%s=%s", e.Key, e.Value))
		}
	}

	ctx := learn.WithContext(r.Context(), "containers")
	id, err := rt.RunContainer(ctx, cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	cli := containers.RunCLIPreview(rt.Name(), cfg)
	h.learn.Echo(ctx, cli, "Deploys container from image "+strings.SplitN(body.Image, ":", 2)[0])

	writeJSON(w, map[string]interface{}{"ok": true, "id": id})
}
