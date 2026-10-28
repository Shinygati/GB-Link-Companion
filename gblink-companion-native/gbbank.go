// The Game Boy Pokémon Bank: the PC boxes of Pokémon Red, Blue, Yellow, Gold, Silver and Crystal,
// next to a bank of boxes kept on this PC. It's kept apart from the GBA bank (bank.go): Pokémon
// move between all the Game Boy and Game Boy Color games (converted between generation 1 and 2
// the way the Time Capsule and PKHeX do it), but never to or from the GBA games.
//
// The boxes come from a cartridge over GB-Link (the GBA in Game Boy Color mode, gbcart.go), or
// from a save file (a flash cart's SD card, an emulator). As with the GBA bank, nothing changes
// until "Save": then a note of the change is written first, the cartridge (or file) next, read
// back and checked, and the bank on this PC last. If anything stops in between, the next time
// that cartridge or file is opened the note is finished from what's on it: no Pokémon are lost.
package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	gbBankStart = 10
	gbBankMax   = 100
)

// ------------------------------------------------------------------------------- the bank
type GBBankMon struct {
	Mon   GBMon
	Added string
}

type GBBankBox struct {
	Name  string
	Slots [gbBoxSlots]*GBBankMon
}

type GBBank struct{ Boxes []GBBankBox }

func newGBBank() GBBank {
	b := GBBank{}
	b.addBoxes(gbBankStart)
	return b
}

func (b *GBBank) addBoxes(n int) {
	for i := 0; i < n && len(b.Boxes) < gbBankMax; i++ {
		b.Boxes = append(b.Boxes, GBBankBox{Name: fmt.Sprintf("BANK %d", len(b.Boxes)+1)})
	}
}

func (b GBBank) clone() GBBank {
	c := GBBank{Boxes: make([]GBBankBox, len(b.Boxes))}
	for i, box := range b.Boxes {
		c.Boxes[i].Name = box.Name
		for j, m := range box.Slots {
			if m != nil {
				mm := GBBankMon{Mon: GBMon{Gen: m.Mon.Gen, Raw: append([]byte(nil), m.Mon.Raw...)}, Added: m.Added}
				c.Boxes[i].Slots[j] = &mm
			}
		}
	}
	return c
}

func (b GBBank) count() int {
	n := 0
	for _, box := range b.Boxes {
		for _, m := range box.Slots {
			if m != nil {
				n++
			}
		}
	}
	return n
}

func (b *GBBank) put(m *GBBankMon) bool {
	for {
		for i := range b.Boxes {
			for j := range b.Boxes[i].Slots {
				if b.Boxes[i].Slots[j] == nil {
					b.Boxes[i].Slots[j] = m
					return true
				}
			}
		}
		if len(b.Boxes) >= gbBankMax {
			return false
		}
		b.addBoxes(1)
	}
}

func gbBankDir() string {
	d := filepath.Join(configDir(), "Pokemon Bank GB")
	os.MkdirAll(d, 0o755)
	return d
}
func gbBankBoxesDir() string   { return filepath.Join(gbBankDir(), "Boxes") }
func gbBankBackupsDir() string { return filepath.Join(gbBankDir(), "Backups") }
func gbBankPendingPath() string {
	return filepath.Join(gbBankDir(), "bank-pending.json")
}

var gbSlotFileNum = regexp.MustCompile(`(?i)^Slot (\d+)\b`)

func gbMonFileName(slot int, m GBMon) string {
	name := m.Nickname()
	if name == "" {
		name = gbSpeciesName(m.Species())
	}
	return fmt.Sprintf("Slot %02d - %s - %s%s", slot+1, fileSafe(name), strings.ToUpper(m.Identity()[:8]), m.Ext())
}

// writeGBBankFolder writes the whole bank into root (a new folder): PKHeX .pk1/.pk2 files.
func writeGBBankFolder(root string, b GBBank) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	for i, box := range b.Boxes {
		dir := filepath.Join(root, fmt.Sprintf("Box %02d - %s", i+1, fileSafe(box.Name)))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		for j, m := range box.Slots {
			if m == nil {
				continue
			}
			p := filepath.Join(dir, gbMonFileName(j, m.Mon))
			if err := writeDurably(p, m.Mon.File()); err != nil {
				return err
			}
			if t, err := time.ParseInLocation("2006-01-02", m.Added, time.Local); err == nil {
				os.Chtimes(p, t, t)
			}
		}
	}
	return writeDurably(filepath.Join(root, bankComplete), []byte("ok\n"))
}

var gbBankWarnings []string

func readGBBankFolder(root string) (GBBank, []string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return GBBank{}, nil, fmt.Errorf("couldn't read the bank's folders: %v", err)
	}
	type boxDir struct {
		num        int
		name, path string
	}
	var dirs []boxDir
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		d := boxDir{num: 1 << 30, name: e.Name(), path: filepath.Join(root, e.Name())}
		if m := boxDirName.FindStringSubmatch(e.Name()); m != nil {
			d.num, _ = strconv.Atoi(m[1])
			d.name = strings.TrimSpace(m[2])
		}
		dirs = append(dirs, d)
	}
	sort.SliceStable(dirs, func(i, j int) bool {
		if dirs[i].num != dirs[j].num {
			return dirs[i].num < dirs[j].num
		}
		return dirs[i].path < dirs[j].path
	})
	if len(dirs) > gbBankMax {
		dirs = dirs[:gbBankMax]
	}
	var b GBBank
	var warn []string
	var spare []*GBBankMon
	for i, d := range dirs {
		box := GBBankBox{Name: nonBlank(d.name, fmt.Sprintf("BANK %d", i+1))}
		files, _ := os.ReadDir(d.path)
		var loose []*GBBankMon
		for _, f := range files {
			ext := strings.ToLower(filepath.Ext(f.Name()))
			if f.IsDir() || (ext != ".pk1" && ext != ".pk2") {
				continue
			}
			gen := 1
			if ext == ".pk2" {
				gen = 2
			}
			p := filepath.Join(d.path, f.Name())
			data, err := os.ReadFile(p)
			var m GBMon
			if err == nil {
				m, err = gbMonFromFile(data, gen)
			}
			if err != nil {
				warn = append(warn, fmt.Sprintf("%s: %v", filepath.Join(filepath.Base(d.path), f.Name()), err))
				continue
			}
			bm := &GBBankMon{Mon: m}
			if info, err := f.Info(); err == nil {
				bm.Added = info.ModTime().Format("2006-01-02")
			}
			if s := gbSlotFileNum.FindStringSubmatch(f.Name()); s != nil {
				if n, _ := strconv.Atoi(s[1]); n >= 1 && n <= gbBoxSlots && box.Slots[n-1] == nil {
					box.Slots[n-1] = bm
					continue
				}
			}
			loose = append(loose, bm)
		}
		for _, m := range loose {
			placed := false
			for k := range box.Slots {
				if box.Slots[k] == nil {
					box.Slots[k], placed = m, true
					break
				}
			}
			if !placed {
				spare = append(spare, m)
			}
		}
		b.Boxes = append(b.Boxes, box)
	}
	if len(b.Boxes) == 0 {
		b.addBoxes(gbBankStart)
	}
	for _, m := range spare {
		if !b.put(m) {
			warn = append(warn, "the bank is full: a Pokémon couldn't be placed")
		}
	}
	return b, warn, nil
}

