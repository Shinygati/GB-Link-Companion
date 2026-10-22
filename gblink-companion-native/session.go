// One play session: load the cart's save, run mGBA, keep the cart in step, write it back at the end.
package main

import (
	"crypto/sha1"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

//go:embed assets/gblink_companion_mb.gba
var helperImage []byte

const controllerPort = 47187

type Session struct {
	ui         *UI
	app        *App
	game       *Game
	state      GameState
	link       *GBLink
	helper     *Helper
	linkMu     sync.Mutex // one user of the link cable at a time
	cart       []byte     // what we know is on the cart (nil = unknown)
	saveType   int
	bridge     *Bridge
	gbawc      *gbaWC        // Wonder Cards on a real GBA (wcgba.go)
	stopped    atomic.Bool   // the window was closed before the game started: stop (closeWhileBusy)
	cancel     chan struct{} // closed when the player cancels a backup
	helperLg   byte          // the language the helper's text is in now (0: not known)
	clockUsed  bool          // the .sav ends with the cartridge's clock for mGBA (cartclock.go)
	lastBackup string        // hash of the last save backed up from mGBA
}

// helperLang: the language for the GBA's text: the Pokémon game's (lang.go), else English
func (s *Session) helperLang() byte {
	if s.game != nil {
		return gameLang(s.game.Code)
	}
	return 'E'
}

// matchHelperLang tells a running helper the game's language, if it might be in another one.
func (s *Session) matchHelperLang() {
	if lang := s.helperLang(); s.helperLg != lang {
		if s.helper.SetLanguage(lang) == nil {
			s.helperLg = lang
		}
	}
}

func hash(b []byte) string { h := sha1.Sum(b); return hex.EncodeToString(h[:]) }

// errCancelled: the player chose not to go on (not a failure to report).
var errCancelled = errors.New("cancelled")

// ----------------------------------------------------------------------------- connection
func (s *Session) connect() error {
	if s.link != nil {
		return nil
	}
	port := os.Getenv("GBLINK_PORT")
	if port == "" {
		port = FindGBLinkPort()
	}
	if port == "" {
		return linkErr("The GB-Link adapter isn't plugged in.")
	}
	l, err := OpenGBLink(port)
	if err != nil {
		return err
	}
	l.SetVoltage3V3()
	// 36 us between words, as GB-Link's own tools use (it backs off by itself if the link slips)
	s.link, s.helper = l, &Helper{link: l, gap: 36}
	s.ui.Log("GB-Link connected on %s.", port)
	return nil
}

// ensureHelper makes sure the helper is running on the GBA, asking the player to switch it on.
// why is the reason shown to the player. Returns errCancelled if they cancel.
func (s *Session) ensureHelper(why string) error {
	if s.helper.Alive(1500 * time.Millisecond) {
		s.matchHelperLang()
		return nil
	}
	s.ui.SetGBA("off")
	for {
		which := "the cartridge you want to back up"
		if s.game != nil {
			which = s.game.Name()
		}
		id := s.ui.ShowPrompt(Prompt{Kind: "power", Title: why,
			Text: "Hold START and SELECT while you switch the GBA on, with " + which +
				" in it. If it's already on, switch it off first.",
			Buttons: []Button{{ID: "cancel", Label: "Cancel", Style: "secondary"}}})
		lang := s.helperLang()
		err := s.link.Multiboot(helperImageFor(lang), 365*24*time.Hour, func() bool { _, ok := s.ui.Answer(id); return ok || s.stopped.Load() })
		s.ui.ClearPrompt(id)
		if err == nil {
			s.ui.SetStatus("Starting the helper on the GBA...", "busy")
			if s.helper.Alive(8 * time.Second) {
				s.helperLg = 0 // it should be in the language already; make sure (and old copies of the helper ignore it)
				s.matchHelperLang()
				s.cart = nil // the cart may have changed while it was off
				s.ui.SetGBA("on")
				if s.bridge != nil {
					s.bridge.resync()
				}
				return nil
			}
			err = linkErr("The GBA didn't start the helper program.")
		}
		if le, ok := err.(*LinkError); ok && le.msg == "Cancelled" {
			return errCancelled
		}
		if !s.askRetry("The GBA didn't connect", err.Error()+" Check the link cable is pushed in at both ends, then try again.") {
			return errCancelled
		}
	}
}

func (s *Session) askRetry(title, text string) bool {
	a := s.ui.Ask(Prompt{Kind: "error", Title: title, Text: text,
		Buttons: []Button{{ID: "retry", Label: "Try again", Style: "primary"}, {ID: "stop", Label: "Cancel", Style: "secondary"}}})
	return a == "retry"
}

func (s *Session) checkCart() (CartInfo, error) {
	info, err := s.helper.SaveInfo()
	if err != nil {
		return info, err
	}
	if _, ok := saveTypes[info.Type]; !ok {
		return info, linkErr("No save chip was found on the cartridge.")
	}
	if s.game.Code != "" && info.Code != s.game.Code {
		return info, linkErr("The cartridge in the GBA is %s [%s], but the game you picked is %s [%s]. Put the right cartridge in.",
			info.Title, info.Code, s.game.Title, s.game.Code)
	}
	if s.saveType == 0 {
		s.saveType = info.Type
	}
	return info, nil
}

// ---------------------------------------------------------------------------- pull / push
func (s *Session) pull() error {
	if err := s.ensureHelper("Switch on your GBA to load your save"); err != nil {
		return err
	}
	info, err := s.checkCart()
	if err != nil {
		return err
	}
	s.ui.Log("Cart: %s [%s], save chip: %s.", info.Title, info.Code, saveTypes[s.saveType].Name)
	data, err := s.helper.ReadSave(s.saveType, func(d, t int) { s.ui.SetProgress("Loading your save from the cartridge", d, t) })
	s.ui.ClearProgress()
	if err != nil {
		return err
	}
	s.game.backup(data, "cart")
	if err := s.game.writeLocal(data); err != nil {
		return err
	}
	s.cart = data
	s.state.Pending, s.state.SyncedHash, s.state.SaveType = false, hash(data), s.saveType
	s.game.saveState(s.state)
	s.ui.SetCart("synced", "Your save was loaded from the cartridge.")
	s.ui.Log("Save loaded into %s.", filepath.Base(s.game.Sav))
	return nil
}

func (s *Session) needsPush() bool {
	data := s.game.localSave()
	st, ok := saveTypes[s.saveType]
	if data == nil || !ok || len(data) < st.Size {
		return false
	}
	return hash(data[:st.Size]) != s.state.SyncedHash
}

func (s *Session) markPending() {
	s.state.Pending = true
	s.game.saveState(s.state)
	s.ui.SetCart("pending", "Your latest save is on this PC. It'll be copied to the cartridge when the GBA is on.")
}

// push writes the PC save to the cart. With why != "", the player is asked to switch the GBA on;
// otherwise it only writes if the helper is running. Returns false if the GBA wasn't there.
func (s *Session) push(why string) (bool, error) {
	data := s.game.localSave()
	if data == nil {
		return true, nil
	}
	st, ok := saveTypes[s.saveType]
	if !ok {
		return false, linkErr("The save type isn't known yet - load the cart's save first.")
	}
	if len(data) < st.Size {
		return false, linkErr("%s is %d bytes, but the cartridge's save is %d bytes.", filepath.Base(s.game.Sav), len(data), st.Size)
	}
	data = data[:st.Size]
	if s.bridge != nil { // the GBA stops answering while it saves: let go of any buttons in mGBA
		s.bridge.letGoKeys()
	}
	if why != "" {
		if err := s.ensureHelper(why); err != nil {
			return false, err
		}
	} else if !s.helper.Alive(1500 * time.Millisecond) {
		s.ui.SetGBA("off")
		return false, nil
	}
	s.ui.SetGBA("on")
	if s.cart == nil {
		if _, err := s.checkCart(); err != nil {
			return false, err
		}
	}
	// until the write is verified this save counts as not synced, so if anything cuts it off,
	// the next launch writes it again
	s.state.Pending = true
	s.game.saveState(s.state)
	s.ui.SetWriting(true)
	n, err := s.helper.WriteSave(s.saveType, data, s.cart, func(d, t int) { s.ui.SetProgress("Saving to the cartridge", d, t) })
	s.ui.ClearProgress()
	if err == nil {
		s.cart = data
		s.state.Pending, s.state.SyncedHash = false, hash(data)
		s.game.saveState(s.state)
	}
	s.ui.SetWriting(false)
	if err != nil {
		return false, err
	}
	if n > 0 {
		s.ui.Log("Save written to the cart and verified (%d of %d blocks changed).", n, max(1, st.Size/saveChunk))
	}
	s.ui.SetCart("synced", "The cartridge has your latest save.")
	if s.bridge != nil {
		s.bridge.resync()
	}
	return true, nil
}

// pushWithRetries asks to switch the GBA on and keeps offering to retry. False = save kept on PC.
func (s *Session) pushWithRetries(why string) bool {
	for {
		s.linkMu.Lock()
		_, err := s.push(why)
		s.linkMu.Unlock()
		if err == nil {
			return true
		}
		if err != errCancelled && s.askRetry("Couldn't save to the cartridge", err.Error()) {
			continue
		}
		s.markPending()
		return false
	}
}

func (s *Session) pullWithRetries() error {
	for {
		s.linkMu.Lock()
		err := s.pull()
		s.linkMu.Unlock()
		if err == nil || err == errCancelled {
			return err
		}
		if !s.askRetry("Couldn't load the save", err.Error()) {
			return errCancelled
		}
	}
}

// ------------------------------------------------------------------------------ main flow
func (s *Session) run() {
	defer func() {
		if s.bridge != nil {
			s.bridge.stop()
		}
		if s.link != nil {
			s.link.Close()
		}
	}()
	g := s.game
	s.state = g.loadState()
	s.saveType = s.state.SaveType
	s.ui.SetPhase("connect")
	s.ui.SetStatus("Connecting to the GB-Link...", "busy")
	for {
		err := s.connect()
		if err == nil {
			break
		}
		if !s.askRetry("GB-Link not found", err.Error()+" Plug it in, then try again.") {
			s.ui.Finish("cancelled", "")
			return
		}
	}

	s.ui.SetPhase("load")
	if s.state.Pending && g.localSave() != nil {
		a := s.ui.Ask(Prompt{Kind: "question", Title: "Your last save isn't on the cartridge yet",
			Text: "Last time, your save couldn't be copied to the cartridge, so the newest save is on this PC.",
			Buttons: []Button{{ID: "write", Label: "Copy this PC's save to the cartridge", Style: "primary"},
				{ID: "load", Label: "Use the cartridge's save instead", Style: "secondary"}}})
		if a == "load" {
			if s.pullWithRetries() != nil {
				s.ui.Finish("cancelled", "")
				return
			}
		} else if !s.pushWithRetries("Switch on your GBA to copy your last save to it") {
			if s.ui.Ask(Prompt{Kind: "question", Title: "Play anyway?", Text: "You can play with the save on this PC. It'll be copied to the cartridge later.",
				Buttons: []Button{{ID: "play", Label: "Play anyway", Style: "primary"}, {ID: "stop", Label: "Cancel", Style: "secondary"}}}) != "play" {
				s.ui.Finish("cancelled", "")
				return
			}
		}
	} else if s.pullWithRetries() != nil {
		if s.ui.Ask(Prompt{Kind: "question", Title: "Play without the cartridge's save?",
			Text:    "mGBA will use the save on this PC (if there is one), and it won't be copied to the cartridge.",
			Buttons: []Button{{ID: "stop", Label: "Cancel", Style: "primary"}, {ID: "play", Label: "Play anyway", Style: "secondary"}}}) != "play" {
			s.ui.Finish("cancelled", "")
			return
		}
	}

	mgba := s.app.settings().MGBA
	if mgba == "" {
		s.ui.Ask(Prompt{Kind: "error", Title: "mGBA wasn't found", Text: "Choose where mGBA is in Settings, then press Play again.",
			Buttons: []Button{{ID: "ok", Label: "OK", Style: "primary"}}})
		s.ui.Finish("cancelled", "")
		return
	}
	s.useCartClock()
	if s.stopped.Load() { // the window was closed while the save was loading: don't start the game
		s.ui.Finish("cancelled", "")
		return
	}
	proc, err := s.startMGBA(mgba)
	if err != nil {
		s.ui.Ask(Prompt{Kind: "error", Title: "mGBA didn't start", Text: err.Error(), Buttons: []Button{{ID: "ok", Label: "OK", Style: "primary"}}})
		s.ui.Finish("cancelled", "")
		return
	}
	s.ui.SetPhase("play")
	s.ui.SetStatus("Playing in mGBA", "ok")
	s.watch(proc)
	if s.bridge != nil {
		s.bridge.stop()
		s.bridge = nil
	}
	s.ui.SetController(func(c *ControllerView) { c.Online, c.ScriptConnected = false, false })

	time.Sleep(500 * time.Millisecond)
	s.ui.SetPhase("finish")
	s.backupMGBASave() // a save made just before mGBA closed
	s.trimSav()
	if s.needsPush() {
		s.markPending()
		s.ui.SetStatus("Copying your save to the cartridge...", "busy")
		s.ui.Raise()
		if !s.pushWithRetries("Switch on your GBA to save to the cartridge") {
			s.ui.Finish("pending", "Your save is kept on this PC. Next time you play this game, you'll be asked to copy it to the cartridge first.")
			return
		}
	}
	s.ui.Finish("done", "The cartridge has your latest save. You can switch the GBA off.")
}

// ----------------------------------------------------------------------------------- mGBA
type mgbaProc struct {
	cmd  *exec.Cmd
	done chan struct{}
}

func (s *Session) startMGBA(mgba string) (*mgbaProc, error) {
	var lua string
	start := func(args ...string) (*mgbaProc, error) {
		name, dir := mgba, filepath.Dir(mgba)
		var pre []string
		if id, ok := strings.CutPrefix(mgba, "flatpak:"); ok {
			// mGBA's Flatpak only sees its own files: let it use the game's folder (the ROM, the .sav
			// and the backups) and the controller script's folder, and talk to this app over localhost
			name, dir = "flatpak", ""
			pre = []string{"run", "--filesystem=" + filepath.Dir(s.game.ROM), "--share=network"}
			if lua != "" {
				pre = append(pre, "--filesystem="+filepath.Dir(lua))
			}
			pre = append(pre, id)
		}
		cmd := exec.Command(name, append(append(pre, args...), s.game.ROM)...)
		cmd.Dir = dir
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		p := &mgbaProc{cmd: cmd, done: make(chan struct{})}
		go func() { cmd.Wait(); close(p.done) }()
		return p, nil
	}
	set := s.app.settings()
	if set.Controller {
		b, err := newBridge(s)
		if err != nil {
			s.ui.Log("GBA controller unavailable (%v).", err)
		} else {
			s.bridge = b
			log := func(f string, a ...any) { s.ui.Log(f, a...) }
			override := luaOverride(s.game.ROM, s.game.Code, log)
			if override == "" {
				override = foundOverride(s.game.ROM, s.game.Code, log)
			}
			lua = writeLua(b.port, override, luaTranslations(b.lang))
		}
	}
	if lua == "" {
		s.ui.SetController(func(c *ControllerView) { c.Enabled = false })
		return start()
	}
	s.ui.SetController(func(c *ControllerView) { c.Enabled, c.ScriptPath = true, lua })
	// mGBA 0.11 (and development builds) load the script at startup with --script. 0.10 doesn't
	// know that option and quits straight away; then start it without, and explain.
	p, err := start("--script", lua)
	if err != nil {
		return nil, err
	}
	select {
	case <-p.done:
		if p.cmd.ProcessState.ExitCode() != 0 {
			p, err = start()
			if err != nil {
				return nil, err
			}
			s.ui.SetController(func(c *ControllerView) { c.Manual = true })
			s.ui.Log("This mGBA can't load scripts at startup; load the controller script from Tools > Scripting.")
		}
	case <-time.After(3 * time.Second):
	}
	s.bridge.start()
	return p, nil
}

// watch keeps the cart in step while mGBA runs.
func (s *Session) watch(p *mgbaProc) {
	var lastSig string
	var stableSince time.Time
	warned := false
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-p.done:
			return
		case <-tick.C:
		}
		st, err := os.Stat(s.game.Sav)
		if err != nil {
			continue
		}
		sig := fmt.Sprint(st.ModTime().UnixNano(), st.Size())
		if sig != lastSig {
			lastSig, stableSince = sig, time.Now()
			continue
		}
		if stableSince.IsZero() || time.Since(stableSince) < 1500*time.Millisecond {
			continue
		}
		stableSince = time.Time{}
		s.linkMu.Lock()
		if !s.needsPush() {
			s.linkMu.Unlock()
			continue
		}
		s.ui.Log("In-game save detected.")
		s.backupMGBASave()
		ok, err := s.push("")
		s.linkMu.Unlock()
		if err != nil {
			s.ui.Log("Couldn't write the save to the cartridge just now (%v).", err)
			s.markPending()
			continue
		}
		if ok {
			warned = false
			continue
		}
		s.markPending()
		if s.app.settings().AskEachSave {
			s.ui.Raise()
			s.pushWithRetries("You saved - switch on your GBA to copy it over")
		} else if !warned {
			s.ui.Log("In-game save kept on this PC: the GBA is off. It'll be copied to the cartridge later.")
			warned = true
		}
	}
}

