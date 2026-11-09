// Command node-dash serves a simple dashboard for a Bitcoin Core node.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/poller"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/server"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/source"
	"github.com/mvanasseldonk/bitcoin-node-dash/web"
)

const (
	pollInterval = 10 * time.Second
	fetchTimeout = 5 * time.Second
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	listen := envOr("NODEDASH_LISTEN", ":8080")

	// Until the Bitcoin Core RPC source lands, the dashboard runs on mock data.
	scenario := envOr("NODEDASH_MOCK", "healthy")
	src, err := source.NewMock(scenario)
	if err != nil {
		return err
	}
	log.Info("using mock data", "scenario", scenario)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	p := poller.New(src, pollInterval, fetchTimeout, log)
	go p.Run(ctx)

	srv := &http.Server{
		Addr:              listen,
		Handler:           server.New(p, web.Dist()),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		log.Info("dashboard listening", "addr", listen)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