func recoverGBBankFolders() {
	dir := gbBankBoxesDir()
	next, prev := dir+".new", dir+".old"
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	if exists(next) {
		if !exists(dir) && exists(filepath.Join(next, bankComplete)) {
			if os.Rename(next, dir) == nil {
				os.Remove(filepath.Join(dir, bankComplete))
			}
		} else {
			os.RemoveAll(next)
		}
	}
	if exists(prev) {
		if exists(dir) {
			backupGBBankFolder(prev)
		} else {
			os.Rename(prev, dir)
		}
	}
}

func backupGBBankFolder(old string) {
	dir := gbBankBackupsDir()
	if os.MkdirAll(dir, 0o755) != nil {
		return
	}
	os.Remove(filepath.Join(old, bankComplete))
	dst := filepath.Join(dir, time.Now().Format("2006-01-02 15-04-05"))
	for n := 2; ; n++ {
		if _, err := os.Stat(dst); os.IsNotExist(err) {
			break
		}
		dst = filepath.Join(dir, time.Now().Format("2006-01-02 15-04-05")+fmt.Sprintf(" (%d)", n))
	}
	if os.Rename(old, dst) != nil {
		return
	}
	entries, _ := os.ReadDir(dir)
	var copies []string
	for _, e := range entries {
		if e.IsDir() {
			copies = append(copies, e.Name())
		}
	}
	sort.Strings(copies)
	for len(copies) > bankBackups {
		os.RemoveAll(filepath.Join(dir, copies[0]))
		copies = copies[1:]
	}
}

func loadGBBank() (GBBank, error) {
	recoverGBBankFolders()
	dir := gbBankBoxesDir()
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return newGBBank(), nil
	}
	b, warn, err := readGBBankFolder(dir)
	gbBankWarnings = warn
	return b, err
}

func saveGBBank(b GBBank) error {
	recoverGBBankFolders()
	dir := gbBankBoxesDir()
	next, prev := dir+".new", dir+".old"
	os.RemoveAll(next)
	if err := writeGBBankFolder(next, b); err != nil {
		os.RemoveAll(next)
		return fmt.Errorf("couldn't write the bank's folders: %v", err)
	}
	if _, err := os.Stat(dir); err == nil {
		if err := os.Rename(dir, prev); err != nil {
			os.RemoveAll(next)
			return fmt.Errorf("couldn't replace the bank's folders (is one of them open?): %v", err)
		}
	}
	if err := os.Rename(next, dir); err != nil {
		os.Rename(prev, dir)
		return fmt.Errorf("couldn't replace the bank's folders: %v", err)
	}
	os.Remove(filepath.Join(dir, bankComplete))
	if _, err := os.Stat(prev); err == nil {
		backupGBBankFolder(prev)
	}
	return nil
}

// --------------------------------------------------------------------------- the pending note
type gbJournalMon struct {
	Gen  int    `json:"gen"`
	Data string `json:"data"`
}

type gbJournal struct {
	Owner     string                      `json:"owner"`
	Game      string                      `json:"game"`
	File      string                      `json:"file,omitempty"` // a save file, not a cartridge
	Bank      [][gbBoxSlots]*gbJournalMon `json:"bank"`
	Names     []string                    `json:"names"`
	Deposited []gbJournalMon              `json:"deposited"`
	Withdrawn []gbJournalMon              `json:"withdrawn"`
	Started   string                      `json:"started"`
}

func jm(m GBMon) gbJournalMon { return gbJournalMon{m.Gen, hex.EncodeToString(m.Raw)} }
func (j gbJournalMon) mon() (GBMon, bool) {
	b, err := hex.DecodeString(j.Data)
	if err != nil || len(b) < 3 {
		return GBMon{}, false
	}
	m := GBMon{Gen: j.Gen, Raw: b}
	return m, m.Present()
}

func (j *gbJournal) bank() GBBank {
	var b GBBank
	for i, slots := range j.Bank {
		box := GBBankBox{Name: fmt.Sprintf("BANK %d", i+1)}
		if i < len(j.Names) {
			box.Name = j.Names[i]
		}
		for k, s := range slots {
			if s != nil {
				if m, ok := s.mon(); ok {
					box.Slots[k] = &GBBankMon{Mon: m, Added: time.Now().Format("2006-01-02")}
				}
			}
		}
		b.Boxes = append(b.Boxes, box)
	}
	return b
}

func loadGBJournal() (*gbJournal, error) {
	data, err := os.ReadFile(gbBankPendingPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var j gbJournal
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, fmt.Errorf("the Game Boy bank's unfinished change (%s) is damaged: %v", gbBankPendingPath(), err)
	}
	return &j, nil
}

func saveGBJournal(j *gbJournal) error {
	data, err := json.MarshalIndent(j, "", " ")
	if err != nil {
		return err
	}
	return writeFileSafely(gbBankPendingPath(), data)
}

func clearGBJournal() { os.Remove(gbBankPendingPath()) }

func gbIdentitiesOn(s *GBSave) map[string]bool {
	have := map[string]bool{}
	for _, box := range s.Boxes {
		for _, m := range box {
			if m.Present() {
				have[m.Identity()] = true
			}
		}
	}
	for _, m := range s.PartyMons() {
		if m.Present() {
			have[m.Identity()] = true
		}
	}
	return have
}

