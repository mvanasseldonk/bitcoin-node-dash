package server

import (
	"encoding/json"
	"errors"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/control"
)

// WithControl adds the node control endpoints (story #10). hosts are extra
// host names, besides IP addresses and localhost, the dashboard may be
// reached on (NODEDASH_HOSTS).
func WithControl(c *control.Controller, hosts []string) Option {
	return func(mux *http.ServeMux) {
		g := guard{hosts: map[string]bool{}}
		for _, h := range hosts {
			if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
				g.hosts[h] = true
			}
		}

		mux.HandleFunc("GET /api/control", func(w http.ResponseWriter, r *http.Request) {
			if err := g.checkHost(r); err != nil {
				writeError(w, http.StatusForbidden, err)
				return
			}
			writeJSON(w, http.StatusOK, c.State(r.Context()))
		})

		mux.HandleFunc("POST /api/control", func(w http.ResponseWriter, r *http.Request) {
			if status, err := g.checkPost(r); err != nil {
				writeError(w, status, err)
				return
			}
			var req struct {
				Action string `json:"action"`
			}
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, errors.New("expected {\"action\": \"start\" | \"stop\" | \"restart\"}"))
				return
			}
			s, err := c.Do(r.Context(), req.Action)
			switch {
			case err == nil:
				writeJSON(w, http.StatusAccepted, s)
			case errors.Is(err, control.ErrUnknownAction):
				writeError(w, http.StatusBadRequest, err)
			case errors.Is(err, control.ErrNotAllowedNow):
				writeError(w, http.StatusConflict, err)
			case errors.Is(err, control.ErrNotPermitted):
				writeError(w, http.StatusForbidden, err)
			default:
				writeError(w, http.StatusBadGateway, err)
			}
		})
	}
}

// guard protects the control endpoints against requests from web pages in a
// browser on the LAN: cross-site requests and DNS rebinding (decision 0001).
type guard struct {
	hosts map[string]bool
}

// checkHost accepts IP addresses, localhost and configured host names. A web
// page that rebinds its own domain to the dashboard's IP sends its own
// domain as Host, so it is rejected.
func (g guard) checkHost(r *http.Request) error {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.ToLower(strings.Trim(host, "[]"))
	if net.ParseIP(host) != nil || host == "localhost" || g.hosts[host] {
		return nil
	}
	return errors.New("node control is only available via the server's IP address, localhost, or a host name in NODEDASH_HOSTS (got " + host + ")")
}

// checkPost requires JSON, which browsers only send cross-site after a CORS
// preflight that the dashboard doesn't answer, and an Origin (when present)
// that matches the dashboard itself.
func (g guard) checkPost(r *http.Request) (int, error) {
	if err := g.checkHost(r); err != nil {
		return http.StatusForbidden, err
	}
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		return http.StatusUnsupportedMediaType, errors.New("Content-Type must be application/json")
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host != r.Host || (u.Scheme != "http" && u.Scheme != "https") {
			return http.StatusForbidden, errors.New("request from another site refused")
		}
	}
	return 0, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
