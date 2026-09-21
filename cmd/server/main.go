package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/jwt"
	"github.com/Abshoor24/backend-go/internal/modules/ai/provider"
	airepo "github.com/Abshoor24/backend-go/internal/modules/ai/repository"
	aiservice "github.com/Abshoor24/backend-go/internal/modules/ai/service"
	executionrepo "github.com/Abshoor24/backend-go/internal/modules/execution/repository"
	executionservice "github.com/Abshoor24/backend-go/internal/modules/execution/service"
	"github.com/Abshoor24/backend-go/internal/repository"
	"github.com/Abshoor24/backend-go/internal/routes"
	"github.com/Abshoor24/backend-go/internal/service"
	"github.com/Abshoor24/backend-go/pkg/logger"
	"github.com/Abshoor24/backend-go/storage"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := logger.New(cfg.App.Env)
	slog.SetDefault(log)

	// Context cancelled by SIGINT (Ctrl+C) or SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	supabase, err := storage.New(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer supabase.Close()

	tokens := jwt.NewManager(cfg.JWT)
	userRepo := repository.NewUserRepository(supabase.Pool())
	authService := service.NewAuthService(userRepo, tokens, log)

	aiProvider, err := provider.New(cfg.AI, log)
	if err != nil {
		return err
	}
	aiRepo := airepo.New(supabase.Pool())
	aiService := aiservice.New(aiProvider, cfg.AI, cfg.Upload, log, aiRepo)

	execProvider := provider.NewJudgeZeroProvider(cfg.Execution, log)
	execRepo := executionrepo.New(supabase.Pool())
	execService := executionservice.New(execProvider, cfg.Execution, log, execRepo)

	srv := &http.Server{
		Addr:         net.JoinHostPort(cfg.Server.Host, cfg.Server.Port),
		Handler:      routes.New(routes.Dependencies{Config: cfg, Logger: log, DB: supabase, Auth: authService, AI: aiService, Execution: execService, Tokens: tokens}),
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	// Serve in the background; report unexpected errors.
	errCh := make(chan error, 1)
	go func() {
		log.Info("server starting",
			"addr", srv.Addr,
			"env", cfg.App.Env,
			"app", cfg.App.Name,
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	// Wait for a shutdown signal or a server error.
	select {
	case <-ctx.Done():
		log.Info("shutdown signal received")
	case err := <-errCh:
		return fmt.Errorf("server: %w", err)
	}

	// Graceful shutdown with a deadline. The deadline must comfortably exceed
	// the longest expected request (AI_TIMEOUT is 30s, Gemini can take a while)
	// so in-flight requests are not cut off during a restart/reload.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server: shutdown: %w", err)
	}

	log.Info("server stopped gracefully")
	return nil
}
