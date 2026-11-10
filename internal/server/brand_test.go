package server

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

// The real index.html, so a change to it can't silently break the name.
func sourceIndex(t *testing.T) fstest.MapFS {
	t.Helper()
	b, err := os.ReadFile("../../web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	return fstest.MapFS{"index.html": {Data: b}}
}

func TestPageCarriesName(t *testing.T) {
	h := New(fixedSnapshot{}, sourceIndex(t), WithName(`Satoshi's <node> & "co"`))
	rec := get(h, http.MethodGet, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	esc := "Satoshi&#39;s &lt;node&gt; &amp; &#34;co&#34;"
	for _, want := range []string{
		"<title>" + esc + "</title>",
		`name="application-name" content="` + esc + `"`,
		`name="apple-mobile-web-app-title" content="` + esc + `"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %s", want)
		}
	}
	if strings.Contains(body, "<node>") {
		t.Error("the name must be escaped")
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
}

func TestPageDefaultName(t *testing.T) {
	static := sourceIndex(t)
	h := New(fixedSnapshot{}, static)
	if got := get(h, http.MethodGet, "/").Body.String(); got != string(static["index.html"].Data) {
		t.Errorf("without a name the page must be served as built")
	}
}

func TestManifest(t *testing.T) {
	h := New(fixedSnapshot{}, fstest.MapFS{}, WithName("Satoshi’s node"))
	rec := get(h, http.MethodGet, "/manifest.webmanifest")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/manifest+json" {
		t.Fatalf("got %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	var m manifest
	if err := json.NewDecoder(rec.Body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	if m.Name != "Satoshi’s node" || m.ShortName != m.Name || m.StartURL != "/" || m.Display != "standalone" {
		t.Errorf("manifest = %+v", m)
	}
	if len(m.Icons) == 0 {
		t.Error("manifest has no icons")
	}
	for _, ic := range m.Icons {
		if _, err := os.Stat("../../web/public" + ic.Src); err != nil {
			t.Errorf("icon %s: %v", ic.Src, err)
		}
	}
}
