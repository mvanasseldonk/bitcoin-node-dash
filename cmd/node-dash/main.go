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
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/details"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/minisign"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/peers"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/poller"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/release"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/server"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/settings"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/source"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/updater"
	"github.com/mvanasseldonk/bitcoin-node-dash/internal/websettings"
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
  verify       check a release: node-dash verify checksums.txt checksums.txt.minisig v1.2.3
  update       install the latest release, as root (run by node-dash-update.service)

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
		case "verify":
			os.Exit(verify(os.Args[2:]))
		case "update":
			os.Exit(selfUpdate(os.Args[2:]))
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

// verify checks a release's checksums.txt against its signature with the
// release key in this binary (story #50). install.sh uses it.
func verify(args []string) int {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Println("Usage: node-dash verify checksums.txt checksums.txt.minisig v1.2.3")
		fmt.Println("Checks the signature with the release key " + minisign.ReleaseKey().ID() + ".")
		return 0
	}
	if len(args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: node-dash verify checksums.txt checksums.txt.minisig v1.2.3")
		return 2
	}
	sums, err := os.ReadFile(args[0])
	if err == nil {
		var sig []byte
		if sig, err = os.ReadFile(args[1]); err == nil {
			err = minisign.VerifyRelease(minisign.ReleaseKey(), args[2], sums, sig)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "signature check failed:", err)
		return 1
	}
	fmt.Println("signature ok: node-dash", args[2], "signed with key", minisign.ReleaseKey().ID())
	return 0
}

