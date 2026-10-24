// What the window shows (State), the answers to prompts, and the app around a play session.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type Button struct {
	ID    string
	Label string
	Style string // primary | secondary
}

type Prompt struct {
	ID      int
	Kind    string // power | question | error
	Title   string
	Text    string
	Buttons []Button
}

type Progress struct {
	Label       string
	Done, Total int
	Detail      string // e.g. "3.2 of 16 MB · about 6 minutes left"
	Cancellable bool
}

type ControllerView struct {
	Enabled, Online, ScriptConnected, Manual bool
	ScriptPath                               string
	GBAShows                                 string // "" = controller mode, else which status screen
	Passthru                                 bool   // ...while its buttons control mGBA (hold SELECT)
	LastShot                                 string // the last screenshot saved ("Save screenshot")
}

type GameView struct {
	ROM, Name, Title, Code string
	HasSave, Pending       bool
	Played                 string
	Verify                 string // No-Intro check: ok | bad | unknown | "" (not checked yet)
	NoIntro                string // the No-Intro name, for ok
}

// Verify: a backup checked against No-Intro's list of good dumps.
type Verify struct {
	Status string // ok | bad | unknown
	Title  string
	Text   string
	SHA1   string
	CRC    string
}

type State struct {
	Phase      string  // home | connect | load | play | finish | done   (backup: connect | load | dump | done)
	Mode       string  // play | backup
	BackupPath string  // the file a backup was saved to
	Verify     *Verify // the No-Intro check of a backup
	DATInfo    string  // which No-Intro database is in use
	Game       *GameView
	Status     string
	Progress   *Progress
	Prompt     *Prompt
	Cart       string // synced | pending | ""
	LastSync   string
	GBA        string // on | off | ""
	Controller ControllerView
	Writing    bool
	Result     string // done | pending | cancelled
	ResultText string
	Log        []string
	Adapter    string
	Settings   Settings
	MGBAFound  bool
	Library    []GameView
	LibError   string
	Toast      string
	ToastUntil time.Time
	WCGame     string      // the game's Wonder Card folder (wondercards.go), "" if it can't take them
	GBAWC      string      // Wonder Cards on a real GBA (wcgba.go): starting, ready, busy, away
	WCWhy      string      // ...and why not
	WCStatus   string      // what happened to the last card sent
	WCChosen   []string    // the cards ticked in the list (remembered for the game)
	WCReplace  *WonderCard // a card waiting for "Replace" (another one that keeps running is on)
	Update     UpdateView  // a newer release on GitHub (version.go)
}

// UI holds the state the window shows, and the answers to prompts.
type UI struct {
	mu      sync.Mutex
	st      State
	changed *sync.Cond
	nextID  int
	answers map[int]string
	app     *App
}

func newUI(app *App) *UI {
	u := &UI{answers: map[int]string{}, app: app}
	u.changed = sync.NewCond(&u.mu)
	u.st.Phase = "home"
	return u
}

// Snapshot returns a copy of the state for drawing.
func (u *UI) Snapshot() State {
	u.mu.Lock()
	defer u.mu.Unlock()
	s := u.st
	s.Log = append([]string(nil), u.st.Log...)
	return s
}

func (u *UI) update(f func(*State)) {
	u.mu.Lock()
	f(&u.st)
	u.changed.Broadcast()
	u.mu.Unlock()
	u.app.redraw()
}

func (u *UI) Log(f string, a ...any) {
	line := time.Now().Format("15:04:05") + "  " + fmt.Sprintf(f, a...)
	fmt.Println(line)
	u.update(func(s *State) {
		s.Log = append(s.Log, line)
		if len(s.Log) > 200 {
			s.Log = s.Log[len(s.Log)-200:]
		}
	})
}

func (u *UI) Toast(t string) {
	u.update(func(s *State) { s.Toast, s.ToastUntil = t, time.Now().Add(2600*time.Millisecond) })
}

