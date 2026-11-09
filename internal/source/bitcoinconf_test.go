package source

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseConf(t *testing.T) {
	tests := []struct {
		name       string
		conf       string
		user, pass string
	}{
		{"top of file", "server=1\nrpcuser=alice\nrpcpassword=secret\n", "alice", "secret"},
		{"spaces and comments", "  rpcuser = alice   # the dashboard\n# rpcpassword=old\nrpcpassword= secret \n", "alice", "secret"},
		{"main section wins over top", "rpcuser=top\nrpcpassword=toppw\n[main]\nrpcuser=alice\nrpcpassword=secret\n", "alice", "secret"},
		{"other network sections are skipped", "rpcuser=alice\nrpcpassword=secret\n[test]\nrpcuser=bob\nrpcpassword=testpw\n[signet]\nrpcpassword=signetpw\n", "alice", "secret"},
		{"main. prefix", "main.rpcuser=alice\nmain.rpcpassword=secret\ntest.rpcuser=bob\n", "alice", "secret"},
		{"first value wins", "rpcuser=alice\nrpcuser=bob\nrpcpassword=secret\n", "alice", "secret"},
		{"password with = sign", "rpcuser=alice\nrpcpassword=se=cret\n", "alice", "se=cret"},
		{"windows line endings", "rpcuser=alice\r\nrpcpassword=secret\r\n", "alice", "secret"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := parseConf(strings.NewReader(tt.conf))
			if u, p := s.get("rpcuser"), s.get("rpcpassword"); u != tt.user || p != tt.pass {
				t.Fatalf("got %q/%q, want %q/%q", u, p, tt.user, tt.pass)
			}
		})
	}
}

func TestConfCredentialsIncomplete(t *testing.T) {
	dir := t.TempDir()
	for name, conf := range map[string]string{
		"only rpcauth":   "rpcauth=alice:abc$def\n",
		"no password":    "rpcuser=alice\n",
		"other network":  "[test]\nrpcuser=alice\nrpcpassword=secret\n",
		"empty password": "rpcuser=alice\nrpcpassword=\n",
	} {
		path := filepath.Join(dir, name+".conf")
		if err := os.WriteFile(path, []byte(conf), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, ok := confCredentials(path); ok {
			t.Errorf("%s: got credentials, want none", name)
		}
	}
	if _, _, ok := confCredentials(filepath.Join(dir, "missing.conf")); ok {
		t.Error("missing file: got credentials, want none")
	}
}

// writeFiles creates bitcoin.conf and .cookie in a temp data directory.
func writeFiles(t *testing.T, conf, cookie string) (confPath, cookiePath string) {
	t.Helper()
	dir := t.TempDir()
	confPath, cookiePath = filepath.Join(dir, "bitcoin.conf"), filepath.Join(dir, ".cookie")
	if conf != "" {
		if err := os.WriteFile(confPath, []byte(conf), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if cookie != "" {
		if err := os.WriteFile(cookiePath, []byte(cookie), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return confPath, cookiePath
}

func TestRPCCredentialPrecedence(t *testing.T) {
	const conf = "server=1\nrpcuser=alice\nrpcpassword=fromconf\n"
	const staleCookie = "__cookie__:stale"

	tests := []struct {
		name               string
		cfg                RPCConfig
		conf, cookie       string
		wantUser, wantPass string
		wantSource         string
	}{
		{"environment wins", RPCConfig{User: "env", Password: "envpw"}, conf, staleCookie, "env", "envpw", "user env"},
		{"bitcoin.conf wins over a stale cookie", RPCConfig{}, conf, staleCookie, "alice", "fromconf", "user alice ("},
		{"cookie when bitcoin.conf has no password", RPCConfig{}, "server=1\nrpcauth=alice:abc$def\n", "__cookie__:fresh", "__cookie__", "fresh", "cookie "},
		{"cookie when bitcoin.conf is missing", RPCConfig{}, "", "__cookie__:fresh", "__cookie__", "fresh", "cookie "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			confPath, cookiePath := writeFiles(t, tt.conf, tt.cookie)
			cfg := tt.cfg
			cfg.ConfPath, cfg.CookiePath = confPath, cookiePath
			srv := fakeCore(t, tt.wantUser, tt.wantPass, okResults)
			defer srv.Close()
			cfg.URL = srv.URL

			r := NewRPC(cfg)
			if _, err := r.Fetch(context.Background()); err != nil {
				t.Fatalf("fetch: %v", err)
			}
			src := r.AuthSource()
			if !strings.HasPrefix(src, tt.wantSource) {
				t.Fatalf("AuthSource() = %q, want prefix %q", src, tt.wantSource)
			}
			if strings.Contains(src, tt.wantPass) {
				t.Fatalf("AuthSource() leaks the password: %q", src)
			}
		})
	}
}
