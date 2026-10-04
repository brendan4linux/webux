package handlers

import (
	"encoding/json"
	"net/http"
	"regexp"

	"github.com/brendan4linux/webux/internal/system/vms"
	"github.com/go-chi/chi/v5"
)

type VMHandler struct{}

func NewVMHandler() *VMHandler { return &VMHandler{} }

var vmIDRe = regexp.MustCompile(`^[a-zA-Z0-9_\-\.]+$`)

func (h *VMHandler) Info(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, vms.Detect())
}

func (h *VMHandler) List(w http.ResponseWriter, r *http.Request) {
	list, err := vms.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, list)
}

func (h *VMHandler) Cluster(w http.ResponseWriter, r *http.Request) {
	list, err := vms.ListCluster()
	if err != nil {
		// Not a proxmox cluster or pvesh not available — return empty, not an error
		writeJSON(w, []vms.ClusterVM{})
		return
	}
	writeJSON(w, list)
}

func (h *VMHandler) Action(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !vmIDRe.MatchString(id) {
		http.Error(w, "invalid vm id", http.StatusBadRequest)
		return
	}

	var req struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	validActions := map[string]bool{
		"start": true, "stop": true, "reboot": true,
		"force-stop": true, "suspend": true, "resume": true,
	}
	if !validActions[req.Action] {
		http.Error(w, "invalid action", http.StatusBadRequest)
		return
	}

	if err := vms.Action(id, req.Action); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}