func (u *UI) SetPhase(p string)                     { u.update(func(s *State) { s.Phase = p }) }
func (u *UI) SetStatus(t, tone string)              { u.update(func(s *State) { s.Status = t }) }
func (u *UI) SetGBA(g string)                       { u.update(func(s *State) { s.GBA = g }) }
func (u *UI) SetWriting(w bool)                     { u.update(func(s *State) { s.Writing = w }) }
func (u *UI) ClearProgress()                        { u.update(func(s *State) { s.Progress = nil }) }
func (u *UI) SetController(f func(*ControllerView)) { u.update(func(s *State) { f(&s.Controller) }) }
func (u *UI) SetProgress(label string, d, t int) {
	u.update(func(s *State) { s.Progress = &Progress{Label: label, Done: d, Total: t} })
}
func (u *UI) SetCart(c, text string) {
	u.update(func(s *State) {
		s.Cart = c
		if c == "synced" {
			s.LastSync = time.Now().Format("3:04 pm")
		}
	})
}
func (u *UI) Finish(result, text string) {
	u.update(func(s *State) { s.Phase, s.Result, s.ResultText, s.Prompt, s.Progress = "done", result, text, nil, nil })
	u.app.sessionEnded()
}

// ShowPrompt shows a prompt without waiting; see Answer / ClearPrompt.
func (u *UI) ShowPrompt(p Prompt) int {
	u.mu.Lock()
	u.nextID++
	p.ID = u.nextID
	u.st.Prompt = &p
	u.changed.Broadcast()
	u.mu.Unlock()
	u.Raise()
	return p.ID
}

func (u *UI) Answer(id int) (string, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	a, ok := u.answers[id]
	return a, ok
}

// Press is called by the window when a prompt button is pressed.
func (u *UI) Press(id int, button string) {
	u.mu.Lock()
	if _, done := u.answers[id]; !done {
		u.answers[id] = button
	}
	u.changed.Broadcast()
	u.mu.Unlock()
	u.app.redraw()
}

func (u *UI) ClearPrompt(id int) {
	u.update(func(s *State) {
		if s.Prompt != nil && s.Prompt.ID == id {
			s.Prompt = nil
		}
	})
}

// Ask shows a prompt and waits for a button.
func (u *UI) Ask(p Prompt) string {
	id := u.ShowPrompt(p)
	u.mu.Lock()
	for {
		if a, ok := u.answers[id]; ok {
			if u.st.Prompt != nil && u.st.Prompt.ID == id {
				u.st.Prompt = nil
			}
			u.mu.Unlock()
			u.app.redraw()
			return a
		}
		u.changed.Wait()
	}
}

// Raise brings the window to the front (opens it again if it was closed).
func (u *UI) Raise() { u.app.raise() }

// CancelPrompt answers the open question with its way out (Cancel / Stop / the secondary
// button), as if the player had chosen it. For when the window is closed under it.
func (u *UI) CancelPrompt() {
	u.mu.Lock()
	p := u.st.Prompt
	if p != nil && len(p.Buttons) > 0 {
		pick := p.Buttons[len(p.Buttons)-1].ID
		for _, b := range p.Buttons {
			if b.Style == "secondary" {
				pick = b.ID
			}
		}
		for _, b := range p.Buttons {
			if b.ID == "cancel" || b.ID == "stop" {
				pick = b.ID
			}
		}
		if _, done := u.answers[p.ID]; !done {
			u.answers[p.ID] = pick
		}
		u.st.Prompt = nil
	}
	u.changed.Broadcast()
	u.mu.Unlock()
}

// closeWhileBusy: the window was closed during a session. While the game runs or its save is
// being written, that goes on in the background (the window comes back if it's needed). Before
// the game has started there's nothing to wait for: the session stops, and the app quits. A
// question left open is answered with its way out, so nothing waits for an answer nobody can
// give (that kept the app running, unseen, in Task Manager).
func (a *App) closeWhileBusy() {
	a.mu.Lock()
	s := a.session
	a.mu.Unlock()
	if s == nil {
		os.Exit(0)
	}
	if ph := a.ui.Snapshot().Phase; ph != "play" && ph != "finish" {
		s.stopped.Store(true)
		if s.cancel != nil {
			select {
			case <-s.cancel:
			default:
				close(s.cancel)
			}
		}
	}
	a.ui.CancelPrompt()
}