// ------------------------------------------------------------------------ GBA as controller
// The mGBA script: passes the GBA's buttons to mGBA, and reads the Pokemon game for the GBA's
// status screens (status.go). __PORT__ is the bridge's port.
//
//go:embed assets/gblink_controller.lua
var luaSource string

// writeLua writes the mGBA script next to this app (easy to find when loading it by hand), or
// in the settings folder, and returns its path.
func writeLua(port int, override, translations string) string {
	text := strings.ReplaceAll(luaSource, "__PORT__", strconv.Itoa(port))
	if override != "" {
		text = strings.Replace(text, "--@OVERRIDE@", "local OVERRIDE = "+override, 1)
	}
	if translations != "" {
		text = strings.Replace(text, "--@LANG@", translations, 1)
	}
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	dirs = append(dirs, configDir())
	for _, d := range dirs {
		p := filepath.Join(d, "gblink_controller.lua")
		if old, err := os.ReadFile(p); err == nil && string(old) == text {
			return p
		}
		if os.WriteFile(p, []byte(text), 0o644) == nil {
			return p
		}
	}
	return ""
}

type Bridge struct {
	s        *Session
	ln       net.Listener
	port     int
	mu       sync.Mutex
	clients  []net.Conn
	keys     int
	online   bool
	needShow bool
	lastOpt  string // the options last sent to the script (sendOptions)
	quit     chan struct{}
	wg       sync.WaitGroup

	// status screens (status.go)
	st          statusState
	modeNow     modeBox
	letGo       atomic.Bool // a save is being written: buttons let go until they're released
	game        string      // the Pokemon game the mGBA script found ("" = none yet)
	gameProblem string      // why the script can't read this game
	noRNG       bool        // the game has no RNG screen (your own builds)
	wc          wcSender    // Wonder Cards sent to the script (wondercards.go)
	scriptWC    bool        // the script in mGBA knows Wonder Cards (said so in its hello)
	screens     map[int]*screen
	bgs         map[string]*stBg // region map pictures from the script, by region
	encNew      int              // wild Pokemon the script saw, not counted yet
	encLast     string           // the last one: "ZIGZAGOON Lv3"
	encShiny    bool
	lang        byte // the game's language (lang.go): the status screens' text and font
}

