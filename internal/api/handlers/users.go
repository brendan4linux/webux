package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/brendan4linux/webux/internal/learn"
	"github.com/brendan4linux/webux/internal/system/users"
)

// UsersHandler manages Linux user and group accounts.
type UsersHandler struct {
	mgr   *users.Manager
	learn *learn.Store
}

func NewUsersHandler(ls *learn.Store) *UsersHandler {
	return &UsersHandler{mgr: users.NewManager(), learn: ls}
}

// ListUsers handles GET /api/users?system=true
func (h *UsersHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	includeSystem := r.URL.Query().Get("system") == "true"

	ctx := learn.WithContext(r.Context(), "users")

	userList, err := h.mgr.ListUsers(includeSystem)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	cmd := "getent passwd"
	if !includeSystem {
		cmd = "getent passwd | awk -F: '$3 >= 1000'"
	}
	h.learn.Echo(ctx, cmd, "Reads user accounts from /etc/passwd and /etc/group")

	writeJSON(w, map[string]interface{}{
		"users": userList,
		"count": len(userList),
	})
}

// GetUser handles GET /api/users/{username}
func (h *UsersHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	ctx := learn.WithContext(r.Context(), "users")

	u, err := h.mgr.GetUser(username)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	h.learn.Echo(ctx,
		"id "+username+" && groups "+username,
		"Shows UID, GID and group memberships for "+username)

	writeJSON(w, u)
}

// CreateUser handles POST /api/users
func (h *UsersHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var opts users.CreateUserOpts
	if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if opts.Username == "" {
		http.Error(w, "username is required", http.StatusBadRequest)
		return
	}

	ctx := learn.WithContext(r.Context(), "users")
	cliCmd, err := h.mgr.CreateUser(opts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.learn.Echo(ctx, cliCmd, "Creates a new Linux user account for "+opts.Username)
	writeJSON(w, map[string]interface{}{"ok": true, "username": opts.Username, "cli_cmd": cliCmd})
}

// DeleteUser handles DELETE /api/users/{username}?remove_home=true
func (h *UsersHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	removeHome := r.URL.Query().Get("remove_home") == "true"

	ctx := learn.WithContext(r.Context(), "users")
	cliCmd, err := h.mgr.DeleteUser(username, removeHome)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.learn.Echo(ctx, cliCmd, "Deletes the Linux user account "+username)
	writeJSON(w, map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}

// LockUser handles POST /api/users/{username}/lock
func (h *UsersHandler) LockUser(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	ctx := learn.WithContext(r.Context(), "users")

	cliCmd, err := h.mgr.LockUser(username)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.learn.Echo(ctx, cliCmd, "Locks "+username+"'s account by prefixing the password hash with !")
	writeJSON(w, map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}

// UnlockUser handles POST /api/users/{username}/unlock
func (h *UsersHandler) UnlockUser(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	ctx := learn.WithContext(r.Context(), "users")

	cliCmd, err := h.mgr.UnlockUser(username)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.learn.Echo(ctx, cliCmd, "Unlocks "+username+"'s account, restoring login access")
	writeJSON(w, map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}

// AddToGroup handles POST /api/users/{username}/groups
func (h *UsersHandler) AddToGroup(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")

	var body struct {
		Group string `json:"group"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Group == "" {
		http.Error(w, "group is required", http.StatusBadRequest)
		return
	}

	ctx := learn.WithContext(r.Context(), "users")
	cliCmd, err := h.mgr.AddToGroup(username, body.Group)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.learn.Echo(ctx, cliCmd, "Adds "+username+" to the "+body.Group+" supplementary group")
	writeJSON(w, map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}

// ListGroups handles GET /api/groups
func (h *UsersHandler) ListGroups(w http.ResponseWriter, r *http.Request) {
	ctx := learn.WithContext(r.Context(), "users")

	groups, err := h.mgr.ListGroups()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.learn.Echo(ctx, "getent group", "Reads all groups from /etc/group")
	writeJSON(w, map[string]interface{}{"groups": groups, "count": len(groups)})
}

// Shells handles GET /api/users/shells
func (h *UsersHandler) Shells(w http.ResponseWriter, r *http.Request) {
	h.learn.Echo(r.Context(), "cat /etc/shells", "Lists all valid login shells on this system")
	writeJSON(w, map[string]interface{}{"shells": users.AvailableShells()})
}

// CreateGroup handles POST /api/groups
func (h *UsersHandler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
		GID  int    `json:"gid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	ctx := learn.WithContext(r.Context(), "users")
	cliCmd, err := h.mgr.CreateGroup(body.Name, body.GID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Creates the Linux group "+body.Name)
	writeJSON(w, map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}

// DeleteGroup handles DELETE /api/groups/{name}
func (h *UsersHandler) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	ctx := learn.WithContext(r.Context(), "users")
	cliCmd, err := h.mgr.DeleteGroup(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Deletes the Linux group "+name)
	writeJSON(w, map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}
