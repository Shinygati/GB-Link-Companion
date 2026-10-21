// Checking backups against No-Intro's list of known good GBA dumps
// (https://datomatic.no-intro.org, system "Nintendo - Game Boy Advance").
//
// A copy of the list is built in. A newer one can be loaded from a DAT file downloaded from
// Datomatic (Logiqx XML, or a .zip with it inside) or a clrmamepro DAT; it's kept in the settings
// folder and used from then on.
package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

//go:embed assets/nointro-gba.tsv.gz
var builtinDAT []byte

type DATEntry struct {
	Name   string
	SHA1   string // lower-case hex
	CRC    string
	Size   int
	Serial string // game code, e.g. AXVE (if the DAT has it)
}

type DAT struct {
	Version string
	Source  string // "built in" or the file it came from
	Entries []DATEntry
	bySHA1  map[string]*DATEntry
}

func (d *DAT) index() {
	d.bySHA1 = make(map[string]*DATEntry, len(d.Entries))
	for i := range d.Entries {
		d.bySHA1[d.Entries[i].SHA1] = &d.Entries[i]
	}
}

func (d *DAT) Lookup(sha1 string) *DATEntry { return d.bySHA1[strings.ToLower(sha1)] }

// WithSerial lists the entries for a game code (needs a DAT with serials).
func (d *DAT) WithSerial(code string) []DATEntry {
	var out []DATEntry
	for _, e := range d.Entries {
		if code != "" && strings.EqualFold(e.Serial, code) {
			out = append(out, e)
		}
	}
	return out
}

var (
	datMu  sync.Mutex
	datCur *DAT
)

func userDATPath() string { return filepath.Join(configDir(), "nointro-gba.dat") }

// currentDAT is the database in use: a loaded DAT file if there is one, else the built-in copy.
func currentDAT() *DAT {
	datMu.Lock()
	defer datMu.Unlock()
	if datCur != nil {
		return datCur
	}
	if b, err := os.ReadFile(userDATPath()); err == nil {
		if d, err := parseDAT(b); err == nil && len(d.Entries) > 0 {
			d.Source = "loaded from a DAT file"
			datCur = d
			return d
		}
	}
	d := &DAT{Source: "built in"}
	if zr, err := gzip.NewReader(bytes.NewReader(builtinDAT)); err == nil {
		sc := bufio.NewScanner(zr)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "#") {
				if i := strings.Index(line, "version "); i >= 0 {
					d.Version = strings.Fields(line[i+8:])[0]
				}
				continue
			}
			f := strings.SplitN(line, "\t", 5)
			if len(f) == 5 {
				n, _ := strconv.Atoi(f[2])
				d.Entries = append(d.Entries, DATEntry{SHA1: f[0], CRC: f[1], Size: n, Serial: f[3], Name: f[4]})
			}
		}
	}
	d.index()
	datCur = d
	return d
}

// loadDATFile checks a DAT (or a .zip with one inside) and makes it the database in use.
func loadDATFile(path string) (*DAT, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b))); err == nil { // Datomatic's download
		b = nil
		for _, f := range zr.File {
			if strings.EqualFold(filepath.Ext(f.Name), ".dat") || strings.EqualFold(filepath.Ext(f.Name), ".xml") {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				b, err = io.ReadAll(rc)
				rc.Close()
				if err != nil {
					return nil, err
				}
				break
			}
		}
		if b == nil {
			return nil, fmt.Errorf("there's no .dat file in that zip")
		}
	}
	d, err := parseDAT(b)
	if err != nil {
		return nil, err
	}
	if len(d.Entries) == 0 {
		return nil, fmt.Errorf("that file has no GBA games in it")
	}
	if err := os.WriteFile(userDATPath(), b, 0o644); err != nil {
		return nil, err
	}
	d.Source = "loaded from a DAT file"
	datMu.Lock()
	datCur = d
	datMu.Unlock()
	return d, nil
}

// parseDAT reads Logiqx XML (what Datomatic gives) or clrmamepro format.
func parseDAT(b []byte) (*DAT, error) {
	t := bytes.TrimSpace(b)
	if bytes.HasPrefix(t, []byte("<")) {
		var x struct {
			Header struct {
				Name    string `xml:"name"`
				Version string `xml:"version"`
			} `xml:"header"`
			Games []struct {
				Name string `xml:"name,attr"`
				Roms []struct {
					Name   string `xml:"name,attr"`
					Size   int    `xml:"size,attr"`
					CRC    string `xml:"crc,attr"`
					SHA1   string `xml:"sha1,attr"`
					Serial string `xml:"serial,attr"`
				} `xml:"rom"`
			} `xml:"game"`
		}
		if err := xml.Unmarshal(b, &x); err != nil {
			return nil, fmt.Errorf("couldn't read the DAT file (%v)", err)
		}
		if x.Header.Name != "" && !strings.Contains(strings.ToLower(x.Header.Name), "advance") {
			return nil, fmt.Errorf("that DAT is for %q, not the Game Boy Advance", x.Header.Name)
		}
		d := &DAT{Version: x.Header.Version}
		for _, g := range x.Games {
			for _, r := range g.Roms {
				if r.SHA1 != "" {
					d.Entries = append(d.Entries, DATEntry{Name: g.Name, SHA1: strings.ToLower(r.SHA1), CRC: strings.ToLower(r.CRC), Size: r.Size, Serial: r.Serial})
				}
			}
		}
		d.index()
		return d, nil
	}
	// clrmamepro
	s := string(b)
	d := &DAT{}
	if m := regexp.MustCompile(`version "([^"]+)"`).FindStringSubmatch(s); m != nil {
		d.Version = m[1]
	}
	game := regexp.MustCompile(`(?s)game \(\s*name "((?:[^"\\]|\\.)*)"(.*?)\n\)`)
	rom := regexp.MustCompile(`rom \( (.*?) \)`)
	field := func(a, k string) string {
		m := regexp.MustCompile(`\b` + k + ` "?([^" ]+)"?`).FindStringSubmatch(a)
		if m == nil {
			return ""
		}
		return m[1]
	}
	for _, g := range game.FindAllStringSubmatch(s, -1) {
		for _, r := range rom.FindAllStringSubmatch(g[2], -1) {
			n, _ := strconv.Atoi(field(r[1], "size"))
			if sh := field(r[1], "sha1"); sh != "" {
				d.Entries = append(d.Entries, DATEntry{Name: g[1], SHA1: strings.ToLower(sh), CRC: strings.ToLower(field(r[1], "crc")),
					Size: n, Serial: field(r[1], "serial")})
			}
		}
	}
	if len(d.Entries) == 0 {
		return nil, fmt.Errorf("that doesn't look like a DAT file")
	}
	d.index()
	return d, nil
}