func newBridge(s *Session) (*Bridge, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", controllerPort))
	if err != nil {
		if ln, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
			return nil, err
		}
	}
	b := &Bridge{s: s, ln: ln, port: ln.Addr().(*net.TCPAddr).Port, needShow: true, quit: make(chan struct{}),
		screens: map[int]*screen{}, bgs: map[string]*stBg{}, lang: 'E'}
	if s.game != nil {
		b.lang = gameLang(s.game.Code)
	}
	b.wc = wcSender{ui: s.ui, transmit: b.wcTransmit,
		noAnswer: "No answer from mGBA's script. Load gblink_controller.lua again in mGBA (Tools › Scripting… › File › Load script…), then try again."}
	b.st.mode = modeFromKey(s.app.settings().StatusMode)
	b.st.lastShown = b.st.mode
	b.modeNow.v.Store(int32(b.st.mode))
	b.st.unknown, b.st.onGBA = true, -1
	s.ui.SetController(func(c *ControllerView) { c.GBAShows = modeTitles[b.st.mode] })
	return b, nil
}

func (b *Bridge) start() {
	b.wg.Add(2)
	go b.acceptLoop()
	go b.pollLoop()
}

func (b *Bridge) stop() {
	select {
	case <-b.quit:
		return
	default:
	}
	close(b.quit)
	b.send(0, true)
	b.ln.Close()
	b.wg.Wait()
	b.mu.Lock()
	for _, c := range b.clients {
		c.Close()
	}
	b.mu.Unlock()
	// mGBA is closed: take the GBA off its status screens, back to the helper's own screen
	if b.st.supported && b.online {
		b.s.linkMu.Lock()
		b.s.helper.Status(stOpLeave, nil, nil)
		b.s.linkMu.Unlock()
	}
}

