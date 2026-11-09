package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/code-corhuila/multi-tour-worker-runtime/health"

	"github.com/code-corhuila/multi-tour-worker-cash-close/internal/config"
)

// The health server's limits are declared here, not left to defaults (Annex D).
const (
	readHeaderTimeout = 5 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 20 * time.Second
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "cash-close:", err)
		os.Exit(1)
	}
}

// run starts the worker: the configuration is read once and a bad one stops it. The job joins in task WK8-02.
func run() error {
	cfg, err := config.LoadClose(os.Getenv)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("GET /health", health.Handler())
	server := &http.Server{Addr: cfg.HealthAddr, Handler: mux, ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout: writeTimeout, IdleTimeout: idleTimeout}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.ListenAndServe() }()

	var failure error
	select {
	case <-ctx.Done():
	case failure = <-serverErr:
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil && failure == nil {
		failure = err
	}
	if errors.Is(failure, http.ErrServerClosed) {
		return nil
	}
	return failure
}