// finishGBJournal works out the bank after a change that was cut short, from the save as it is
// now: one going to the bank that's still on the cartridge stays there, one going to the
// cartridge that isn't on it goes back in the bank.
func finishGBJournal(j *gbJournal, s *GBSave) (GBBank, string) {
	bank := j.bank()
	have := gbIdentitiesOn(s)
	kept, back := 0, 0
	for _, d := range j.Deposited {
		m, ok := d.mon()
		if !ok || !have[m.Identity()] {
			continue
		}
		for i := range bank.Boxes {
			for k, x := range bank.Boxes[i].Slots {
				if x != nil && x.Mon.Identity() == m.Identity() {
					bank.Boxes[i].Slots[k] = nil
					kept++
				}
			}
		}
	}
	for _, w := range j.Withdrawn {
		m, ok := w.mon()
		if !ok || have[m.Identity()] {
			continue
		}
		if bank.put(&GBBankMon{Mon: m, Added: time.Now().Format("2006-01-02")}) {
			back++
		}
	}
	msg := "The last change was written, so the bank has been brought up to date."
	if kept > 0 || back > 0 {
		msg = fmt.Sprintf("The last change didn't finish. The bank was matched to the save: %d Pokémon stayed in the game and %d went back in the bank.", kept, back)
	}
	return bank, msg
}

// ----------------------------------------------------------------------------- the session
type gbRef = slotRef // a slot in the game's boxes (Bank false) or the bank's

// GBBankSession is what the Game Boy bank's window shows and changes.
type GBBankSession struct {
	mu sync.Mutex
	s  *Session

	Game, GameName string
	Trainer        string
	TID            int
	owner          string
	save           *GBSave
	file           string // the save file open (not a cartridge)
	raw            bool   // the cartridge's header isn't the game's (a flash cart's menu): its RAM is read directly
	BoxNames       []string
	orig, Cart     [][]GBMon
	origBank, Bank GBBank
	PartyCount     int

	CartBox, BankBox int
	Mode             string
	Multi, Marked    map[gbRef]bool
	Held, Sel        *gbRef
	Note             string
	Ready            bool
	CartLoaded       bool
	Locked           bool
	Loading, Saving  bool
	Rev              uint64

	cmd chan string
}

func (a *App) openGBBank() error {
	a.mu.Lock()
	if a.session != nil {
		a.mu.Unlock()
		return fmt.Errorf("Something is already running.")
	}
	s := &Session{ui: a.ui, app: a, cancel: make(chan struct{})}
	b := &GBBankSession{s: s, cmd: make(chan string, 8), Mode: modeMove, Multi: map[gbRef]bool{}, Marked: map[gbRef]bool{}}
	s.gbbank = b
	a.session = s
	a.mu.Unlock()
	a.ui.update(func(st *State) {
		st.Game = &GameView{Name: "Game Boy Pokémon Bank", GB: true}
		st.Phase, st.Mode, st.Status, st.Progress, st.Prompt = "connect", "gbbank", "", nil, nil
		st.Cart, st.LastSync, st.GBA, st.Result, st.ResultText, st.BackupPath = "", "", "", "", "", ""
		st.Verify, st.Controller, st.Log, st.GBBank, st.Tool = nil, ControllerView{}, nil, b, ""
	})
	go b.run()
	return nil
}

func (a *App) gbBankSend(c string) {
	a.mu.Lock()
	s := a.session
	a.mu.Unlock()
	if s != nil && s.gbbank != nil {
		select {
		case s.gbbank.cmd <- c:
		default:
		}
	}
}

func (b *GBBankSession) run() {
	s := b.s
	defer s.closeGB()
	if why := b.openOffline(); why != "" {
		s.ui.Finish("failed", why)
		return
	}
	s.ui.SetPhase("play")
	for {
		select {
		case <-s.cancel:
			s.ui.Finish("cancelled", "Nothing was changed in the game or in the bank.")
			return
		case c := <-b.cmd:
			switch c {
			case "load", "file":
				b.setLoading(true)
				var why string
				if c == "load" {
					why = b.loadCart()
				} else {
					why = b.loadFile()
				}
				b.setLoading(false)
				s.ui.SetStatus("", "")
				s.ui.ClearProgress()
				if why != "" && why != "cancelled" {
					b.setNote(why)
				}
				s.ui.update(func(*State) {})
			case "save":
				if done, why := b.commit(); done {
					s.ui.Finish("failed", why)
					return
				}
				s.ui.SetStatus("", "")
			case "undo":
				b.undo()
			case "release":
				n := b.markedCount()
				if n == 0 {
					continue
				}
				if s.ui.Ask(Prompt{Kind: "question", Title: fmt.Sprintf("Release %d Pokémon?", n),
					Text:    "Once you save, they're gone for good. Until you save, Undo changes brings them back.",
					Buttons: []Button{{ID: "release", Label: "Release", Style: "primary"}, {ID: "keep", Label: "Keep them", Style: "secondary"}}}) == "release" {
					b.releaseMarked()
				}
				s.ui.update(func(*State) {})
			case "done":
				if b.dirty() && s.ui.Ask(Prompt{Kind: "question", Title: "Leave without saving?",
					Text:    "The Pokémon you moved haven't been saved. If you leave now, the game and the bank stay as they were.",
					Buttons: []Button{{ID: "leave", Label: "Leave without saving", Style: "primary"}, {ID: "stay", Label: "Keep going", Style: "secondary"}}}) != "leave" {
					s.app.mu.Lock()
					s.app.homeAfter = false
					s.app.mu.Unlock()
					continue
				}
				s.ui.Finish("bank", "Your Pokémon are where you left them.")
				return
			}
		}
	}
}

func (b *GBBankSession) setLoading(on bool) {
	b.mu.Lock()
	b.Loading = on
	b.Rev++
	b.mu.Unlock()
	b.s.ui.update(func(*State) {})
}

func (b *GBBankSession) setNote(t string) {
	b.mu.Lock()
	b.Note = t
	b.Rev++
	b.mu.Unlock()
}

func emptyGBBoxes(n int) [][]GBMon {
	out := make([][]GBMon, n)
	for i := range out {
		out[i] = make([]GBMon, gbBoxSlots)
	}
	return out
}

func copyGBBoxes(in [][]GBMon) [][]GBMon {
	out := make([][]GBMon, len(in))
	for i := range in {
		out[i] = make([]GBMon, len(in[i]))
		for k, m := range in[i] {
			if m.Raw != nil {
				out[i][k] = GBMon{Gen: m.Gen, Raw: append([]byte(nil), m.Raw...)}
			}
		}
	}
	return out
}

