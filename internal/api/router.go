package api

import (
	"database/sql"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/brendan4linux/webux/internal/api/handlers"
	"github.com/brendan4linux/webux/internal/auth"
	"github.com/brendan4linux/webux/internal/config"
	"github.com/brendan4linux/webux/internal/learn"
	"github.com/brendan4linux/webux/internal/system"
	"github.com/brendan4linux/webux/internal/system/initsys"
	"github.com/brendan4linux/webux/internal/ws"
)

type RouterConfig struct {
	DB       *sql.DB
	Config   *config.Config
	Hub      *ws.Hub
	HostInfo *system.HostInfo
	WebFS    fs.FS
	AuthMgr  *auth.Manager
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func NewRouter(cfg RouterConfig) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(func(next http.Handler) http.Handler {
    compress := middleware.Compress(5)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/ws") ||
				strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
				next.ServeHTTP(w, r)
				return
			}
			compress(next).ServeHTTP(w, r)
		})
	})

	learnStore := learn.NewStore(cfg.DB)
	learnStore.Subscribe(func(e learn.EchoEntry) {
		cfg.Hub.Broadcast(ws.EventCLIEcho, e)
	})

	initSys := initsys.Detect()

	// ── Auth middleware FIRST — chi requires all Use() before any routes ──
	authH := handlers.NewAuthHandler(cfg.AuthMgr)
	if cfg.Config == nil || !cfg.Config.Auth.Disabled {
		r.Use(cfg.AuthMgr.Middleware)
	}

	// ── Public auth routes (exempted via isPublicPath in middleware) ──────
	r.Post("/auth/login", authH.Login)
	r.Post("/auth/logout", authH.Logout)
	r.Get("/auth/whoami", authH.WhoAmI)

	r.Route("/api", func(r chi.Router) {
		// System
		sysH := handlers.NewSystemHandler(cfg.HostInfo)
		r.Get("/system/info", sysH.Info)
		r.Get("/system/stats", sysH.Stats)

		// Services
		svcH := handlers.NewServicesHandler(initSys, learnStore)
		r.Get("/services", svcH.List)
		r.Get("/services/{name}", svcH.Get)
		r.Post("/services/{name}/action", svcH.Action)
		r.Get("/services/{name}/logs", svcH.Logs)

		// Processes
		procH := handlers.NewProcessesHandler(learnStore)
		r.Get("/processes", procH.List)
		r.Post("/processes/{pid}/kill", procH.Kill)

		// Health checks
		healthH := handlers.NewHealthHandler(cfg.DB)
		r.Get("/health", healthH.Run)
		r.Get("/health/detail", healthH.Detail)
		r.Get("/health/checks", healthH.GetChecks)
		r.Put("/health/checks", healthH.SaveChecks)
		r.Delete("/health/checks", healthH.ResetChecks)

		// Users & groups
		usersH := handlers.NewUsersHandler(learnStore)
		r.Get("/users", usersH.ListUsers)
		r.Post("/users", usersH.CreateUser)
		r.Get("/users/shells", usersH.Shells)
		r.Get("/users/{username}", usersH.GetUser)
		r.Delete("/users/{username}", usersH.DeleteUser)
		r.Post("/users/{username}/lock", usersH.LockUser)
		r.Post("/users/{username}/unlock", usersH.UnlockUser)
		r.Post("/users/{username}/groups", usersH.AddToGroup)
		r.Get("/groups", usersH.ListGroups)
		r.Post("/groups", usersH.CreateGroup)

		// Ports & sockets
		portsH := handlers.NewPortsHandler(learnStore)
		r.Get("/ports", portsH.List)
		r.Get("/ports/sockets", portsH.ListSystemdSockets)

		// Network interfaces & routes
		netH := handlers.NewNetworkHandler(learnStore)
		r.Get("/network/interfaces", netH.ListInterfaces)
		r.Get("/network/routes", netH.ListRoutes)
		r.Post("/network/interfaces/{name}/state", netH.SetState)
		r.Post("/network/interfaces/{name}/addresses", netH.AddAddress)
		r.Delete("/network/interfaces/{name}/addresses", netH.DelAddress)
		r.Post("/network/interfaces/{name}/mtu", netH.SetMTU)
		r.Get("/network/interfaces/{name}/stats/stream", handlers.StreamInterfaceStats)

		// Firewall
		fwH := handlers.NewFirewallHandler(learnStore)
		r.Get("/firewall", fwH.Status)
		r.Post("/firewall/rules", fwH.AddRule)
		r.Delete("/firewall/rules/{id}", fwH.DeleteRule)
		r.Post("/firewall/enable", fwH.Enable)
		r.Post("/firewall/disable", fwH.Disable)

		// Containers (Docker + Podman)
		ctrH := handlers.NewContainersHandler(learnStore)
		r.Get("/containers/status", ctrH.Status)
		r.Get("/containers", ctrH.List)
		r.Post("/containers/{id}/action", ctrH.Action)
		r.Get("/containers/{id}/logs", ctrH.Logs)
		r.Get("/containers/{id}/stats", ctrH.Stats)
		r.Get("/containers/images", ctrH.ListImages)

		// Databases
		dbH := handlers.NewDatabasesHandler(learnStore)
		r.Get("/databases", dbH.Detect)
		r.Post("/databases/connect", dbH.Connect)
		r.Get("/databases/{connID}/databases", dbH.ListDatabases)
		r.Get("/databases/{connID}/tables", dbH.ListTables)
		r.Post("/databases/{connID}/query", dbH.Query)
		r.Delete("/databases/{connID}", dbH.Disconnect)

		// Webservers
		wsrvH := handlers.NewWebserversHandler(learnStore)
		r.Get("/webservers", wsrvH.List)
		r.Get("/webservers/{type}/config", wsrvH.GetConfig)
		r.Put("/webservers/{type}/config", wsrvH.SaveConfig)
		r.Post("/webservers/{type}/test", wsrvH.TestConfig)
		r.Post("/webservers/{type}/reload", wsrvH.Reload)
		r.Get("/webservers/{type}/sites", wsrvH.ListSites)
		r.Post("/webservers/{type}/sites/{site}/enable", wsrvH.EnableSite)
		r.Post("/webservers/{type}/sites/{site}/disable", wsrvH.DisableSite)

		// Files
		filesH := handlers.NewFilesHandler(learnStore)
		r.Get("/files", filesH.List)
		r.Get("/files/read", filesH.Read)
		r.Put("/files/write", filesH.Write)
		r.Delete("/files", filesH.Delete)
		r.Post("/files/mkdir", filesH.Mkdir)
		r.Post("/files/rename", filesH.Rename)
		r.Post("/files/chmod", filesH.Chmod)
		r.Post("/files/upload", filesH.Upload)
		r.Get("/files/download", filesH.Download)

		// Cron
		cronH := handlers.NewCronHandler(learnStore)
		r.Get("/cron", cronH.List)
		r.Get("/cron/schedules", cronH.Schedules)
		r.Post("/cron", cronH.Add)
		r.Put("/cron", cronH.Update)
		r.Delete("/cron", cronH.Delete)
		r.Post("/cron/validate", cronH.Validate)

		// Puppet
		puppetH := handlers.NewPuppetHandler(learnStore)
		r.Get("/puppet/status", puppetH.Status)
		r.Get("/puppet/catalog", puppetH.Catalog)
		r.Get("/puppet/report", puppetH.Report)
		r.Get("/puppet/facts", puppetH.Facts)
		r.Post("/puppet/run", puppetH.Run)
		r.Post("/puppet/enable", puppetH.Enable)
		r.Post("/puppet/disable", puppetH.Disable)

		// Packages
		pkgH := handlers.NewPackagesHandler(learnStore)
		r.Get("/packages/info", pkgH.Info)
		r.Get("/packages", pkgH.ListInstalled)
		r.Get("/packages/upgradable", pkgH.ListUpgradable)
		r.Get("/packages/search", pkgH.Search)
		r.Post("/packages/install", pkgH.Install)
		r.Post("/packages/remove", pkgH.Remove)
		r.Post("/packages/upgrade", pkgH.Upgrade)
		r.Post("/packages/update-cache", pkgH.UpdateCache)
		// Repos
		r.Get("/packages/repos", pkgH.ListRepos)
		r.Post("/packages/repos", pkgH.AddRepo)
		r.Delete("/packages/repos", pkgH.RemoveRepo)
		r.Post("/packages/repos/enable", pkgH.EnableRepo)
		r.Post("/packages/repos/disable", pkgH.DisableRepo)
		r.Get("/packages/repos/flatpak", pkgH.ListFlatpakRemotes)
		r.Post("/packages/repos/flatpak", pkgH.AddFlatpakRemote)
		r.Delete("/packages/repos/flatpak", pkgH.RemoveFlatpakRemote)
		// Flatpak apps
		r.Get("/packages/flatpak", pkgH.ListFlatpaks)
		r.Post("/packages/flatpak/remove", pkgH.RemoveFlatpak)
		r.Post("/packages/flatpak/update", pkgH.UpdateFlatpaks)

		disksH := handlers.NewDisksHandler(learnStore)
		r.Get("/disks", disksH.Summary)
		r.Post("/disks/extend", disksH.Extend)

		// Ansible
		ansibleH := handlers.NewAnsibleHandler(learnStore, cfg.DB)
		r.Get("/ansible/status", ansibleH.Status)
		r.Put("/ansible/settings", ansibleH.Settings)
		r.Get("/ansible/playbooks", ansibleH.ListPlaybooks)
		r.Get("/ansible/inventory", ansibleH.ListInventory)
		r.Post("/ansible/run", ansibleH.Run)
		r.Post("/ansible/install", ansibleH.Install)

		// AI Assistant
		aiH := handlers.NewAIHandler(cfg.DB)
		r.Get("/ai/status", aiH.Status)
		r.Put("/ai/settings", aiH.SaveSettings)
		r.Get("/ai/models", aiH.ListModels)
		r.Post("/ai/models/pull", aiH.PullModel)
		r.Delete("/ai/models", aiH.DeleteModel)
		r.Post("/ai/chat", aiH.Chat)

		// Settings API
		r.Get("/settings/server", func(w http.ResponseWriter, r *http.Request) {
			var port string
			cfg.DB.QueryRow("SELECT value FROM webux_settings WHERE key='server.port'").Scan(&port)
			if port == "" {
				port = "8989"
			}
			writeJSON(w, map[string]interface{}{"port": port})
		})
		r.Put("/settings/server", func(w http.ResponseWriter, r *http.Request) {
			var body struct{ Port string `json:"port"` }
			json.NewDecoder(r.Body).Decode(&body)
			if body.Port != "" {
				cfg.DB.Exec(`INSERT INTO webux_settings (key,value) VALUES ('server.port',?)
					ON CONFLICT(key) DO UPDATE SET value=excluded.value`, body.Port)
			}
			writeJSON(w, map[string]interface{}{"ok": true})
		})
		r.Get("/settings/auth", func(w http.ResponseWriter, r *http.Request) {
			var token string
			cfg.DB.QueryRow("SELECT value FROM webux_settings WHERE key='auth.bypass_token'").Scan(&token)
			writeJSON(w, map[string]interface{}{"bypass_token": token})
		})
		r.Put("/settings/auth", func(w http.ResponseWriter, r *http.Request) {
			var body struct{ BypassToken string `json:"bypass_token"` }
			json.NewDecoder(r.Body).Decode(&body)
			cfg.DB.Exec(`INSERT INTO webux_settings (key,value) VALUES ('auth.bypass_token',?)
				ON CONFLICT(key) DO UPDATE SET value=excluded.value`, body.BypassToken)
			writeJSON(w, map[string]interface{}{"ok": true})
		})

		// Migration
		migH := handlers.NewMigrationHandler(learnStore)
		r.Get("/migration/snapshot", migH.Snapshot)
		r.Get("/migration/template", migH.Template)

		// Learn / CLI echo history
		r.Get("/learn/recent", func(w http.ResponseWriter, r *http.Request) {
			entries := learnStore.Recent(50)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(entries)
		})
	})

	r.Get("/ws", cfg.Hub.ServeWS)

	// Terminal PTY — separate WebSocket, not the event hub
	termH := handlers.NewTerminalHandler(cfg.DB)
	r.Get("/ws/terminal", termH.ServeTerminal)
	r.Get("/api/terminal/settings", termH.Settings)
	r.Put("/api/terminal/settings", termH.SaveSettings)

	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		http.FileServer(http.FS(cfg.WebFS)).ServeHTTP(w, r)
	})

	return r
}
