// Command node-dash serves a simple dashboard for a Bitcoin Core node.
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
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/control"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/poller"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/release"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/server"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/source"
	"github.com/mvanasseldonk/bitcoin-node-dash/web"
)

const (
	pollInterval = 10 * time.Second
	fetchTimeout = 5 * time.Second
)

// version is set at build time with -ldflags "-X main.version=v1.2.3".
var version = "dev"

const usage = `Usage: node-dash [command]

Commands:
  (none)       run the dashboard
  healthcheck  exit 0 if the dashboard on NODEDASH_LISTEN responds (for Docker HEALTHCHECK)
  version      print the version

Configuration is read from environment variables; see the README.
`

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "--version", "-v":
			fmt.Println(version)
			return
		case "healthcheck":
			if err := healthcheck(envOr("NODEDASH_LISTEN", ":8080")); err != nil {
				fmt.Fprintln(os.Stderr, "unhealthy:", err)
				os.Exit(1)
			}
			return
		default:
			fmt.Fprint(os.Stderr, usage)
			os.Exit(2)
		}
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// healthcheck asks the locally running dashboard whether it is up.
func healthcheck(listen string) error {
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("parse NODEDASH_LISTEN %q: %w", listen, err)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort("127.0.0.1", port) + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func run(log *slog.Logger) error {
	listen := envOr("NODEDASH_LISTEN", ":8080")
	log.Info("starting node-dash", "version", version)

	src, err := newSource(log)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	p := poller.New(src, pollInterval, fetchTimeout, log)
	switch {
	case os.Getenv("NODEDASH_MOCK") != "":
		p.UseReleases(release.Fixed{Major: 32, Minor: 1})
	case os.Getenv("NODEDASH_UPDATE_CHECK") == "0":
		log.Info("update check off: no upgrade advice")
	default:
		// Once a day, ask GitHub for the latest Bitcoin Core release (story #24).
		checker := release.NewChecker(version, log)
		go checker.Run(ctx)
		p.UseReleases(checker)
	}
	go p.Run(ctx)

	var opts []server.Option
	if os.Getenv("NODEDASH_CONTROL") == "1" {
		unit := envOr("BITCOIN_SERVICE", "bitcoind.service")
		ready := func() bool {
			s := p.Snapshot()
			return s.Reachable && s.Health.Level != model.Starting
		}
		var hosts []string
		if v := os.Getenv("NODEDASH_HOSTS"); v != "" {
			hosts = strings.Split(v, ",")
		}
		ctrl := control.New(unit, ready, log)
		if os.Getenv("NODEDASH_MOCK") != "" {
			ctrl = control.NewWithRunner(unit, control.MockRunner(), ready, log)
		}
		opts = append(opts, server.WithControl(ctrl, hosts))
		log.Info("node control enabled", "unit", unit)
	}

	srv := &http.Server{
		Addr:              listen,
		Handler:           server.New(p, web.Dist(), opts...),
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
		User:       os.Getenv("BITCOIN_RPC_USER"),
		Password:   os.Getenv("BITCOIN_RPC_PASSWORD"),
		ConfPath:   envOr("BITCOIN_CONF", filepath.Join(dataDir, "bitcoin.conf")),
		CookiePath: envOr("BITCOIN_RPC_COOKIE", filepath.Join(dataDir, ".cookie")),
		DataDir:    dataDir,
	}
	rpc := source.NewRPC(cfg)
	log.Info("using Bitcoin Core RPC", "url", cfg.URL, "auth", rpc.AuthSource(), "datadir", cfg.DataDir)
	return rpc, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