func (b *GBBankSession) openOffline() string {
	note, locked := "", false
	if j, err := loadGBJournal(); err != nil {
		return err.Error()
	} else if j != nil {
		locked = true
		where := "cartridge"
		if j.File != "" {
			where = "save file (" + filepath.Base(j.File) + ")"
		}
		note = fmt.Sprintf("The last change was being written to %s's %s %s when it stopped. Open that %s again to finish it: until then the bank can be looked through but not changed.",
			strings.SplitN(j.Owner, "|", 2)[0], gbGameName(j.Game), where, where)
	}
	bank, err := loadGBBank()
	if err != nil {
		return err.Error()
	}
	if len(gbBankWarnings) > 0 {
		for _, w := range gbBankWarnings {
			b.s.ui.Log("Bank: couldn't use %s", w)
		}
		note = joinNotes(note, fmt.Sprintf("%d file(s) in the bank's folders couldn't be read (see Details).", len(gbBankWarnings)))
	}
	b.mu.Lock()
	b.orig, b.Cart = emptyGBBoxes(sav1Boxes), emptyGBBoxes(sav1Boxes)
	b.origBank, b.Bank = bank.clone(), bank
	b.Note, b.Locked, b.Ready = note, locked, true
	b.Rev++
	b.mu.Unlock()
	b.s.ui.update(func(*State) {})
	return ""
}

// readCartSave reads the cartridge's save: the fast way for a Pokémon cartridge, or straight from
// its RAM for a flash cart showing its own menu. game is the game from the header ("" if unknown).
func (b *GBBankSession) readCartSave() (raw []byte, game string, why string) {
	s := b.s
	if s.link == nil {
		if !s.connectOrFinishQuiet() {
			return nil, "", "cancelled"
		}
	}
	h, ok := s.gbReady("Switch on your GBA to open its boxes", "your Pokémon Red, Blue, Yellow, Gold, Silver or Crystal cartridge")
	if !ok {
		return nil, "", "cancelled"
	}
	_, game = gbGameOfTitle(h.Title)
	var err error
	if game != "" && h.RAMSize >= gbSaveRAM {
		b.raw = false
		raw, err = s.gbc.dump(true, func(d, t int) { s.ui.SetProgress("Reading the save from the cartridge", d, t) })
	} else {
		if s.ui.Ask(Prompt{Kind: "question", Title: "This cartridge isn't a Pokémon game",
			Text: "The cartridge says it's " + nonBlank(h.Title, "an unnamed game") + ". If it's a flash cart (like an EZ-Flash Junior) " +
				"that keeps the game's save in its memory, its save can be read straight from there. If the boxes can't be found in it, " +
				"nothing is changed: copy the .sav from its SD card instead and use “Open a save file”.",
			Buttons: []Button{{ID: "read", Label: "Read its save memory", Style: "primary"}, {ID: "cancel", Label: "Cancel", Style: "secondary"}}}) != "read" {
			return nil, "", "cancelled"
		}
		b.raw = true
		raw, err = s.gbc.readSRAMRaw(func(d, t int) { s.ui.SetProgress("Reading the cartridge's save memory", d, t) })
		game = ""
	}
	s.ui.ClearProgress()
	if err != nil {
		return nil, "", "Couldn't read the save: " + err.Error()
	}
	return raw, game, ""
}

func (s *Session) connectOrFinishQuiet() bool {
	for {
		err := s.connect()
		if err == nil {
			return true
		}
		if !s.askRetry("GB-Link not found", err.Error()+" Plug it in, then try again.") {
			return false
		}
	}
}

func (b *GBBankSession) loadCart() string {
	raw, game, why := b.readCartSave()
	if why != "" {
		return why
	}
	return b.open(raw, game, "")
}

func (b *GBBankSession) loadFile() string {
	if !pickMu.TryLock() {
		return ""
	}
	path, err := pickFile("Open a Pokémon Red, Blue, Yellow, Gold, Silver or Crystal save", "Save files (*.sav, *.srm)", "*.sav;*.srm", b.s.app.backupFolder())
	pickMu.Unlock()
	if err != nil || path == "" {
		return "cancelled"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "Couldn't read that file: " + err.Error()
	}
	return b.open(raw, "", path)
}

// open shows a save's boxes beside the bank (file: the save file, "" for the cartridge).
func (b *GBBankSession) open(raw []byte, game, file string) string {
	s := b.s
	save, err := parseGBSave(raw, game)
	if err != nil {
		return "Couldn't open the boxes: " + err.Error() + ". Nothing was changed."
	}
	trainer, tid := save.Trainer()
	s.ui.Log("Save: %s (%s), ID %05d.", trainer, save.GameName(), tid)
	j, err := loadGBJournal()
	if err != nil {
		return err.Error()
	}
	note, reload := "", false
	if j != nil {
		if j.Owner != save.Owner() || (j.File != "") != (file != "") {
			return fmt.Sprintf("The last change in the bank was being written to %s's %s when it stopped. Open that one first to finish the change, so no Pokémon are lost.",
				strings.SplitN(j.Owner, "|", 2)[0], gbGameName(j.Game))
		}
		bank, msg := finishGBJournal(j, save)
		if err := saveGBBank(bank); err != nil {
			return "Couldn't finish the last change in the bank: " + err.Error()
		}
		clearGBJournal()
		s.ui.Log("%s", msg)
		note, reload = msg, true
	}
	var bank GBBank
	if reload {
		if bank, err = loadGBBank(); err != nil {
			return err.Error()
		}
	}
	b.mu.Lock()
	b.Game, b.GameName = save.Game, save.GameName()
	b.save, b.owner, b.file = save, save.Owner(), file
	b.Trainer, b.TID = trainer, tid
	b.BoxNames = save.BoxNames()
	b.orig = copyGBBoxes(save.Boxes)
	b.Cart = copyGBBoxes(save.Boxes)
	if reload {
		b.origBank, b.Bank = bank.clone(), bank
		if b.BankBox >= len(b.Bank.Boxes) {
			b.BankBox = 0
		}
	}
	b.PartyCount = save.Party
	b.CartBox, b.Held, b.Locked = 0, nil, false
	b.Multi, b.Marked = map[gbRef]bool{}, map[gbRef]bool{}
	b.CartLoaded = true
	b.Note = note
	b.Rev++
	b.mu.Unlock()
	sub := b.GameName
	if file != "" {
		sub += " · " + filepath.Base(file)
	}
	s.ui.update(func(v *State) { v.Game = &GameView{Name: "Game Boy Pokémon Bank", Title: sub, GB: true} })
	return ""
}

