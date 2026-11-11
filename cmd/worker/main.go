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

	// The runtime image is plain Alpine with no zoneinfo; the job needs the zone of every tenant, so the database travels in the binary.
	_ "time/tzdata"

	"github.com/code-corhuila/multi-tour-worker-runtime/health"
	"github.com/code-corhuila/multi-tour-worker-runtime/httpclient"
	"github.com/code-corhuila/multi-tour-worker-runtime/logging"
	"github.com/code-corhuila/multi-tour-worker-runtime/runner"

	alertlog "github.com/code-corhuila/multi-tour-worker-cash-close/internal/adapter/out/alert"
	cashhttp "github.com/code-corhuila/multi-tour-worker-cash-close/internal/adapter/out/http"
	"github.com/code-corhuila/multi-tour-worker-cash-close/internal/application/usecase"
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

// run starts the worker: the configuration is read once and a bad one stops it. The job is the only one of this worker.
func run() error {
	cfg, err := config.LoadClose(os.Getenv)
	if err != nil {
		return err
	}
	log := logging.New(os.Stdout)
	cash := httpclient.New(cfg.CashReportingURL, cfg.Runtime.ServiceToken,
		httpclient.WithTimeout(cfg.Runtime.HTTPTimeout), httpclient.WithAttempts(cfg.Runtime.HTTPAttempts))
	job := usecase.NewCloseDays(cashhttp.NewCashDays(cash), alertlog.NewLog(log), log, time.Now)
	jobRunner, err := runner.New(job, runner.Options{Interval: cfg.Interval, BatchSize: cfg.Runtime.BatchSize,
		RunTimeout: cfg.Runtime.RunTimeout, Logger: log})
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
	runnerDone := make(chan struct{})
	go func() { jobRunner.Start(ctx); close(runnerDone) }()

	var failure error
	select {
	case <-ctx.Done():
	case failure = <-serverErr:
		stop()
	}
	// Ordered shutdown: the run in flight ends first (bounded by RUN_TIMEOUT), then the health server.
	<-runnerDone
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