// ------------------------------------------------------------------------------------ app
type App struct {
	ui      *UI
	mu      sync.Mutex
	set     Settings
	session *Session
	win     *Window // the open window, nil if closed
	winMu   sync.Mutex
}

func (a *App) settings() Settings { a.mu.Lock(); defer a.mu.Unlock(); return a.set }

func (a *App) setSettings(f func(*Settings)) {
	a.mu.Lock()
	f(&a.set)
	a.set.save()
	s := a.set
	a.mu.Unlock()
	a.ui.update(func(st *State) {
		st.Settings = s
		st.MGBAFound = s.MGBA != ""
	})
	a.refreshLibrary()
}

func (a *App) sessionEnded() {
	a.mu.Lock()
	a.session = nil
	a.mu.Unlock()
	go a.refreshLibrary()
	// the window was closed during the game: if all went well just quit, otherwise show why
	a.winMu.Lock()
	closed := a.win == nil
	a.winMu.Unlock()
	if closed {
		if r := a.ui.Snapshot().Result; r == "done" || r == "cancelled" || r == "pending" || r == "gbawc" {
			go func() { time.Sleep(300 * time.Millisecond); os.Exit(0) }()
		} else {
			a.raise()
		}
	}
}

func (a *App) busy() bool { a.mu.Lock(); defer a.mu.Unlock(); return a.session != nil }

func (a *App) addRecent(rom string) {
	a.mu.Lock()
	a.set.addRecent(rom)
	a.set.save()
	s := a.set
	a.mu.Unlock()
	a.ui.update(func(st *State) { st.Settings = s })
	a.refreshLibrary()
}

func gameView(rom string) (GameView, bool) {
	g, err := NewGame(rom)
	if err != nil {
		return GameView{}, false
	}
	v := GameView{ROM: g.ROM, Name: g.Name(), Title: g.Title, Code: g.Code}
	if st, err := os.Stat(g.Sav); err == nil {
		v.HasSave = true
		v.Played = st.ModTime().Format("2 Jan 2006")
	}
	v.Pending = g.loadState().Pending
	if h, ok := hashes.get(g.ROM); ok {
		v.Verify, v.NoIntro = classify(h.SHA1, g.Code)
	}
	return v, true
}

// refreshLibrary lists the recent games first, then every ROM in the games folder.
func (a *App) refreshLibrary() {
	set := a.settings()
	var lib []GameView
	seen := map[string]bool{}
	for _, r := range set.Recent {
		if v, ok := gameView(r); ok && !seen[strings.ToLower(v.ROM)] {
			seen[strings.ToLower(v.ROM)] = true
			lib = append(lib, v)
		}
	}
	libErr := ""
	if dir := set.GamesFolder; dir != "" {
		var roms []string
		filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if p != dir && (strings.EqualFold(d.Name(), "gblink_backups") || strings.Count(strings.TrimPrefix(p, dir), string(os.PathSeparator)) > 3) {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.EqualFold(filepath.Ext(p), ".gba") {
				roms = append(roms, p)
			}
			return nil
		})
		if roms == nil {
			if _, err := os.Stat(dir); err != nil {
				libErr = "The games folder can't be found. Choose it again in Settings."
			}
		}
		sort.Slice(roms, func(i, j int) bool {
			return strings.ToLower(filepath.Base(roms[i])) < strings.ToLower(filepath.Base(roms[j]))
		})
		for _, r := range roms {
			if v, ok := gameView(r); ok && !seen[strings.ToLower(v.ROM)] {
				seen[strings.ToLower(v.ROM)] = true
				lib = append(lib, v)
			}
		}
	}
	var todo []string
	for _, g := range lib {
		if g.Verify == "" {
			todo = append(todo, g.ROM)
		}
	}
	if len(todo) > 0 {
		hashes.enqueue(a, todo)
	}
	dat := currentDAT()
	info := fmt.Sprintf("%d GBA dumps", len(dat.Entries))
	if dat.Version != "" {
		info = "Version " + dat.Version + " · " + info
	}
	info += " · " + dat.Source
	a.ui.update(func(s *State) { s.Library, s.LibError, s.DATInfo = lib, libErr, info })
}