// selfUpdate installs the latest release (story #50). It runs as root from
// node-dash-update.service; see internal/updater.
func selfUpdate(args []string) int {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Println("Usage: node-dash update")
		fmt.Println("Installs the latest release of the same major version, as root. Run by node-dash-update.service.")
		return 0
	}
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "usage: node-dash update")
		return 2
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	st := updater.Run(context.Background(), updater.Defaults(version, log))
	if st.Result == model.UpdateRefused || st.Result == model.UpdateFailed {
		return 1
	}
	return 0
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

	name, err := appName(os.Getenv("NODEDASH_NAME"))
	if err != nil {
		return err
	}

	src, err := newSource(log)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	p := poller.New(src, pollInterval, fetchTimeout, log)
	// The server's figures for the meters on the home page (#65).
	if m, ok := src.(*source.Mock); ok {
		p.UseServer(m)
	} else {
		p.UseServer(source.NewSystem(dataDir()))
	}
	// The Docker image sets NODEDASH_DOCKER=1: it is updated by pulling the image.
	inDocker := os.Getenv("NODEDASH_DOCKER") == "1"
	var hosts []string
	if v := os.Getenv("NODEDASH_HOSTS"); v != "" {
		hosts = strings.Split(v, ",")
	}

	// The dashboard's own settings: the environment, and what was changed on
	// its settings page (story #61, decision 0004).
	dashSettings, err := webSettings(name, hosts, inDocker, log)
	if err != nil {
		return err
	}
	updateCheck := dashSettings.UpdateCheck

	// The dashboard's own version and latest release (story #39).
	dashVersion := version
	var dashLatest release.Latest // nil: no update check
	if os.Getenv("NODEDASH_MOCK") != "" {
		p.UseReleases(release.Fixed{Major: 32, Minor: 1})
		dashVersion, dashLatest = mockDashboard(os.Getenv("NODEDASH_MOCK"), version)
	} else {
		if !updateCheck() {
			log.Info("update check off: no upgrade advice, no notice of new dashboard versions")
		}
		// Once a day, ask GitHub for the latest Bitcoin Core release (story
		// #24), while the update check is on.
		checker := release.NewChecker(version, log)
		checker.Enabled = updateCheck
		go checker.Run(ctx)
		p.UseReleases(release.Gated{L: checker, On: updateCheck})
		// And for the dashboard's own, unless this is a local build.
		if _, ok := release.ParseVersion(version); ok {
			own := release.NewDashboardChecker(version, log)
			own.Enabled = updateCheck
			go own.Run(ctx)
			dashLatest = own
		}
	}
	go p.Run(ctx)
	// The status of the automatic updates (story #50), written by the updater.
	updateStatus := func() *model.AutoUpdate {
		return updater.ReadStatus(filepath.Join(updater.DefaultStateDir, updater.StatusFile))
	}
	if scenario := os.Getenv("NODEDASH_MOCK"); scenario != "" {
		updateStatus = func() *model.AutoUpdate { return mockAutoUpdate(scenario, dashVersion) }
	}
	opts := []server.Option{
		server.WithBrand(dashSettings.Name, dashSettings.Ancoro),
		server.WithDashboard(func() model.Dashboard {
			latest := dashLatest
			if latest != nil && !updateCheck() {
				latest = nil
			}
			d := release.Dashboard(dashVersion, latest, inDocker)
			d.AutoUpdate = updateStatus()
			return d
		}),
	}
	// On by default; NODEDASH_WEB_SETTINGS=0 turns the page off (#61).
	if os.Getenv("NODEDASH_WEB_SETTINGS") != "0" {
		opts = append(opts, server.WithWebSettings(dashSettings, hosts))
	} else {
		log.Info("dashboard settings page off (NODEDASH_WEB_SETTINGS=0)")
	}
	switchOn := func(sw string) func() bool { return func() bool { return dashSettings.On(sw) } }
	// Changing settings (story #55) is opt-in, and not for Docker, where
	// bitcoin.conf is mounted read-only (decision 0003).
	editSettings := os.Getenv("NODEDASH_SETTINGS") == "1"
	if editSettings && inDocker {
		log.Warn("NODEDASH_SETTINGS=1 is not supported in Docker: the node settings stay read-only")
		editSettings = false
	}
	if ds, ok := src.(source.DetailSource); ok {
		var sys source.ServerReader
		var conf source.SettingsReader
		var editor *settings.Reader
		stateDir := settingsStateDir()
		if m, ok := src.(*source.Mock); ok {
			sys, conf = m, m
			if editSettings {
				r, dir, err := mockSettings(os.Getenv("NODEDASH_MOCK"))
				if err != nil {
					return err
				}
				conf, editor, stateDir = r, r, dir
			}
		} else {
			sys = source.NewSystem(dataDir())
			r := settings.New(confPath(), dataDir())
			conf = r
			if editSettings {
				editor = r
			}
		}
		det := details.New(ds, sys, conf, log)
		opts = append(opts, server.WithDetails(det))
		if editor != nil {
			editor.AllowWhile(switchOn(websettings.Settings))
			service := envOr("BITCOIN_SERVICE", "bitcoind.service")
			if err := editor.EnableEditing(stateDir, service, log); err != nil {
				log.Error("can't turn on changing settings; they stay read-only", "err", err)
			} else {
				opts = append(opts, server.WithSettings(det, editor, hosts, switchOn(websettings.Settings)))
				log.Info("changing settings enabled", "conf", editor.ConfPath, "backups", filepath.Join(stateDir, "backups"))
			}
		}
	}
	if ps, ok := src.(source.PeerSource); ok {
		// Peer addresses only on request: the dashboard has no login (#12).
		addresses := os.Getenv("NODEDASH_PEER_ADDRESSES") == "1"
		opts = append(opts, server.WithPeers(peers.New(ps, addresses, log).AddressesWhile(switchOn(websettings.PeerAddresses))))
	}
	if os.Getenv("NODEDASH_CONTROL") == "1" {
		unit := envOr("BITCOIN_SERVICE", "bitcoind.service")
		ready := func() bool {
			s := p.Snapshot()
			return s.Reachable && s.Health.Level != model.Starting
		}
		ctrl := control.New(unit, ready, log)
		if os.Getenv("NODEDASH_MOCK") != "" {
			ctrl = control.NewWithRunner(unit, control.MockRunner(), ready, log)
		}
		opts = append(opts, server.WithControl(ctrl, hosts, switchOn(websettings.Control)))
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

// mockDashboard pretends a released version in mock mode, so the dashboard's
// version row can be seen in a local build: "outdated" has a newer release.
func mockDashboard(scenario, version string) (string, release.Latest) {
	own, ok := release.ParseVersion(version)
	if !ok {
		own = release.Release{Minor: 5}
	}
	latest := own
	if scenario == "outdated" {
		latest = release.Release{Major: own.Major, Minor: own.Minor + 1}
	}
	return own.Tag(), release.Fixed(latest)
}

// mockAutoUpdate pretends automatic updates in mock mode: "healthy" updated
// last night, "warning" had a release that didn't start. Elsewhere they're off.
func mockAutoUpdate(scenario, version string) *model.AutoUpdate {
	last := time.Now().Add(-9 * time.Hour).UTC().Truncate(time.Minute)
	switch scenario {
	case "healthy":
		return &model.AutoUpdate{LastRun: last, Result: model.UpdateUpdated, From: "v0.4.2", To: version}
	case "warning":
		return &model.AutoUpdate{LastRun: last, Result: model.UpdateFailed, From: version, To: "v0.6.1",
			Message: "v0.6.1 didn't start within 30s; back on " + version}
	}
	return nil
}

// newSource returns the mock source when NODEDASH_MOCK is set,
// and the Bitcoin Core RPC source otherwise.
func newSource(log *slog.Logger) (source.Source, error) {
	if scenario := os.Getenv("NODEDASH_MOCK"); scenario != "" {
		log.Info("using mock data", "scenario", scenario)
		return source.NewMock(scenario)
	}

	dataDir := dataDir()
	cfg := source.RPCConfig{
		URL:        envOr("BITCOIN_RPC_URL", "http://127.0.0.1:8332"),
		User:       os.Getenv("BITCOIN_RPC_USER"),
		Password:   os.Getenv("BITCOIN_RPC_PASSWORD"),
		ConfPath:   confPath(),
		CookiePath: envOr("BITCOIN_RPC_COOKIE", filepath.Join(dataDir, ".cookie")),
		DataDir:    dataDir,
	}
	rpc := source.NewRPC(cfg)
	log.Info("using Bitcoin Core RPC", "url", cfg.URL, "auth", rpc.AuthSource(), "datadir", cfg.DataDir)
	return rpc, nil
}

// settingsStateDir holds the backups of bitcoin.conf and the time of the
// last save: StateDirectory= of the drop-in install.sh writes, which systemd
// passes in STATE_DIRECTORY, among others (decision 0004).
func settingsStateDir() string {
	return websettings.Dir(os.Getenv("STATE_DIRECTORY"), "node-dash-settings", "/var/lib/node-dash-settings")
}

// webSettings reads the environment's dashboard settings and opens the
// store of the settings page: StateDirectory=node-dash-web, or a temporary
// directory in mock mode, so the page can be tried.
func webSettings(name string, hosts []string, docker bool, log *slog.Logger) (*websettings.Store, error) {
	env := websettings.Env{
		Ancoro:      boolEnv("NODEDASH_ANCORO"),
		UpdateCheck: boolEnv("NODEDASH_UPDATE_CHECK"),
		On: map[string]bool{
			websettings.Control:       os.Getenv("NODEDASH_CONTROL") == "1",
			websettings.Settings:      os.Getenv("NODEDASH_SETTINGS") == "1",
			websettings.PeerAddresses: os.Getenv("NODEDASH_PEER_ADDRESSES") == "1",
		},
		Listen: os.Getenv("NODEDASH_LISTEN"),
		Hosts:  hosts,
		Docker: docker,
	}
	if name != server.DefaultName || strings.TrimSpace(os.Getenv("NODEDASH_NAME")) != "" {
		env.Name = name
	}
	dir := websettings.Dir(os.Getenv("STATE_DIRECTORY"), "node-dash-web", websettings.DefaultDir)
	if os.Getenv("NODEDASH_MOCK") != "" {
		d, err := os.MkdirTemp("", "node-dash-mock-web-")
		if err != nil {
			return nil, err
		}
		dir = d
	}
	return websettings.New(dir, env, log), nil
}

// boolEnv is a 0/1 environment variable: nil when not set. Like before #61,
// only "0" turns a setting off.
func boolEnv(key string) *bool {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return nil
	}
	on := v != "0"
	return &on
}