// ----------------------------------------------------------------------------- moving them
func (b *GBBankSession) gen() int {
	if b.save != nil {
		return b.save.Gen
	}
	return 2
}

func (b *GBBankSession) boxCount() int {
	if b.save != nil {
		return len(b.save.Boxes)
	}
	return sav1Boxes
}

func (b *GBBankSession) get(r gbRef) *GBMon {
	if r.Slot < 0 || r.Slot >= b.slots(r.Bank) {
		return nil
	}
	if r.Bank {
		if r.Box < len(b.Bank.Boxes) && b.Bank.Boxes[r.Box].Slots[r.Slot] != nil {
			m := b.Bank.Boxes[r.Box].Slots[r.Slot].Mon
			return &m
		}
		return nil
	}
	if r.Box < len(b.Cart) && b.Cart[r.Box][r.Slot].Present() {
		m := b.Cart[r.Box][r.Slot]
		return &m
	}
	return nil
}

func (b *GBBankSession) meta(r gbRef) *GBBankMon {
	if r.Bank && r.Box < len(b.Bank.Boxes) && r.Slot >= 0 && r.Slot < gbBoxSlots {
		return b.Bank.Boxes[r.Box].Slots[r.Slot]
	}
	return nil
}

// fits: why m can't go into the game's boxes ("" if it can). As in the games themselves (and
// PKHeX), Japanese, Korean and international Pokémon stay in their own games.
func (b *GBBankSession) fits(m *GBMon) string {
	if m == nil || b.save == nil {
		return ""
	}
	if r := m.Region(); r != b.save.Region {
		name := m.Nickname()
		if name == "" {
			name = gbSpeciesName(m.Species())
		}
		return fmt.Sprintf("%s is from a %s game, and this one is %s. As in the games themselves, Pokémon can't be traded between them.",
			name, gbRegionName(r), gbRegionName(b.save.Region))
	}
	if b.gen() == 2 {
		return ""
	}
	return m.gen2Only()
}

// slots: how many Pokémon a box holds on that side (the Japanese games' boxes hold 30).
func (b *GBBankSession) slots(bank bool) int {
	if bank || b.save == nil {
		return gbBoxSlots
	}
	return b.save.Slots()
}

// place puts m (nil: none) in a slot, in the game's form when it's the game's box.
func (b *GBBankSession) place(r gbRef, m *GBMon, meta *GBBankMon) {
	if r.Bank {
		switch {
		case m == nil:
			b.Bank.Boxes[r.Box].Slots[r.Slot] = nil
		case meta != nil && bytes.Equal(meta.Mon.Raw, m.Raw):
			b.Bank.Boxes[r.Box].Slots[r.Slot] = meta
		default:
			b.Bank.Boxes[r.Box].Slots[r.Slot] = &GBBankMon{Mon: *m, Added: time.Now().Format("2006-01-02")}
		}
		return
	}
	if m == nil {
		b.Cart[r.Box][r.Slot] = GBMon{}
		return
	}
	cm, err := m.as(b.gen())
	if err != nil {
		return // checked before (fits)
	}
	// a Pokémon that came from this very slot keeps its exact bytes
	if o := b.orig[r.Box][r.Slot]; o.Present() && o.Identity() == cm.Identity() && o.Gen == cm.Gen {
		if m.Gen == o.Gen {
			cm = GBMon{Gen: o.Gen, Raw: append([]byte(nil), m.Raw...)}
		}
	}
	b.Cart[r.Box][r.Slot] = cm
}

func (b *GBBankSession) editable() bool { return b.Ready && !b.Saving && !b.Loading && !b.Locked }

func (b *GBBankSession) shown(bank bool) int {
	if bank {
		return b.BankBox
	}
	return b.CartBox
}

func (b *GBBankSession) SetMode(m string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if m != modeMove && m != modeMulti && m != modeRelease || m == b.Mode {
		return
	}
	b.Mode, b.Held, b.Note = m, nil, ""
	b.Multi, b.Marked = map[gbRef]bool{}, map[gbRef]bool{}
	b.Rev++
}

func (b *GBBankSession) Tap(r gbRef) {
	b.mu.Lock()
	defer b.mu.Unlock()
	has := b.get(r) != nil
	if has {
		b.Sel = &r
		b.Rev++
	}
	if !b.editable() {
		return
	}
	b.Note = ""
	switch b.Mode {
	case modeMulti: // a click only chooses: moving them is always a drag
		if has {
			if b.Multi[r] {
				delete(b.Multi, r)
			} else {
				b.Multi[r] = true
			}
		}
	case modeRelease:
		if has {
			if b.Marked[r] {
				delete(b.Marked, r)
			} else {
				b.Marked[r] = true
			}
		}
	default: // Move: a click only shows its summary; Pokémon are moved by dragging them
		if !has {
			b.Sel = nil
		}
	}
	b.Rev++
}

func (b *GBBankSession) swap(from, to gbRef) bool {
	moving, there := b.get(from), b.get(to)
	if moving == nil {
		return false
	}
	if !to.Bank && !b.CartLoaded || !from.Bank && !b.CartLoaded {
		return false
	}
	why := ""
	if !to.Bank {
		why = b.fits(moving)
	}
	if why == "" && !from.Bank {
		why = b.fits(there)
	}
	if why != "" {
		b.Note = why
		return false
	}
	fm, tm := b.meta(from), b.meta(to)
	b.place(to, moving, fm)
	b.place(from, there, tm)
	b.Sel = &to
	return true
}

func (b *GBBankSession) Drop(from, to gbRef) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.editable() || from == to {
		return
	}
	b.Note, b.Held = "", nil
	if b.Mode == modeMulti && b.Multi[from] && len(b.Multi) > 1 {
		b.moveGroup(b.picked(b.Multi), to)
	} else if b.swap(from, to) && b.Mode == modeMulti {
		b.Multi = map[gbRef]bool{}
	}
	b.Rev++
}