func (a *App) play(rom string) error {
	a.mu.Lock()
	if a.session != nil {
		a.mu.Unlock()
		return fmt.Errorf("A game is already running.")
	}
	if a.set.MGBA == "" {
		a.mu.Unlock()
		return fmt.Errorf("Find mGBA first (in Settings).")
	}
	g, err := NewGame(rom)
	if err != nil {
		a.mu.Unlock()
		return err
	}
	s := &Session{ui: a.ui, app: a, game: g}
	a.session = s
	a.set.addRecent(g.ROM)
	a.set.save()
	set := a.set
	a.mu.Unlock()
	v, _ := gameView(g.ROM)
	a.ui.update(func(st *State) {
		st.Settings = set
		st.Game = &v
		st.Phase, st.Mode, st.Status, st.Progress, st.Prompt = "connect", "play", "", nil, nil
		st.Cart, st.LastSync, st.GBA, st.Result, st.ResultText = "", "", "", "", ""
		st.Controller = ControllerView{}
		st.Log = nil
		st.WCGame, st.WCWhy, st.WCStatus = "", "", ""
		st.WCChosen = g.loadState().WCCards
	})
	go func() { // can it take Wonder Cards? (a verified ROM: hashing it can take a moment)
		key, why := wcCheckROM(g)
		a.ui.update(func(st *State) { st.WCGame, st.WCWhy = key, why })
	}()
	go s.run()
	return nil
}

func (a *App) goHome() {
	if a.busy() {
		return
	}
	a.ui.update(func(s *State) { s.Phase, s.Game, s.Result = "home", nil, "" })
	go a.refreshLibrary()
}

var pickMu sync.Mutex // one file picker at a time

// browse opens a file/folder picker (what: rom | mgba | folder) and applies the choice.
func (a *App) browse(what string) {
	if !pickMu.TryLock() {
		return
	}
	defer pickMu.Unlock()
	var path string
	var err error
	switch what {
	case "folder":
		path, err = pickFolder("Choose the folder with your GBA games")
	case "mgba":
		if runtime.GOOS == "windows" {
			path, err = pickFile("Find mGBA", "mGBA (mGBA.exe)", "*.exe", filepath.Dir(a.settings().MGBA))
		} else {
			path, err = pickFile("Find mGBA (mgba-qt or an mGBA AppImage)", "mGBA", "*", filepath.Dir(a.settings().MGBA))
		}
	default:
		path, err = pickFile("Choose a GBA game", "GBA games (*.gba)", "*.gba", a.settings().GamesFolder)
	}
	if err != nil {
		a.ui.Toast(err.Error())
		return
	}
	if path == "" {
		return
	}
	switch what {
	case "folder":
		a.setSettings(func(s *Settings) { s.GamesFolder = path })
		a.ui.Toast("Games folder set")
	case "mgba":
		a.setSettings(func(s *Settings) { s.MGBA = path })
		a.ui.Toast("mGBA set")
	default:
		v, ok := gameView(path)
		if !ok {
			a.ui.Toast("That doesn't look like a GBA game")
			return
		}
		a.addRecent(v.ROM)
		a.selectGame(v.ROM)
		a.ui.Toast("Added to your games")
	}
}

func (a *App) selectGame(rom string) {
	a.winMu.Lock()
	w := a.win
	a.winMu.Unlock()
	if w != nil {
		w.selectROM(rom)
	}
}
