package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gyaneshwarpardhi/ifttt/internal/action"
	"github.com/gyaneshwarpardhi/ifttt/internal/action/points"
	"github.com/gyaneshwarpardhi/ifttt/internal/api"
	"github.com/gyaneshwarpardhi/ifttt/internal/config"
	"github.com/gyaneshwarpardhi/ifttt/internal/dag"
	"github.com/gyaneshwarpardhi/ifttt/internal/engine"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	cfgPath := flag.String("config", "configs/rules.yaml", "Path to rules YAML config")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// ── Load config ──────────────────────────────────────────────────────────
	loader, err := config.NewLoader(*cfgPath)
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}
	cfg := loader.Config()
	if err := config.Validate(cfg); err != nil {
		slog.Error("config validation failed", "err", err)
		os.Exit(1)
	}

	// ── Build initial DAG ─────────────────────────────────────────────────────
	g, err := dag.Build(cfg)
	if err != nil {
		slog.Error("failed to build DAG", "err", err)
		os.Exit(1)
	}
	slog.Info("DAG built", "nodes", g.NodeCount(), "scenarios", len(cfg.Scenarios))

	// ── Action registry ───────────────────────────────────────────────────────
	reg := action.NewRegistry()
	reg.Register(points.New())

	// ── Engine ────────────────────────────────────────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eng := engine.New(ctx, g, reg, cfg.Engine)

	// ── Hot-reload watcher ────────────────────────────────────────────────────
	loader.OnChange(func(newCfg *config.RuleConfig) {
		if err := config.Validate(newCfg); err != nil {
			slog.Warn("hot-reload skipped: config invalid", "err", err)
			return
		}
		newGraph, err := dag.Build(newCfg)
		if err != nil {
			slog.Warn("hot-reload skipped: DAG build failed", "err", err)
			return
		}
		eng.SwapGraph(newGraph)
		slog.Info("DAG hot-reloaded", "nodes", newGraph.NodeCount())
	})
	stopWatch, err := loader.Watch()
	if err != nil {
		slog.Warn("config watcher unavailable (hot-reload disabled)", "err", err)
	} else {
		defer stopWatch()
	}

	// ── HTTP server ───────────────────────────────────────────────────────────
	handler := api.New(eng, loader)
	srv := &http.Server{
		Addr:         *addr,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("server starting", "addr", *addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	// ── Graceful shutdown ─────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("shutting down…")

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutCancel()
	_ = srv.Shutdown(shutCtx)
	cancel() // stop worker pools
	eng.Shutdown()
	slog.Info("goodbye")
}
