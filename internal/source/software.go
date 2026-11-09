package source

import (
	"fmt"
	"strings"

	"github.com/mvanasseldonk/bitcoin-node-dash/internal/model"
)

// ParseSoftware tells which node software runs, from getnetworkinfo's
// version (e.g. 320100 for 32.1.0) and subversion, the user agent it shows
// to peers (e.g. "/Satoshi:32.1.0/" or "/Satoshi:28.1.0/Knots:20250305/").
func ParseSoftware(version int, subversion string) model.Software {
	s := model.Software{
		Major: version / 10000,
		Minor: version / 100 % 100,
		Patch: version % 100,
	}
	s.Version = fmt.Sprintf("%d.%d", s.Major, s.Minor)
	if s.Patch != 0 {
		s.Version += fmt.Sprintf(".%d", s.Patch)
	}

	// The user agent is "/Name:version(comments)/Name:version/…".
	var parts []string
	for _, p := range strings.Split(strings.Trim(subversion, "/"), "/") {
		name, _, _ := strings.Cut(p, "(") // drop uacomment
		if name != "" {
			parts = append(parts, name)
		}
	}
	switch {
	case version > 0 && len(parts) == 1 && strings.HasPrefix(parts[0], "Satoshi:"):
		s.Name = "Bitcoin Core"
	case version > 0 && len(parts) == 2 && strings.HasPrefix(parts[0], "Satoshi:") && strings.HasPrefix(parts[1], "Knots:"):
		s.Name = "Bitcoin Knots"
		s.Version += ".knots" + strings.TrimPrefix(parts[1], "Knots:")
	}
	if s.Name != "" {
		s.Label = s.Name + " " + s.Version
	} else {
		s.Label = strings.Trim(subversion, "/")
	}
	return s
}
