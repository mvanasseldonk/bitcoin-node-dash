package source

import (
	"bufio"
	"io"
	"os"
	"strings"
)

// confCredentials returns rpcuser and rpcpassword for mainnet from a
// bitcoin.conf file. ok is false when the file can't be read or doesn't set
// both. rpcauth lines only hold a hash, so they can't be used here.
func confCredentials(path string) (user, pass string, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", false
	}
	defer f.Close()
	s := parseConf(f)
	user, pass = s.get("rpcuser"), s.get("rpcpassword")
	return user, pass, user != "" && pass != ""
}

// confSettings holds the mainnet-relevant settings of a bitcoin.conf.
type confSettings struct {
	top, main map[string]string
}

// get returns a setting for mainnet: the [main] section (or a "main."
// prefix) takes precedence over the top of the file, like in Bitcoin Core.
func (s confSettings) get(key string) string {
	if v, ok := s.main[key]; ok {
		return v
	}
	return s.top[key]
}

// parseConf reads a bitcoin.conf the way Bitcoin Core does, as far as it
// matters here: "key=value" lines, "#" comments, [section] headers and
// "network.key" prefixes. Other networks' sections are skipped. Within a
// section the first value of a key wins, like for Bitcoin Core config files.
func parseConf(r io.Reader) confSettings {
	s := confSettings{top: map[string]string{}, main: map[string]string{}}
	section := ""
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line, _, _ := strings.Cut(sc.Text(), "#")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		target := section
		if net, k, ok := strings.Cut(key, "."); ok {
			target, key = net, k
		}
		var m map[string]string
		switch target {
		case "":
			m = s.top
		case "main":
			m = s.main
		default:
			continue // another network
		}
		if _, seen := m[key]; !seen {
			m[key] = value
		}
	}
	return s
}
