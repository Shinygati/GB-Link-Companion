// Settings, per-game state, the .sav file next to the ROM, and backups.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

func configDir() string {
	if d := os.Getenv("GBLINK_SYNC_HOME"); d != "" { // for tests
		os.MkdirAll(d, 0o755)
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	d := filepath.Join(base, "GBLinkSaveSync")
	os.MkdirAll(d, 0o755)
	return d
}

// ------------------------------------------------------------------------------- settings
type Settings struct {
	MGBA        string   `json:"mgba"`
	AskEachSave bool     `json:"askEachSave"`
	Controller  bool     `json:"controller"`
	GamesFolder string   `json:"gamesFolder"`
	SetupDone   bool     `json:"setupDone"`
	Recent      []string `json:"recent"`
	StatusMode  string   `json:"statusMode"` // what the GBA showed last time (status.go)
	// Pokémon Emerald (the original game) always starts its RNG at seed 0; mGBA's script gives it
	// a random one when it starts, unless this is set (some RNG manipulation relies on seed 0)
	KeepEmeraldSeed bool `json:"keepEmeraldSeed"`
}

func loadSettings() Settings {
	s := Settings{Controller: true}
	if b, err := os.ReadFile(filepath.Join(configDir(), "settings.json")); err == nil {
		json.Unmarshal(b, &s)
	}
	return s
}

func (s Settings) save() {
	b, _ := json.MarshalIndent(s, "", " ")
	os.WriteFile(filepath.Join(configDir(), "settings.json"), b, 0o644)
}

func (s *Settings) addRecent(rom string) {
	out := []string{rom}
	for _, r := range s.Recent {
		if !strings.EqualFold(r, rom) && len(out) < 6 {
			out = append(out, r)
		}
	}
	s.Recent = out
}

// DefaultMGBA looks for mGBA where it's usually installed.
func DefaultMGBA() string {
	var cands []string
	if runtime.GOOS == "windows" {
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"} {
			if b := os.Getenv(env); b != "" {
				cands = append(cands, filepath.Join(b, "mGBA", "mGBA.exe"), filepath.Join(b, "Programs", "mGBA", "mGBA.exe"))
			}
		}
		if exe, err := os.Executable(); err == nil { // mGBA's folder next to this app
			d := filepath.Dir(exe)
			cands = append(cands, filepath.Join(d, "mGBA.exe"), filepath.Join(d, "mGBA", "mGBA.exe"))
		}
	} else if runtime.GOOS == "darwin" {
		cands = append(cands, "/Applications/mGBA.app/Contents/MacOS/mGBA")
	} else {
		cands = append(cands, "/usr/bin/mgba-qt", "/usr/local/bin/mgba-qt", "/usr/games/mgba-qt")
	}
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

// ------------------------------------------------------------------------------ the game
type Game struct {
	ROM, Title, Code   string
	Sav, State, Backup string
}

func NewGame(rom string) (*Game, error) {
	rom, _ = filepath.Abs(rom)
	f, err := os.Open(rom)
	if err != nil {
		return nil, fmt.Errorf("Couldn't open %s", filepath.Base(rom))
	}
	defer f.Close()
	h := make([]byte, 0xC0)
	if n, _ := f.Read(h); n < 0xC0 {
		return nil, fmt.Errorf("%s doesn't look like a GBA ROM", filepath.Base(rom))
	}
	base := strings.TrimSuffix(rom, filepath.Ext(rom))
	g := &Game{ROM: rom, Title: printable(h[0xA0:0xAC]), Code: printable(h[0xAC:0xB0]),
		Sav: base + ".sav", State: base + ".gblink.json",
		Backup: filepath.Join(filepath.Dir(rom), "gblink_backups", filepath.Base(base))}
	return g, nil
}

// Name is what the player sees: the file name, which is usually nicer than the header title.
func (g *Game) Name() string {
	return strings.TrimSuffix(filepath.Base(g.ROM), filepath.Ext(g.ROM))
}

// GameState is kept next to the ROM (same file the Python launcher used).
type GameState struct {
	Pending    bool   `json:"pending"`
	SyncedHash string `json:"synced_hash"`
	SaveType   int    `json:"save_type"`
	MGBA       string `json:"mgba,omitempty"`
	// the GBA's encounter counter for this game (status.go)
	EncCount    int  `json:"encounters,omitempty"`
	EncTracking bool `json:"encountersOn,omitempty"`
	// the Wonder Cards last chosen for this game (wondercards.go), by name
	WCCards []string `json:"wonderCards,omitempty"`
}

func (g *Game) loadState() GameState {
	var s GameState
	if b, err := os.ReadFile(g.State); err == nil {
		json.Unmarshal(b, &s)
	}
	return s
}

func (g *Game) saveState(s GameState) {
	b, _ := json.MarshalIndent(s, "", " ")
	os.WriteFile(g.State, b, 0o644)
}

func (g *Game) localSave() []byte {
	b, err := os.ReadFile(g.Sav)
	if err != nil {
		return nil
	}
	return b
}

func (g *Game) backup(data []byte, tag string) {
	os.MkdirAll(g.Backup, 0o755)
	name := time.Now().Format("20060102-150405") + "-" + tag + ".sav"
	os.WriteFile(filepath.Join(g.Backup, name), data, 0o644)
	entries, _ := os.ReadDir(g.Backup)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for len(names) > 30 { // keep the newest 30
		os.Remove(filepath.Join(g.Backup, names[0]))
		names = names[1:]
	}
}

// writeLocal puts the cart's save into mGBA's .sav, keeping any extra data mGBA adds after the
// save (e.g. clock data), and backing up a different PC save first.
func (g *Game) writeLocal(save []byte) error {
	old := g.localSave()
	if old != nil && !bytesPrefixEqual(old, save) {
		g.backup(old, "pc-before-load")
	}
	data := append([]byte(nil), save...)
	if len(old) > len(save) && len(old)-len(save) <= 0x200 {
		data = append(data, old[len(save):]...)
	}
	tmp := g.Sav + ".tmp"
	var last error
	for attempt := 0; attempt < 12; attempt++ {
		if _, err := os.Stat(g.Sav); err == nil {
			os.Chmod(g.Sav, 0o644) // clear a read-only flag
		}
		if err := os.WriteFile(tmp, data, 0o644); err == nil {
			if err = os.Rename(tmp, g.Sav); err == nil {
				return nil
			}
			last = err
		} else {
			last = err
		}
		// Windows won't let us swap the file in (open elsewhere, or being scanned): write into it
		if f, err := os.OpenFile(g.Sav, os.O_RDWR|os.O_CREATE, 0o644); err == nil {
			_, werr := f.WriteAt(data, 0)
			if werr == nil {
				werr = f.Truncate(int64(len(data)))
			}
			f.Close()
			if werr == nil {
				os.Remove(tmp)
				return nil
			}
			last = werr
		} else {
			last = err
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("Can't write %s (%v). Close mGBA or anything else using that save, make sure the file "+
		"isn't read-only, and pause OneDrive/cloud sync for that folder if it's synced", filepath.Base(g.Sav), last)
}

func bytesPrefixEqual(a, b []byte) bool {
	if len(a) < len(b) {
		return false
	}
	for i := range b {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
