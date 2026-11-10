package server

import (
	"bytes"
	"html"
)

// Brand (story #35): the operator's name for their node in the page and the
// web app manifest, so "Add to Home Screen" gives a proper icon and name.

// withName puts name in the built index.html: its title and the
// application-name meta tags, which the frontend reads. The name is escaped,
// so it is only ever text.
func withName(index []byte, name string) []byte {
	if name == DefaultName {
		return index
	}
	esc := []byte(html.EscapeString(name))
	out := bytes.Replace(index, []byte("<title>"+DefaultName+"</title>"), append(append([]byte("<title>"), esc...), "</title>"...), 1)
	for _, meta := range []string{"application-name", "apple-mobile-web-app-title"} {
		old := []byte(`name="` + meta + `" content="` + DefaultName + `"`)
		out = bytes.Replace(out, old, append(append([]byte(`name="`+meta+`" content="`), esc...), '"'), 1)
	}
	return out
}

type manifest struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	ShortName       string `json:"short_name"`
	StartURL        string `json:"start_url"`
	Scope           string `json:"scope"`
	Display         string `json:"display"`
	BackgroundColor string `json:"background_color"`
	ThemeColor      string `json:"theme_color"`
	Icons           []icon `json:"icons"`
}

type icon struct {
	Src     string `json:"src"`
	Sizes   string `json:"sizes"`
	Type    string `json:"type"`
	Purpose string `json:"purpose"`
}

// newManifest describes the dashboard as a web app. The icons are drawn by
// tools/icons; the green halo on ink leaves room for maskable cropping.
func newManifest(name string) manifest {
	return manifest{
		ID:              "/",
		Name:            name,
		ShortName:       name,
		StartURL:        "/",
		Scope:           "/",
		Display:         "standalone",
		BackgroundColor: "#F2F3F5",
		ThemeColor:      "#F2F3F5",
		Icons: []icon{
			{Src: "/icons/icon-192.png", Sizes: "192x192", Type: "image/png", Purpose: "any"},
			{Src: "/icons/icon-512.png", Sizes: "512x512", Type: "image/png", Purpose: "any"},
			{Src: "/icons/icon-512.png", Sizes: "512x512", Type: "image/png", Purpose: "maskable"},
		},
	}
}