func (b *GBBankSession) picked(set map[gbRef]bool) []gbRef {
	var out []gbRef
	for r, on := range set {
		if on && b.get(r) != nil {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		x, y := out[i], out[j]
		if x.Bank != y.Bank {
			return !x.Bank
		}
		if x.Box != y.Box {
			return x.Box < y.Box
		}
		return x.Slot < y.Slot
	})
	return out
}

func (b *GBBankSession) moveGroup(refs []gbRef, to gbRef) {
	if len(refs) == 0 {
		return
	}
	if !to.Bank && !b.CartLoaded {
		b.Note = "Open a game's boxes first."
		return
	}
	type mon struct {
		m    GBMon
		meta *GBBankMon
	}
	var mons []mon
	for _, r := range refs {
		m := b.get(r)
		if !to.Bank {
			if why := b.fits(m); why != "" {
				b.Note = "One of them can't go there: " + why
				return
			}
		}
		mons = append(mons, mon{*m, b.meta(r)})
	}
	for _, r := range refs {
		b.place(r, nil, nil)
	}
	var dest []gbRef
	box, slot, added := to.Box, to.Slot, 0
	for len(dest) < len(mons) {
		boxes := b.boxCount()
		if to.Bank {
			boxes = len(b.Bank.Boxes)
		}
		if box >= boxes {
			if !to.Bank || len(b.Bank.Boxes) >= gbBankMax {
				break
			}
			b.Bank.addBoxes(1)
			added++
		}
		r := gbRef{Bank: to.Bank, Box: box, Slot: slot}
		if b.get(r) == nil {
			dest = append(dest, r)
		}
		if slot++; slot == b.slots(to.Bank) {
			box, slot = box+1, 0
		}
	}
	if len(dest) < len(mons) {
		for i, r := range refs {
			m := mons[i].m
			b.place(r, &m, mons[i].meta)
		}
		if added > 0 {
			b.Bank.Boxes = b.Bank.Boxes[:len(b.Bank.Boxes)-added]
		}
		b.Note = fmt.Sprintf("There isn't room for %d Pokémon from there. Start further back, or make some room.", len(mons))
		return
	}
	b.Multi = map[gbRef]bool{}
	for i, r := range dest {
		m := mons[i].m
		b.place(r, &m, mons[i].meta)
	}
	b.Sel = &dest[0]
	if dest[0].Box != b.shown(to.Bank) {
		if to.Bank {
			b.BankBox = dest[0].Box
		} else {
			b.CartBox = dest[0].Box
		}
	}
	b.Note = fmt.Sprintf("Moved %d Pokémon.", len(mons))
}

func (b *GBBankSession) SendTo(bank bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.editable() {
		return
	}
	if !bank && !b.CartLoaded {
		b.Note = "Open a game's boxes first."
		b.Rev++
		return
	}
	var refs []gbRef
	for _, r := range b.picked(b.Multi) {
		if r.Bank != bank {
			refs = append(refs, r)
		}
	}
	b.Note = ""
	if len(refs) == 0 {
		b.Note = "They're all there already."
	} else {
		b.moveGroup(refs, gbRef{Bank: bank, Box: b.shown(bank)})
	}
	b.Rev++
}

func (b *GBBankSession) SelectBox(bank bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.editable() {
		return
	}
	set := b.Multi
	if b.Mode == modeRelease {
		set = b.Marked
	}
	all := true
	var refs []gbRef
	for i := 0; i < b.slots(bank); i++ {
		r := gbRef{Bank: bank, Box: b.shown(bank), Slot: i}
		if b.get(r) != nil {
			refs = append(refs, r)
			all = all && set[r]
		}
	}
	for _, r := range refs {
		if all {
			delete(set, r)
		} else {
			set[r] = true
		}
	}
	b.Rev++
}

func (b *GBBankSession) SelectRefs(refs []gbRef, add bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.editable() || b.Mode == modeMove {
		return
	}
	set := &b.Multi
	if b.Mode == modeRelease {
		set = &b.Marked
	}
	if !add {
		*set = map[gbRef]bool{}
	}
	for _, r := range refs {
		if b.get(r) != nil {
			(*set)[r] = true
			b.Sel = &r
		}
	}
	b.Note = ""
	b.Rev++
}

func (b *GBBankSession) ClearPicks() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Multi, b.Marked, b.Held = map[gbRef]bool{}, map[gbRef]bool{}, nil
	b.Rev++
}

func (b *GBBankSession) markedCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.picked(b.Marked))
}

func (b *GBBankSession) releaseMarked() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.editable() {
		return
	}
	refs := b.picked(b.Marked)
	for _, r := range refs {
		b.place(r, nil, nil)
	}
	b.Marked = map[gbRef]bool{}
	if b.Sel != nil && b.get(*b.Sel) == nil {
		b.Sel = nil
	}
	if len(refs) > 0 {
		b.Note = fmt.Sprintf("%d released. They're gone for good once you save (Undo changes brings them back until then).", len(refs))
	}
	b.Rev++
}

func (b *GBBankSession) Turn(bank bool, by int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if bank {
		n := len(b.Bank.Boxes)
		b.BankBox = ((b.BankBox+by)%n + n) % n
	} else {
		n := b.boxCount()
		b.CartBox = ((b.CartBox+by)%n + n) % n
	}
	b.Rev++
}

func (b *GBBankSession) AddBox() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.Saving || len(b.Bank.Boxes) >= gbBankMax {
		return
	}
	b.Bank.addBoxes(1)
	b.BankBox = len(b.Bank.Boxes) - 1
	b.Rev++
}

func (b *GBBankSession) undo() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.Saving {
		return
	}
	b.Cart = copyGBBoxes(b.orig)
	b.Bank = b.origBank.clone()
	if b.BankBox >= len(b.Bank.Boxes) {
		b.BankBox = len(b.Bank.Boxes) - 1
	}
	b.Held, b.Sel, b.Note = nil, nil, "Your changes were undone."
	b.Multi, b.Marked = map[gbRef]bool{}, map[gbRef]bool{}
	b.Rev++
}

// ------------------------------------------------------------------------- what changed
func gbBoxIDs(boxes [][]GBMon) map[string]GBMon {
	out := map[string]GBMon{}
	for _, box := range boxes {
		for _, m := range box {
			if m.Present() {
				out[m.Identity()] = m
			}
		}
	}
	return out
}

func gbBankIDs(bank GBBank) map[string]bool {
	out := map[string]bool{}
	for _, box := range bank.Boxes {
		for _, m := range box.Slots {
			if m != nil {
				out[m.Mon.Identity()] = true
			}
		}
	}
	return out
}

