package settings

import (
	"fmt"
	"strconv"
	"strings"
)

// Marker is the comment above the settings the form adds to bitcoin.conf.
const Marker = "# Set by node-dash"

// confLine is one line of bitcoin.conf, as it is in the file.
type confLine struct {
	text string // without the line ending
	eol  string // "\n", "\r\n", or "" for a last line without one
}

// parsed is what a line sets, for the editor.
type parsed struct {
	section bool   // a [section] header
	target  string // the section or chain prefix it applies to; "" at the top
	prefix  string // "main." in "main.prune=550"
	key     string // without "no"
	lead    string // whitespace before the key
	comment string // " # …" after the value
}

func splitLines(content []byte) []confLine {
	var lines []confLine
	s := string(content)
	for s != "" {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			lines = append(lines, confLine{text: s})
			break
		}
		text, eol := s[:i], "\n"
		if strings.HasSuffix(text, "\r") {
			text, eol = text[:len(text)-1], "\r\n"
		}
		lines = append(lines, confLine{text: text, eol: eol})
		s = s[i+1:]
	}
	return lines
}

// parseLine reads a line like parseFile does; current is the section the
// line is in.
func parseLine(text, current string) parsed {
	body, comment, hasComment := strings.Cut(text, "#")
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return parsed{target: current}
	}
	if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
		return parsed{section: true, target: strings.TrimSpace(trimmed[1 : len(trimmed)-1])}
	}
	key, _, ok := strings.Cut(trimmed, "=")
	if !ok {
		return parsed{target: current}
	}
	p := parsed{target: current, lead: body[:len(body)-len(strings.TrimLeft(body, " \t"))]}
	if hasComment {
		p.comment = " #" + comment
	}
	key = strings.TrimSpace(key)
	if net, k, ok := strings.Cut(key, "."); ok {
		p.target, p.prefix, key = net, net+".", k
	}
	if name, ok := strings.CutPrefix(key, "no"); ok && known(name) {
		key = name
	}
	p.key = key
	return p
}

// Edit returns bitcoin.conf with the settings in changes set for chain.
// Every other byte stays as it is: other settings, comments, sections, line
// endings, and whether the file ends with a newline.
//
//   - A setting that is set is changed where it takes effect: in the
//     chain's section if it is set there, else its first line at the top.
//     A trailing comment stays, and "noprune=1" becomes "prune=…".
//   - A setting that isn't set is added at the top, before the first
//     section (else it would land in that section), under Marker.
func Edit(content []byte, chain string, changes map[string]int64) ([]byte, error) {
	for key := range changes {
		if specFor(key) == nil {
			return nil, fmt.Errorf("%s isn't a setting the form can change", key)
		}
	}
	lines := splitLines(content)
	eol := "\n"
	if strings.Contains(string(content), "\r\n") {
		eol = "\r\n"
	}

	// Where each key takes effect, and where the top level ends.
	inSection, atTop := map[string]int{}, map[string]int{}
	firstSection, marker := len(lines), -1
	current := ""
	for i, l := range lines {
		p := parseLine(l.text, current)
		if p.section {
			current = p.target
			if firstSection == len(lines) {
				firstSection = i
			}
			continue
		}
		if current == "" && strings.TrimSpace(l.text) == Marker {
			marker = i
		}
		if p.key == "" {
			continue
		}
		if _, ok := changes[p.key]; !ok {
			continue
		}
		switch p.target {
		case chain:
			if _, seen := inSection[p.key]; !seen {
				inSection[p.key] = i
			}
		case "":
			if _, seen := atTop[p.key]; !seen {
				atTop[p.key] = i
			}
		}
	}

	var add []string
	for _, key := range Keys { // in the order of the form
		v, ok := changes[key]
		if !ok {
			continue
		}
		i, ok := inSection[key]
		if !ok {
			i, ok = atTop[key]
		}
		if !ok {
			add = append(add, key+"="+strconv.FormatInt(v, 10))
			continue
		}
		p := parseLine(lines[i].text, "")
		lines[i].text = p.lead + p.prefix + key + "=" + strconv.FormatInt(v, 10) + p.comment
	}

	if len(add) > 0 {
		var block []confLine
		at := firstSection
		if marker >= 0 {
			// Under the marker, after the settings already there.
			at = marker + 1
			for at < len(lines) && at < firstSection {
				t := strings.TrimSpace(lines[at].text)
				if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "[") {
					break
				}
				at++
			}
		} else {
			if at > 0 && strings.TrimSpace(lines[at-1].text) != "" {
				block = append(block, confLine{eol: eol})
			}
			block = append(block, confLine{text: Marker, eol: eol})
		}
		for _, a := range add {
			block = append(block, confLine{text: a, eol: eol})
		}
		if marker < 0 && at < len(lines) {
			block = append(block, confLine{eol: eol}) // a blank line before the section
		}
		if at > 0 && lines[at-1].eol == "" {
			lines[at-1].eol = eol
		}
		lines = append(lines[:at], append(block, lines[at:]...)...)
	}

	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.text)
		b.WriteString(l.eol)
	}
	return []byte(b.String()), nil
}

func specFor(key string) *spec {
	for i := range specs {
		if specs[i].key == key {
			return &specs[i]
		}
	}
	return nil
}
