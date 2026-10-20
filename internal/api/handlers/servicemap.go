package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/brendan4linux/webux/internal/learn"
	"github.com/brendan4linux/webux/internal/system/servicemap"
)

// ServiceMapHandler handles all service-map endpoints.
type ServiceMapHandler struct {
	db    *sql.DB
	learn *learn.Store
}

func NewServiceMapHandler(db *sql.DB, ls *learn.Store) *ServiceMapHandler {
	return &ServiceMapHandler{db: db, learn: ls}
}

// Snapshot handles GET /api/servicemap/snapshot
// Returns the live service inventory + active connections.
func (h *ServiceMapHandler) Snapshot(w http.ResponseWriter, r *http.Request) {
	h.learn.Echo(r.Context(),
		`ss -tulpn && cat /proc/net/tcp /proc/net/tcp6 | awk '{print $2,$3,$4,$10}'`,
		"Building service map — reading /proc/net/tcp, process metadata, and active connections")

	snap, err := servicemap.Capture()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(snap)
}

// ConnectionStats handles GET /api/servicemap/connections
// Returns aggregated historical outbound connection stats from SQLite.
func (h *ServiceMapHandler) ConnectionStats(w http.ResponseWriter, r *http.Request) {
	h.learn.Echo(r.Context(),
		"cat /proc/net/nf_conntrack | awk '{print $3,$8,$9,$10,$11}'",
		"Querying aggregated outbound connection history from the service map database")

	rows, err := h.db.Query(`
		SELECT src_process, dst_ip, dst_port, protocol, service_hint, hit_count, first_seen, last_seen
		FROM sm_connection_stats
		ORDER BY hit_count DESC
		LIMIT 500
	`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type stat struct {
		SrcProcess  string    `json:"src_process"`
		DstIP       string    `json:"dst_ip"`
		DstPort     int       `json:"dst_port"`
		Protocol    string    `json:"protocol"`
		ServiceHint string    `json:"service_hint"`
		HitCount    int64     `json:"hit_count"`
		FirstSeen   time.Time `json:"first_seen"`
		LastSeen    time.Time `json:"last_seen"`
	}

	var stats []stat
	for rows.Next() {
		var s stat
		var firstStr, lastStr string
		if err := rows.Scan(&s.SrcProcess, &s.DstIP, &s.DstPort, &s.Protocol,
			&s.ServiceHint, &s.HitCount, &firstStr, &lastStr); err != nil {
			continue
		}
		s.FirstSeen, _ = time.Parse("2006-01-02 15:04:05", firstStr)
		s.LastSeen, _ = time.Parse("2006-01-02 15:04:05", lastStr)
		stats = append(stats, s)
	}
	if stats == nil {
		stats = []stat{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"connections": stats,
		"queried_at":  time.Now().UTC(),
	})
}

// Topology handles GET /api/servicemap/topology
// Returns a graph of nodes + edges derived from live connections + DB history.
func (h *ServiceMapHandler) Topology(w http.ResponseWriter, r *http.Request) {
	snap, err := servicemap.Capture()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type Node struct {
		ID          string `json:"id"`
		Label       string `json:"label"`
		Type        string `json:"type"` // "local-service" | "remote-host"
		ServiceType string `json:"service_type,omitempty"`
		Port        int    `json:"port,omitempty"`
		IP          string `json:"ip,omitempty"`
	}
	type Edge struct {
		Source      string `json:"source"`
		Target      string `json:"target"`
		Protocol    string `json:"protocol"`
		ServiceHint string `json:"service_hint,omitempty"`
		Count       int64  `json:"count"`
	}

	nodeMap := map[string]*Node{}
	var edges []Edge

	for _, svc := range snap.Services {
		id := nodeID("local", svc.Name, svc.Port)
		if _, ok := nodeMap[id]; !ok {
			label := svc.DisplayName
			if label == svc.Name {
				label = svc.Name
			}
			nodeMap[id] = &Node{
				ID:          id,
				Label:       label + " :" + smItoa(svc.Port),
				Type:        "local-service",
				ServiceType: string(svc.ServiceType),
				Port:        svc.Port,
			}
		}
		for _, ob := range svc.Outbound {
			remoteID := nodeID("remote", ob.DstIP, ob.DstPort)
			if _, ok := nodeMap[remoteID]; !ok {
				label := ob.DstIP + ":" + smItoa(ob.DstPort)
				if ob.ServiceHint != "" {
					label += " (" + ob.ServiceHint + ")"
				}
				nodeMap[remoteID] = &Node{
					ID:    remoteID,
					Label: label,
					Type:  "remote-host",
					IP:    ob.DstIP,
					Port:  ob.DstPort,
				}
			}
			edges = append(edges, Edge{
				Source:      id,
				Target:      remoteID,
				Protocol:    ob.Protocol,
				ServiceHint: ob.ServiceHint,
				Count:       1,
			})
		}
	}

	// Enrich edge counts from historical DB data
	rows, _ := h.db.Query(`
		SELECT src_process, dst_ip, dst_port, protocol, service_hint, hit_count
		FROM sm_connection_stats ORDER BY hit_count DESC LIMIT 200
	`)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var srcProc, dstIP, proto, hint string
			var dstPort int
			var count int64
			if err := rows.Scan(&srcProc, &dstIP, &dstPort, &proto, &hint, &count); err != nil {
				continue
			}
			// Find matching local service node
			var srcID string
			for _, svc := range snap.Services {
				if svc.Name == srcProc {
					srcID = nodeID("local", svc.Name, svc.Port)
					break
				}
			}
			if srcID == "" {
				continue
			}
			remoteID := nodeID("remote", dstIP, dstPort)
			if _, ok := nodeMap[remoteID]; !ok {
				label := dstIP + ":" + smItoa(dstPort)
				if hint != "" {
					label += " (" + hint + ")"
				}
				nodeMap[remoteID] = &Node{
					ID:    remoteID,
					Label: label,
					Type:  "remote-host",
					IP:    dstIP,
					Port:  dstPort,
				}
			}
			// Update or add edge
			found := false
			for i := range edges {
				if edges[i].Source == srcID && edges[i].Target == remoteID {
					edges[i].Count = count
					found = true
					break
				}
			}
			if !found {
				edges = append(edges, Edge{
					Source:      srcID,
					Target:      remoteID,
					Protocol:    proto,
					ServiceHint: hint,
					Count:       count,
				})
			}
		}
	}

	nodes := make([]*Node, 0, len(nodeMap))
	for _, n := range nodeMap {
		nodes = append(nodes, n)
	}
	if edges == nil {
		edges = []Edge{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"nodes": nodes,
		"edges": edges,
	})
}

func nodeID(kind, name string, port int) string {
	return kind + ":" + name + ":" + strconv.Itoa(port)
}

func smItoa(n int) string { return strconv.Itoa(n) }
