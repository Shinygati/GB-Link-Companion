// Settings, per-game state, the .sav file next to the ROM, and backups.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

func configDir() string {
	if d := os.Getenv("GBLINK_COMPANION_HOME"); d != "" { // for tests
		os.MkdirAll(d, 0o755)
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	d := filepath.Join(base, "GBLinkCompanion")
	if _, err := os.Stat(d); os.IsNotExist(err) { // settings from before the rename: move them over
		os.Rename(filepath.Join(base, "GBLinkSaveSync"), d)
	}
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
	// Pokémon Ruby, Sapphire and Emerald: mGBA's clock follows the PC, not the cartridge's clock
	// (on by default: mGBA starts from the cartridge's clock, see cartclock.go)
	PCClock bool `json:"pcClock"`
	// Pokémon Emerald, FireRed and LeafGreen: hide the Wonder Cards card on the play screen
	WonderCardsOff bool `json:"wonderCardsOff"`

	// Discord alerts for shiny wild Pokémon (discord.go). A bot token + channel ID, or a webhook URL.
	DiscordOn      bool   `json:"discordOn"`
	DiscordToken   string `json:"discordToken,omitempty"`
	DiscordChannel string `json:"discordChannel,omitempty"`
	DiscordWebhook string `json:"discordWebhook,omitempty"`
	DiscordMessage string `json:"discordMessage,omitempty"` // the text posted; {pokemon} {level} {game} {encounters}
	DiscordShots   bool   `json:"discordShots"`             // attach the encounter and the wild Pokémon's info screen
	DiscordDelay   int    `json:"discordDelay,omitempty"`   // seconds of game time to wait for the Pokémon to be fully on screen
}

func loadSettings() Settings {
	s := Settings{Controller: true, DiscordShots: true}
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
		// the distro's package, then an mGBA AppImage in the usual places, then the Flatpak
		if p, err := exec.LookPath("mgba-qt"); err == nil {
			cands = append(cands, p)
		}
		cands = append(cands, "/usr/bin/mgba-qt", "/usr/local/bin/mgba-qt", "/usr/games/mgba-qt")
		cands = append(cands, mgbaAppImages()...)
	}
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	if runtime.GOOS == "linux" && flatpakInstalled(mgbaFlatpak) {
		return "flatpak:" + mgbaFlatpak
	}
	return ""
}

// mgbaFlatpak is mGBA's Flatpak (Flathub). The mGBA setting is then "flatpak:" + this ID.
const mgbaFlatpak = "io.mgba.mGBA"

// mgbaExists says whether the mGBA setting still points at an mGBA.
func mgbaExists(mgba string) bool {
	if id, ok := strings.CutPrefix(mgba, "flatpak:"); ok {
		return flatpakInstalled(id)
	}
	st, err := os.Stat(mgba)
	return err == nil && !st.IsDir()
}

func flatpakInstalled(id string) bool {
	dirs := []string{"/var/lib/flatpak/app"}
	if h, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(h, ".local", "share", "flatpak", "app"))
	}
	for _, d := range dirs {
		if st, err := os.Stat(filepath.Join(d, id)); err == nil && st.IsDir() {
			return true
		}
	}
	return false
}

// mgbaAppImages lists mGBA AppImages (mGBA-0.10.5-appimage-x64.appimage and the like) next to this
// app and in the folders people keep AppImages in, newest name first.
func mgbaAppImages() []string {
	var dirs []string
	if a := os.Getenv("APPIMAGE"); a != "" { // this app is an AppImage: look next to it
		dirs = append(dirs, filepath.Dir(a))
	}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	if h, err := os.UserHomeDir(); err == nil {
		for _, d := range []string{"Applications", "AppImages", ".local/bin", "bin", "Downloads", "Desktop", ""} {
			dirs = append(dirs, filepath.Join(h, d))
		}
	}
	dirs = append(dirs, "/opt", "/opt/mgba")
	var out []string
	for _, d := range dirs {
		ents, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		var found []string
		for _, e := range ents {
			n := strings.ToLower(e.Name())
			if strings.HasPrefix(n, "mgba") && strings.HasSuffix(n, ".appimage") {
				found = append(found, filepath.Join(d, e.Name()))
			}
		}
		sort.Sort(sort.Reverse(sort.StringSlice(found)))
		out = append(out, found...)
	}
	return out
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

// backup keeps a copy of a save in the ROM's folder (gblink_backups/<game>/), named by when and
// why: "cart" (read from the cartridge), "mgba" (saved in mGBA), "pc-before-load". The newest 30
// of each kind are kept (100 of the saves made in mGBA).
func (g *Game) backup(data []byte, tag string) {
	os.MkdirAll(g.Backup, 0o755)
	name := time.Now().Format("20060102-150405") + "-" + tag + ".sav"
	os.WriteFile(filepath.Join(g.Backup, name), data, 0o644)
	entries, _ := os.ReadDir(g.Backup)
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "-"+tag+".sav") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	keep := 30
	if tag == "mgba" {
		keep = 100
	}
	for len(names) > keep {
		os.Remove(filepath.Join(g.Backup, names[0]))
		names = names[1:]
	}
}

// writeLocal puts the cart's save into mGBA's .sav, backing up a different PC save first. Only the
// save: clock data mGBA added after it goes (cartclock.go puts the cartridge's clock there when
// the game has one).
func (g *Game) writeLocal(save []byte) error {
	old := g.localSave()
	if old != nil && !bytesPrefixEqual(old, save) {
		g.backup(old, "pc-before-load")
	}
	data := append([]byte(nil), save...)
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
