// Webux — A lightweight Linux management panel
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/brendan4linux/webux/internal/api"
	"github.com/brendan4linux/webux/internal/auth"
	"github.com/brendan4linux/webux/internal/config"
	"github.com/brendan4linux/webux/internal/db"
	"github.com/brendan4linux/webux/internal/system"
	"github.com/brendan4linux/webux/internal/system/hardening"
	webuxTLS "github.com/brendan4linux/webux/internal/tlsutil"
	"github.com/brendan4linux/webux/internal/ws"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	var (
		cfgPath     = flag.String("config", "/etc/webux/config.yaml", "Path to config file")
		showVersion = flag.Bool("version", false, "Print version and exit")
		noAuth      = flag.Bool("no-auth", false, "Disable authentication (development only)")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("webux %s (%s) built %s\n", version, commit, date)
		fmt.Printf("auth backend: %s\n", auth.BuildInfo())
		os.Exit(0)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}
	if *noAuth {
		cfg.Auth.Disabled = true
	}

	database, err := db.Open(cfg.DataDir + "/webux.db")
	if err != nil {
		slog.Error("failed to open database", "err", err)
		os.Exit(1)
	}
	defer database.Close()

	if err := db.Migrate(database); err != nil {
		slog.Error("migration failed", "err", err)
		os.Exit(1)
	}

	// Run security hardening checks in the background at startup
	go func() {
		score := hardening.RunAll()
		b, err := json.Marshal(score)
		if err == nil {
			database.Exec(
				`INSERT INTO webux_settings(key, value) VALUES(?, ?)
				 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
				"hardening.results", string(b),
			)
		}
	}()

	// Ensure JWT secret exists — generate if missing
	jwtSecret := cfg.Auth.JWTSecret
	if jwtSecret == "" {
		var stored string
		database.QueryRow("SELECT value FROM webux_settings WHERE key='auth.jwt_secret'").Scan(&stored)
		if stored == "" {
			stored = auth.GenerateSecret()
			database.Exec(`INSERT INTO webux_settings (key,value) VALUES ('auth.jwt_secret',?)
				ON CONFLICT(key) DO UPDATE SET value=excluded.value`, stored)
			slog.Info("generated new JWT secret")
		}
		jwtSecret = stored
	}

	bypassToken := cfg.Auth.BypassToken
	if bypassToken == "" {
		database.QueryRow("SELECT value FROM webux_settings WHERE key='auth.bypass_token'").Scan(&bypassToken)
	}

	authMgr := auth.NewManager([]byte(jwtSecret), bypassToken)

	hostInfo, err := system.Detect()
	if err != nil {
		slog.Warn("host detection partial", "err", err)
	}
	slog.Info("host detected",
		"distro", hostInfo.Distro,
		"init", hostInfo.InitSystem,
		"arch", hostInfo.Arch,
	)

	hub := ws.NewHub()
	go hub.Run()

	webDist, err := fs.Sub(webFS, "dist")
	if err != nil {
		slog.Error("failed to sub web fs", "err", err)
		os.Exit(1)
	}

	router := api.NewRouter(api.RouterConfig{
		DB:       database,
		Config:   cfg,
		Hub:      hub,
		HostInfo: hostInfo,
		WebFS:    webDist,
		AuthMgr:  authMgr,
	})

	// TLS — load provided cert/key or auto-generate self-signed
	tlsCfg, err := webuxTLS.Load(webuxTLS.Config{
		CertFile: cfg.TLSCertFile,
		KeyFile:  cfg.TLSKeyFile,
		DataDir:  cfg.DataDir,
	})
	if err != nil {
		slog.Error("TLS setup failed", "err", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      router,
		TLSConfig:    tlsCfg,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // no write timeout — SSE streams (puppet, ansible, packages) can run for minutes
		IdleTimeout:  120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		authMode := "PAM"
		if cfg.Auth.Disabled {
			authMode = "DISABLED"
		} else if !auth.PAMAvailable() {
			authMode = "shadow"
		}
		slog.Info("webux listening",
			"addr", "https://"+cfg.ListenAddr,
			"auth", authMode,
			"bypass_token", bypassToken != "",
		)
		// Empty strings — TLS config already contains the certificate
		if err := srv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "err", err)
	}
}