// sendOptions tells the script the settings it uses (only when they change, or force).
func (b *Bridge) sendOptions() {
	opt := "F1\n" // fix Emerald's starting seed
	if b.s.app.settings().KeepEmeraldSeed {
		opt = "F0\n"
	}
	b.mu.Lock()
	b.lastOpt = opt
	b.writeLocked([]byte(opt))
	b.mu.Unlock()
}

func (b *Bridge) resync() { b.mu.Lock(); b.needShow = true; b.mu.Unlock() }

func (b *Bridge) acceptLoop() {
	defer b.wg.Done()
	for {
		c, err := b.ln.Accept()
		if err != nil {
			return
		}
		if tc, ok := c.(*net.TCPConn); ok {
			tc.SetNoDelay(true)
		}
		b.mu.Lock()
		b.clients = append(b.clients, c)
		k := b.keys
		b.mu.Unlock()
		b.s.ui.SetController(func(cv *ControllerView) { cv.ScriptConnected = true })
		b.s.ui.Log("mGBA's controller script connected.")
		go b.readScript(c)
		b.sendMode(b.currentMode())
		b.send(k, true)
		b.sendOptions()
	}
}

func (b *Bridge) send(keys int, force bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if keys == b.keys && !force {
		return
	}
	b.keys = keys
	b.writeLocked([]byte(fmt.Sprintf("K%03x\n", keys)))
}

