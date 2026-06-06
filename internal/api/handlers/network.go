package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/brendan4linux/webux/internal/learn"
	"github.com/brendan4linux/webux/internal/system/network/interfaces"
)

type NetworkHandler struct {
	mgr   *interfaces.Manager
	learn *learn.Store
}

func NewNetworkHandler(ls *learn.Store) *NetworkHandler {
	return &NetworkHandler{mgr: interfaces.NewManager(), learn: ls}
}

// ListInterfaces handles GET /api/network/interfaces
func (h *NetworkHandler) ListInterfaces(w http.ResponseWriter, r *http.Request) {
	ctx := learn.WithContext(r.Context(), "network")
	ifaces, err := h.mgr.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, "ip -j addr show && ip -j link show",
		"Lists all network interfaces, their addresses, flags and statistics from the kernel")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"interfaces": ifaces, "count": len(ifaces)})
}

// ListRoutes handles GET /api/network/routes
func (h *NetworkHandler) ListRoutes(w http.ResponseWriter, r *http.Request) {
	ctx := learn.WithContext(r.Context(), "network")
	routes, err := h.mgr.Routes()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, "ip route show && ip -6 route show",
		"Reads the kernel routing table from /proc/net/route and /proc/net/ipv6_route")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"routes": routes, "count": len(routes)})
}

// SetState handles POST /api/network/interfaces/{name}/state
// Body: {"state": "up"|"down"}
func (h *NetworkHandler) SetState(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	ctx := learn.WithContext(r.Context(), "network")

	var body struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	var cliCmd string
	var err error
	switch body.State {
	case "up":
		cliCmd, err = h.mgr.SetUp(name)
	case "down":
		cliCmd, err = h.mgr.SetDown(name)
	default:
		http.Error(w, "state must be 'up' or 'down'", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Brings the "+name+" interface "+body.State)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}

// AddAddress handles POST /api/network/interfaces/{name}/addresses
// Body: {"cidr": "192.168.1.100/24"}
func (h *NetworkHandler) AddAddress(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	ctx := learn.WithContext(r.Context(), "network")

	var body struct {
		CIDR string `json:"cidr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.CIDR == "" {
		http.Error(w, "cidr is required", http.StatusBadRequest)
		return
	}
	cliCmd, err := h.mgr.AddAddress(name, body.CIDR)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Assigns the IP address "+body.CIDR+" to "+name)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}

// DelAddress handles DELETE /api/network/interfaces/{name}/addresses
// Body: {"cidr": "192.168.1.100/24"}
func (h *NetworkHandler) DelAddress(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	ctx := learn.WithContext(r.Context(), "network")

	var body struct {
		CIDR string `json:"cidr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.CIDR == "" {
		http.Error(w, "cidr is required", http.StatusBadRequest)
		return
	}
	cliCmd, err := h.mgr.DelAddress(name, body.CIDR)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Removes the IP address "+body.CIDR+" from "+name)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}

// SetMTU handles POST /api/network/interfaces/{name}/mtu
// Body: {"mtu": 1500}
func (h *NetworkHandler) SetMTU(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	ctx := learn.WithContext(r.Context(), "network")

	var body struct {
		MTU int `json:"mtu"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.MTU <= 0 {
		http.Error(w, "valid mtu is required", http.StatusBadRequest)
		return
	}
	cliCmd, err := h.mgr.SetMTU(name, body.MTU)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Sets the MTU of "+name+" to "+itoa(body.MTU)+" bytes")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}
