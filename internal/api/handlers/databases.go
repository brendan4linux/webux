package handlers

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/brendan4linux/webux/internal/learn"
	"github.com/brendan4linux/webux/internal/system/databases"
)

// DatabasesHandler manages database detection and queries.
type DatabasesHandler struct {
	learn       *learn.Store
	connections sync.Map // id → databases.DB (live connections)
}

func NewDatabasesHandler(ls *learn.Store) *DatabasesHandler {
	return &DatabasesHandler{learn: ls}
}

// Detect handles GET /api/databases — returns all detected DB instances.
func (h *DatabasesHandler) Detect(w http.ResponseWriter, r *http.Request) {
	ctx := learn.WithContext(r.Context(), "databases")
	instances := databases.Detect()

	h.learn.Echo(ctx,
		"ss -tlnp | grep -E '3306|5432|6379|27017'",
		"Detects running databases by scanning /proc/net/tcp for well-known ports")

	writeJSON(w, map[string]interface{}{
		"instances": instances,
		"count":     len(instances),
	})
}

// Connect handles POST /api/databases/connect
// Body: {"driver":"mysql","host":"127.0.0.1","port":3306,"password":"..."}
func (h *DatabasesHandler) Connect(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Driver   databases.Driver `json:"driver"`
		Host     string           `json:"host"`
		Port     int              `json:"port"`
		Socket   string           `json:"socket"`
		Password string           `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	inst := databases.Instance{
		Driver: body.Driver,
		Host:   body.Host,
		Port:   body.Port,
		Socket: body.Socket,
	}

	ctx := learn.WithContext(r.Context(), "databases")
	db, err := databases.Connect(ctx, inst, body.Password)
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	// Store connection keyed by driver
	connID := string(body.Driver)
	h.connections.Store(connID, db)

	h.learn.Echo(ctx,
		"mysql -h "+body.Host+" -u root -p",
		"Opens a connection to the "+string(body.Driver)+" database")

	writeJSON(w, map[string]interface{}{"ok": true, "connection_id": connID})
}

// ListDatabases handles GET /api/databases/{connID}/databases
func (h *DatabasesHandler) ListDatabases(w http.ResponseWriter, r *http.Request) {
	connID := chi.URLParam(r, "connID")
	db, err := h.getConn(connID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	ctx := learn.WithContext(r.Context(), "databases")
	dbs, err := db.ListDatabases(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.learn.Echo(ctx, "SHOW DATABASES;", "Lists all databases on the "+string(db.Driver())+" server")
	writeJSON(w, map[string]interface{}{"databases": dbs})
}

// ListTables handles GET /api/databases/{connID}/tables?db=mydb
func (h *DatabasesHandler) ListTables(w http.ResponseWriter, r *http.Request) {
	connID := chi.URLParam(r, "connID")
	database := r.URL.Query().Get("db")

	db, err := h.getConn(connID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	ctx := learn.WithContext(r.Context(), "databases")
	tables, err := db.ListTables(ctx, database)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.learn.Echo(ctx, "SHOW TABLES;", "Lists all tables in database "+database)
	writeJSON(w, map[string]interface{}{"tables": tables, "database": database})
}

// Query handles POST /api/databases/{connID}/query
// Body: {"database":"mydb","sql":"SELECT ..."}
func (h *DatabasesHandler) Query(w http.ResponseWriter, r *http.Request) {
	connID := chi.URLParam(r, "connID")
	db, err := h.getConn(connID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	var body struct {
		Database string `json:"database"`
		SQL      string `json:"sql"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	ctx := learn.WithContext(r.Context(), "databases")
	result, err := db.Query(ctx, body.Database, body.SQL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.learn.Echo(ctx, body.SQL, "Runs a read-only query on the "+string(db.Driver())+" database")
	writeJSON(w, result)
}

// Disconnect handles DELETE /api/databases/{connID}
func (h *DatabasesHandler) Disconnect(w http.ResponseWriter, r *http.Request) {
	connID := chi.URLParam(r, "connID")
	if v, ok := h.connections.Load(connID); ok {
		if db, ok := v.(databases.DB); ok {
			db.Close()
		}
		h.connections.Delete(connID)
	}
	writeJSON(w, map[string]interface{}{"ok": true})
}

func (h *DatabasesHandler) getConn(id string) (databases.DB, error) {
	v, ok := h.connections.Load(id)
	if !ok {
		return nil, errorf("no active connection for %q — connect first", id)
	}
	db, ok := v.(databases.DB)
	if !ok {
		return nil, errorf("invalid connection object")
	}
	return db, nil
}

func errorf(format string, args ...interface{}) error {
	return &handlerError{msg: format}
}

type handlerError struct{ msg string }

func (e *handlerError) Error() string { return e.msg }