// writeAll sends a line to the mGBA script(s).
func (b *Bridge) writeAll(msg []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.writeLocked(msg)
}

func (b *Bridge) writeLocked(msg []byte) {
	alive := b.clients[:0]
	for _, c := range b.clients {
		c.SetWriteDeadline(time.Now().Add(200 * time.Millisecond))
		if _, err := c.Write(msg); err == nil {
			alive = append(alive, c)
		} else {
			c.Close()
		}
	}
	b.clients = alive
}

func (b *Bridge) pollLoop() {
	defer b.wg.Done()
	misses := 0
	lastBeat := time.Time{}
	s := b.s
	for {
		select {
		case <-b.quit:
			return
		default:
		}
		keys, fwd := -1, 0
		s.linkMu.Lock()
		if !b.online && misses >= 15 {
			b.tryRestart()
		}
		keys = s.helper.Keys()
		b.mu.Lock()
		show := b.needShow
		b.mu.Unlock()
		if keys >= 0 {
			fwd = b.statusTick(keys, show) // the menu and status screens, or controller mode
			if b.letGo.Swap(false) {       // a save was just written: wait for the buttons to be let go
				b.st.suppress, fwd = keys != 0, 0
			}
		}
		s.linkMu.Unlock()
		if keys < 0 {
			misses++
			if misses == 1 || misses == 15 {
				debugf("keys: no answer (%d)", misses)
			}
			if b.online && misses >= 15 { // ~0.15 s of silence: GBA off or busy
				b.online = false
				b.resync()
				b.send(0, false)
				s.ui.SetGBA("off")
				s.ui.SetController(func(c *ControllerView) { c.Online = false })
				s.ui.Log("The GBA stopped answering (switched off?). Use the keyboard or a pad in mGBA.")
			}
			if b.online {
				time.Sleep(10 * time.Millisecond)
			} else {
				time.Sleep(500 * time.Millisecond)
			}
			continue
		}
		misses = 0
		if !b.online {
			b.online = true
			s.ui.SetGBA("on")
			s.ui.SetController(func(c *ControllerView) { c.Online = true })
		}
		beat := time.Since(lastBeat) > 250*time.Millisecond // mGBA lets go after ~0.75 s without one
		if beat {
			lastBeat = time.Now()
			b.mu.Lock()
			old := b.lastOpt
			b.mu.Unlock()
			if want := map[bool]string{false: "F1\n", true: "F0\n"}[s.app.settings().KeepEmeraldSeed]; want != old {
				b.sendOptions()
			}
		}
		b.send(fwd, beat)
		time.Sleep(8 * time.Millisecond) // ~100 polls a second
	}
}

// tryRestart: the GBA was off. If it's waiting for multiboot now (switched on holding START +
// SELECT), start the helper again and catch the cart up with any save made meanwhile.
// Called with linkMu held.
func (b *Bridge) tryRestart() {
	s := b.s
	lang := s.helperLang()
	if err := s.link.Multiboot(helperImageFor(lang), 50*time.Millisecond, nil); err != nil {
		s.helper.Configure() // back to the helper's link timing
		return
	}
	s.ui.Log("GBA switched on - starting the helper again.")
	if !s.helper.Alive(8 * time.Second) {
		s.ui.Log("The helper didn't start. Switch the GBA off, then on again holding START + SELECT.")
		return
	}
	s.helperLg = 0 // it should be in the language already; make sure (and old copies of the helper ignore it)
	s.matchHelperLang()
	s.cart = nil
	b.resync()
	s.ui.SetGBA("on")
	if s.needsPush() {
		if _, err := s.push(""); err != nil {
			s.ui.Log("Couldn't write the save to the cartridge just now (%v).", err)
			s.markPending()
		}
	}
}
