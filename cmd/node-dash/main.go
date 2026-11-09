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
	"path/filepath"
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

	src, err := newSource(log)
	if err != nil {
		return err
	}

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

// newSource returns the mock source when NODEDASH_MOCK is set,
// and the Bitcoin Core RPC source otherwise.
func newSource(log *slog.Logger) (source.Source, error) {
	if scenario := os.Getenv("NODEDASH_MOCK"); scenario != "" {
		log.Info("using mock data", "scenario", scenario)
		return source.NewMock(scenario)
	}

	dataDir := os.Getenv("BITCOIN_DATADIR")
	if dataDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dataDir = filepath.Join(home, ".bitcoin")
		}
	}
	cfg := source.RPCConfig{
		URL:        envOr("BITCOIN_RPC_URL", "http://127.0.0.1:8332"),
		CookiePath: envOr("BITCOIN_RPC_COOKIE", filepath.Join(dataDir, ".cookie")),
		User:       os.Getenv("BITCOIN_RPC_USER"),
		Password:   os.Getenv("BITCOIN_RPC_PASSWORD"),
		DataDir:    dataDir,
	}
	auth := "cookie " + cfg.CookiePath
	if cfg.User != "" {
		auth = "user " + cfg.User
	}
	log.Info("using Bitcoin Core RPC", "url", cfg.URL, "auth", auth, "datadir", cfg.DataDir)
	return source.NewRPC(cfg), nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