// mockSettings is a demo bitcoin.conf in a temporary directory, so the
// settings form can be tried in mock mode. In "outdated", pruning is set on
// the command line.
func mockSettings(scenario string) (*settings.Reader, string, error) {
	dir, err := os.MkdirTemp("", "node-dash-mock-settings-")
	if err != nil {
		return nil, "", err
	}
	conf := "# Demo bitcoin.conf of the mock mode (NODEDASH_MOCK)\nserver=1\nrpcuser=satoshi\nrpcpassword=not-a-real-password\ndbcache=2048\ntxindex=1\n\n[main]\nmaxuploadtarget=5000\n"
	var args []string
	if scenario == "outdated" {
		conf = strings.Replace(conf, "txindex=1\n", "", 1)
		args = []string{"-prune=550"}
	}
	path := filepath.Join(dir, "bitcoin.conf")
	if err := os.WriteFile(path, []byte(conf), 0o600); err != nil {
		return nil, "", err
	}
	return settings.New(path, dir).WithArgs(args), filepath.Join(dir, "state"), nil
}

// confPath is bitcoin.conf: BITCOIN_CONF, or bitcoin.conf in the data directory.
func confPath() string {
	return envOr("BITCOIN_CONF", filepath.Join(dataDir(), "bitcoin.conf"))
}

// dataDir is the Bitcoin data directory: BITCOIN_DATADIR, or ~/.bitcoin.
func dataDir() string {
	if d := os.Getenv("BITCOIN_DATADIR"); d != "" {
		return d
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".bitcoin")
	}
	return ""
}

// appName is the name the operator gave their node (NODEDASH_NAME), or
// "Node". A name that can't be shown well is refused, so the dashboard
// doesn't start with a broken header.
func appName(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return server.DefaultName, nil
	}
	if p := websettings.NameProblem(v); p != "" {
		return "", errors.New("NODEDASH_NAME: " + p)
	}
	return v, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