func (b *GBBankSession) counts() bankCounts {
	oc, nc := gbBoxIDs(b.orig), gbBoxIDs(b.Cart)
	ob, nb := gbBankIDs(b.origBank), gbBankIDs(b.Bank)
	var c bankCounts
	for id := range oc {
		if _, still := nc[id]; !still {
			if nb[id] {
				c.ToBank++
			} else {
				c.Released++
			}
		}
	}
	for id := range ob {
		if _, there := nc[id]; there {
			c.ToCart++
		} else if !nb[id] {
			c.Released++
		}
	}
	c.Any = b.cartChanged() || b.bankChanged()
	return c
}

func (b *GBBankSession) dirty() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cartChanged() || b.bankChanged()
}

func (b *GBBankSession) cartChanged() bool {
	for x := range b.Cart {
		for i := range b.Cart[x] {
			n, o := b.Cart[x][i], b.orig[x][i]
			if n.Present() != o.Present() || (n.Present() && !bytes.Equal(n.Raw, o.Raw)) {
				return true
			}
		}
	}
	return false
}

func (b *GBBankSession) bankChanged() bool {
	if len(b.Bank.Boxes) != len(b.origBank.Boxes) {
		return true
	}
	for x := range b.Bank.Boxes {
		for i := range b.Bank.Boxes[x].Slots {
			m, o := b.Bank.Boxes[x].Slots[i], b.origBank.Boxes[x].Slots[i]
			if (m == nil) != (o == nil) || (m != nil && !bytes.Equal(m.Mon.Raw, o.Mon.Raw)) {
				return true
			}
		}
	}
	return false
}

func (b *GBBankSession) moves() (dep, wd []GBMon) {
	before, after := gbBoxIDs(b.orig), gbBoxIDs(b.Cart)
	for id, m := range before {
		if _, ok := after[id]; !ok {
			dep = append(dep, m)
		}
	}
	for id, m := range after {
		if _, ok := before[id]; !ok {
			wd = append(wd, m)
		}
	}
	return
}

// ------------------------------------------------------------------------------- saving
func (b *GBBankSession) commit() (end bool, why string) {
	s := b.s
	b.mu.Lock()
	if !b.Ready || b.Saving {
		b.mu.Unlock()
		return false, ""
	}
	cart := copyGBBoxes(b.Cart)
	bank := b.Bank.clone()
	dep, wd := b.moves()
	cartChanged, bankChanged := b.cartChanged(), b.bankChanged()
	cnt := b.counts()
	b.Saving, b.Held = true, nil
	b.Rev++
	b.mu.Unlock()
	finish := func(note string) {
		b.mu.Lock()
		b.Saving, b.Note = false, note
		b.Rev++
		b.mu.Unlock()
		s.ui.SetPhase("play")
		s.ui.update(func(*State) {})
	}
	if !cartChanged && !bankChanged {
		finish("Nothing has changed since the last save.")
		return false, ""
	}
	if !cartChanged {
		if err := saveGBBank(bank); err != nil {
			finish("Couldn't save the bank: " + err.Error())
			return false, ""
		}
		b.mu.Lock()
		b.origBank = bank.clone()
		b.mu.Unlock()
		finish("Saved. (Only the bank's boxes changed: the game's save didn't need writing.)")
		return false, ""
	}
	newSave := b.save.WithBoxes(cart)
	if _, err := parseGBSave(newSave, b.save.Game); err != nil { // never write a save that doesn't open again
		finish("The new save didn't check out (" + err.Error() + "), so nothing was written.")
		return false, ""
	}
	j := &gbJournal{Owner: b.owner, Game: b.Game, File: b.file, Started: time.Now().Format(time.RFC3339)}
	for _, box := range bank.Boxes {
		var slots [gbBoxSlots]*gbJournalMon
		for k, m := range box.Slots {
			if m != nil {
				x := jm(m.Mon)
				slots[k] = &x
			}
		}
		j.Bank = append(j.Bank, slots)
		j.Names = append(j.Names, box.Name)
	}
	for _, m := range dep {
		j.Deposited = append(j.Deposited, jm(m))
	}
	for _, m := range wd {
		j.Withdrawn = append(j.Withdrawn, jm(m))
	}
	if err := saveGBJournal(j); err != nil {
		finish("Couldn't save on this PC (" + err.Error() + "), so nothing was written.")
		return false, ""
	}
	s.ui.SetPhase("finish")
	if b.file != "" {
		return b.commitFile(newSave, bank, cnt, finish)
	}
	s.ui.SetStatus("Saving to the cartridge...", "busy")
	ram := len(b.save.Raw) // the cartridge's whole save memory, as it was read
	// the same cartridge, with the same save as when the boxes were opened
	var now []byte
	for {
		raw, _, why := b.readCartSave()
		if why == "" {
			if len(raw) < ram || !bytes.Equal(raw[:ram], b.save.Raw[:ram]) {
				clearGBJournal()
				return true, "It's a different cartridge, or the game was saved on it since its boxes were opened. Nothing was written, and the bank wasn't changed. Open the bank again to start from the cartridge as it is now."
			}
			now = raw
			break
		}
		if why == "cancelled" || !s.askRetry("Couldn't check the cartridge", why) {
			clearGBJournal()
			finish("Nothing was saved: the cartridge wasn't checked. Your changes are still here; save again when you're ready.")
			return false, ""
		}
	}
	dir := filepath.Join(s.app.backupFolder(), "Save backups")
	backup := uniquePath(filepath.Join(dir, fileSafe(b.GameName)+" - before Pokemon Bank - "+time.Now().Format("2006-01-02 15-04-05")+".sav"))
	if os.MkdirAll(dir, 0o755) != nil || os.WriteFile(backup, now, 0o644) != nil {
		clearGBJournal()
		finish("Couldn't save a copy of the cartridge's save on this PC first, so nothing was written.")
		return false, ""
	}
	s.ui.Log("Cartridge's save copied to %s.", backup)
	for {
		var err error
		s.ui.SetWriting(true)
		if b.raw {
			err = s.gbc.writeSRAMRaw(newSave[:ram], func(d, t int) { s.ui.SetProgress("Writing the boxes to the cartridge", d, t) })
		} else {
			err = s.gbc.restore(newSave[:ram], func(d, t int) { s.ui.SetProgress("Writing the boxes to the cartridge", d, t) })
			if err == nil {
				var back []byte
				back, err = s.gbc.dump(true, func(d, t int) { s.ui.SetProgress("Reading it back to check it", d, t) })
				if err == nil && !bytes.Equal(back, newSave[:ram]) {
					err = linkErr("The cartridge's save didn't match what was written. Clean the cartridge's contacts.")
				}
			}
		}
		s.ui.SetWriting(false)
		s.ui.ClearProgress()
		if err == nil {
			break
		}
		if !s.askRetry("Couldn't save to the cartridge", err.Error()) {
			return true, "The boxes weren't written to the cartridge completely, so the bank on this PC wasn't changed yet. No Pokémon are lost: " +
				"open the Game Boy bank again with this cartridge and it finishes the change. The cartridge's save from before is in " + backup + "."
		}
	}
	return b.afterWrite(newSave, bank, cnt, finish)
}

