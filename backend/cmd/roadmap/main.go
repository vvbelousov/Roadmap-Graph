package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"roadmap-graph/backend/internal/server"
	"syscall"
	"time"
)

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func main() {
	var level slog.Level
	if e := level.UnmarshalText([]byte(env("ROADMAP_LOG_LEVEL", "info"))); e != nil {
		slog.Error("Invalid ROADMAP_LOG_LEVEL; use debug, info, warn or error")
		os.Exit(1)
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))
	db, e := server.OpenDB(env("ROADMAP_DB", "data/roadmap.db"))
	if e != nil {
		slog.Error("database failed", "error", e)
		os.Exit(1)
	}
	defer db.Close()
	app, e := server.New(db, server.Config{Password: os.Getenv("ROADMAP_PASSWORD"), StaticDir: env("ROADMAP_STATIC", "../frontend/dist"), SecureCookie: env("ROADMAP_COOKIE_SECURE", "true") != "false", Demo: env("ROADMAP_DEMO", "false") == "true"})
	if e != nil {
		slog.Error("configuration failed", "error", e)
		os.Exit(1)
	}
	srv := &http.Server{Addr: env("ROADMAP_ADDR", ":8080"), Handler: app, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 6 * time.Minute, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	app.StartTracker(ctx)
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(c)
	}()
	slog.Info("listening", "address", srv.Addr)
	if e = srv.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		slog.Error("server stopped", "error", e)
		os.Exit(1)
	}
}
