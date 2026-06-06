package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/brendan4linux/webux/internal/learn"
	"github.com/brendan4linux/webux/internal/system/network/firewall"
)

type FirewallHandler struct {
	mgr   *firewall.Manager
	learn *learn.Store
}

func NewFirewallHandler(ls *learn.Store) *FirewallHandler {
	return &FirewallHandler{mgr: firewall.NewManager(), learn: ls}
}

// Status handles GET /api/firewall
func (h *FirewallHandler) Status(w http.ResponseWriter, r *http.Request) {
	ctx := learn.WithContext(r.Context(), "firewall")
	status, err := h.mgr.GetStatus()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx,
		firewall.CLIEquivalentStatus(h.mgr.Backend()),
		"Reads all active firewall rules from "+string(h.mgr.Backend()))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// AddRule handles POST /api/firewall/rules
func (h *FirewallHandler) AddRule(w http.ResponseWriter, r *http.Request) {
	ctx := learn.WithContext(r.Context(), "firewall")
	var rule firewall.Rule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	cliCmd, err := h.mgr.AddRule(rule)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Adds a new firewall rule via "+string(h.mgr.Backend()))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}

// DeleteRule handles DELETE /api/firewall/rules/{id}
func (h *FirewallHandler) DeleteRule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx := learn.WithContext(r.Context(), "firewall")
	cliCmd, err := h.mgr.DeleteRule(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Removes firewall rule #"+id+" from "+string(h.mgr.Backend()))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}

// Enable handles POST /api/firewall/enable
func (h *FirewallHandler) Enable(w http.ResponseWriter, r *http.Request) {
	ctx := learn.WithContext(r.Context(), "firewall")
	cliCmd, err := h.mgr.Enable()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Enables the "+string(h.mgr.Backend())+" firewall")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}

// Disable handles POST /api/firewall/disable
func (h *FirewallHandler) Disable(w http.ResponseWriter, r *http.Request) {
	ctx := learn.WithContext(r.Context(), "firewall")
	cliCmd, err := h.mgr.Disable()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Disables the "+string(h.mgr.Backend())+" firewall")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}