func (b *GBBankSession) commitFile(newSave []byte, bank GBBank, cnt bankCounts, finish func(string)) (bool, string) {
	s := b.s
	now, err := os.ReadFile(b.file)
	if err != nil || len(now) < gbSaveRAM || !bytes.Equal(now[:gbSaveRAM], b.save.Raw[:gbSaveRAM]) {
		clearGBJournal()
		return true, "The save file changed since its boxes were opened (or it can't be read). Nothing was written, and the bank wasn't changed."
	}
	backup := b.file + ".before-bank-" + time.Now().Format("20060102-150405") + ".bak"
	if os.WriteFile(backup, now, 0o644) != nil {
		clearGBJournal()
		finish("Couldn't save a copy of the save file first, so nothing was written.")
		return false, ""
	}
	if err := writeFileSafely(b.file, newSave); err != nil {
		return true, "The save file couldn't be written (" + err.Error() + "). No Pokémon are lost: open it in the bank again to finish the change."
	}
	if back, err := os.ReadFile(b.file); err != nil || !bytes.Equal(back, newSave) {
		return true, "The save file didn't read back as written. No Pokémon are lost: open it in the bank again to finish the change. The file from before is " + backup + "."
	}
	s.ui.Log("Save file written: %s (the one from before: %s).", b.file, backup)
	return b.afterWrite(newSave, bank, cnt, finish)
}

func (b *GBBankSession) afterWrite(newSave []byte, bank GBBank, cnt bankCounts, finish func(string)) (bool, string) {
	s := b.s
	if err := saveGBBank(bank); err != nil {
		return true, "The game's save was written, but the bank on this PC couldn't be saved (" + err.Error() + "). No Pokémon are lost: open the bank again with this game and it finishes the change."
	}
	clearGBJournal()
	ns, err := parseGBSave(newSave, b.save.Game)
	if err != nil {
		return true, "Saved, but the new save couldn't be opened again: " + err.Error()
	}
	b.mu.Lock()
	b.save = ns
	b.orig = copyGBBoxes(ns.Boxes)
	b.Cart = copyGBBoxes(ns.Boxes)
	b.origBank, b.Bank = bank.clone(), bank
	b.mu.Unlock()
	s.ui.Log("Saved: %d Pokémon to the bank, %d to the game, %d released.", cnt.ToBank, cnt.ToCart, cnt.Released)
	where := "cartridge"
	if b.file != "" {
		where = "save file (copy it back to your flash cart's SD card)"
	}
	note := fmt.Sprintf("Saved: %d Pokémon went to the bank and %d to the game", cnt.ToBank, cnt.ToCart)
	if cnt.Released > 0 {
		note += fmt.Sprintf(", and %d were released", cnt.Released)
	}
	finish(note + ". The " + where + " was checked after writing.")
	return false, ""
}

// ------------------------------------------------------------------------- for the window
type gbBankView struct {
	Ready, Saving, Loading      bool
	CartLoaded, Locked          bool
	Mode                        string
	GameName, Trainer, File     string
	TID, Gen                    int
	CartBox, BankBox, BankBoxes int
	CartBoxes                   int
	CartName, BankName          string
	Cart, BankSlots             [gbMaxSlots]*GBMon
	CartPick, BankPick          [gbMaxSlots]bool
	CartSlots                   int
	Held, Sel                   *gbRef
	SelMon                      *GBMon
	SelMeta                     *GBBankMon
	Note                        string
	Counts                      bankCounts
	Picked                      int
	InBank, PartyCount          int
}

func (b *GBBankSession) view() gbBankView {
	b.mu.Lock()
	defer b.mu.Unlock()
	v := gbBankView{Ready: b.Ready, Saving: b.Saving, Loading: b.Loading, CartLoaded: b.CartLoaded, Locked: b.Locked, Mode: b.Mode,
		GameName: b.GameName, Trainer: b.Trainer, TID: b.TID, Gen: b.gen(), CartBox: b.CartBox, BankBox: b.BankBox,
		BankBoxes: len(b.Bank.Boxes), CartBoxes: b.boxCount(), Note: b.Note, PartyCount: b.PartyCount, File: b.file, CartSlots: b.slots(false)}
	if !b.Ready {
		return v
	}
	if b.CartLoaded && b.CartBox < len(b.BoxNames) {
		v.CartName = b.BoxNames[b.CartBox]
	}
	if b.BankBox < len(b.Bank.Boxes) {
		v.BankName = b.Bank.Boxes[b.BankBox].Name
	}
	set := b.Multi
	if b.Mode == modeRelease {
		set = b.Marked
	}
	for i := 0; i < b.slots(false); i++ {
		c := gbRef{Box: b.CartBox, Slot: i}
		v.Cart[i] = b.get(c)
		v.CartPick[i] = set[c] && v.Cart[i] != nil
	}
	for i := 0; i < gbBoxSlots; i++ {
		k := gbRef{Bank: true, Box: b.BankBox, Slot: i}
		v.BankSlots[i] = b.get(k)
		v.BankPick[i] = set[k] && v.BankSlots[i] != nil
	}
	v.Picked = len(b.picked(set))
	if b.Held != nil {
		h := *b.Held
		v.Held = &h
	}
	if b.Sel != nil {
		sl := *b.Sel
		v.Sel = &sl
		v.SelMon = b.get(sl)
		if m := b.meta(sl); m != nil {
			mm := *m
			v.SelMeta = &mm
		}
	}
	v.Counts = b.counts()
	v.InBank = b.Bank.count()
	return v
}
