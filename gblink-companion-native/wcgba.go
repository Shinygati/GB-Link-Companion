// Wonder Cards on a real GBA: retail English Pokémon Emerald, through GB-Link, without mGBA.
//
// The GBA is switched on holding START + SELECT with Emerald in it, and gets a small multiboot
// program (assets/emerald_wc_mb.gba, built from b2s/emerald_wc). It checks the cartridge, then
// starts Emerald as usual with a little program of its own inside the game (its save isn't
// touched). While you walk around, that program listens on the link port: this app sends it a card
// there, and it adds the card to the game or uses it right where you stand, as the mGBA script
// does (wondercards.go has the rest: the cards, the queue, the answers).
//
// The way into the game comes from gen3-poke-multiplayer by Wired-Ariel (GPL-3.0).
//
// Link protocol (b2s/emerald_wc/payload.c has the GBA's side): this app is the clock; every word
// it sends gets back the word the GBA had ready.
//
//	app -> GBA  0x00000000                        a poll
//	            0x5743014F, 335 words, checksum    a card
//	GBA -> app  0xA5 state id code                 status (state 2: you can move; id/code: the last card)
//	            0xB0 why ....                      the multiboot program won't start this cartridge
package main

import (
	_ "embed"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

//go:embed assets/emerald_wc_mb.gba
var emeraldWCImage []byte

const (
	wcGBAKey    = "BPEE0" // the cards made for retail English Emerald
	wcGBAGap    = 250     // us between words in the game (the GBA answers in an interrupt)
	wcBodyWords = 335

	wcCodeWaiting, wcCodeRunning                = 1, 2
	wcCodeAdded, wcCodeUsed, wcCodeResidentOn   = 0x10, 0x11, 0x12
	wcErrNoGame, wcErrNoScript, wcErrJobBusy    = 0x21, 0x22, 0x23
	wcErrTimeout, wcErrResidentUnknown, wcErrCk = 0x24, 0x25, 0x26
)

type gbaWC struct {
	s    *Session
	wc   wcSender
	mu   sync.Mutex
	out  []gbaPacket      // cards waiting to go over the cable
	jobs map[byte]*gbaJob // what each packet id is
	pid  byte
}

type gbaPacket struct {
	pid   byte
	words []uint32
}

type gbaJob struct {
	id     int    // the sender's id (wondercards.go)
	how    string // add, use, replace
	waited bool   // told the player it waits for them to be free
}

// gbaWonderCards starts Wonder Cards on a real GBA (the game list's button).
func (a *App) gbaWonderCards() error {
	a.mu.Lock()
	if a.session != nil {
		a.mu.Unlock()
		return fmt.Errorf("Something is already running.")
	}
	s := &Session{ui: a.ui, app: a, cancel: make(chan struct{})}
	g := &gbaWC{s: s, jobs: map[byte]*gbaJob{}}
	g.wc = wcSender{ui: a.ui, transmit: g.transmit,
		noAnswer: "No answer from the GBA. Make sure Emerald is running and you're walking around in the game, with the link cable in."}
	s.gbawc = g
	a.session = s
	a.mu.Unlock()
	a.ui.update(func(st *State) {
		st.Game = &GameView{Name: "Pokémon Emerald", Title: "POKEMON EMER", Code: "BPEE"}
		st.Phase, st.Mode, st.Status, st.Progress, st.Prompt = "connect", "gbawc", "", nil, nil
		st.Cart, st.LastSync, st.GBA, st.Result, st.ResultText, st.BackupPath = "", "", "", "", "", ""
		st.Verify = nil
		st.Controller = ControllerView{}
		st.Log = nil
		st.WCGame, st.WCWhy, st.WCStatus, st.WCChosen, st.WCReplace = wcGBAKey, "", "", nil, nil
		st.GBAWC = ""
	})
	go g.run()
	return nil
}

// stopGBAWonderCards is the Done button: the game keeps running on the GBA.
func (a *App) stopGBAWonderCards() {
	a.mu.Lock()
	s := a.session
	a.mu.Unlock()
	if s != nil && s.gbawc != nil {
		select {
		case <-s.cancel:
		default:
			close(s.cancel)
		}
	}
}

func (g *gbaWC) run() {
	s := g.s
	defer func() {
		if s.link != nil {
			go s.link.Close() // (in the background: it can hang if the adapter was just unplugged)
		}
	}()
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
	for {
		id := s.ui.ShowPrompt(Prompt{Kind: "power", Title: "Start Emerald on your GBA",
			Text: "With Pokémon Emerald in the GBA, hold START and SELECT while you switch it on. " +
				"If it's already on, switch it off first. Emerald then starts as usual: your save isn't changed. " +
				"(Already started it this way? Just walk around in the game: this picks it up.)",
			Buttons: []Button{{ID: "cancel", Label: "Cancel", Style: "secondary"}}})
		err := s.link.Multiboot(emeraldWCImage, 365*24*time.Hour, func() bool {
			_, ok := s.ui.Answer(id)
			return ok || s.cancelled() || s.stopped.Load()
		})
		s.ui.ClearPrompt(id)
		if err == nil {
			break
		}
		if errors.Is(err, errEmeraldWCRunning) { // started earlier: carry on with it
			s.ui.Log("Emerald is already running with Wonder Cards on the GBA.")
			break
		}
		if le, ok := err.(*LinkError); (ok && le.msg == "Cancelled") || s.cancelled() || s.stopped.Load() {
			s.ui.Finish("cancelled", "")
			return
		}
		if g.adapterGone() { // unplugged while waiting for the GBA: wait for it, then ask again
			if !g.waitForAdapter() {
				s.ui.Finish("cancelled", "")
				return
			}
			continue
		}
		if !s.askRetry("The GBA didn't connect", err.Error()+" Check the link cable is pushed in at both ends, then try again.") {
			s.ui.Finish("cancelled", "")
			return
		}
	}
	s.link.SetTiming(wcGBAGap, 4)
	s.ui.Log("Emerald started on the GBA with Wonder Cards.")
	s.ui.SetPhase("play")
	g.setState("starting")
	if why := g.loop(); why != "" {
		s.ui.Finish("failed", why)
		return
	}
	s.ui.Finish("gbawc", "Emerald keeps running on your GBA. Save in the game to keep what the cards gave you.")
}

// loop polls the GBA and sends cards until Done. Returns why it stopped, if it's a problem.
func (g *gbaWC) loop() string {
	s := g.s
	heard := false
	quiet := 0
	for !s.cancelled() && !s.stopped.Load() {
		g.mu.Lock()
		var p *gbaPacket
		if len(g.out) > 0 {
			p = &g.out[0]
		}
		g.mu.Unlock()
		if p != nil {
			g.send(*p)
			g.mu.Lock()
			g.out = g.out[1:]
			g.mu.Unlock()
			continue
		}
		if quiet > 0 && g.adapterGone() { // not hearing the GBA because the adapter was unplugged?
			if !g.reconnect() {
				return ""
			}
			continue
		}
		r, err := s.link.Words([]uint32{0, 0}, 2*time.Second)
		if err != nil { // the adapter was unplugged: wait for it, the game keeps running on the GBA
			if !g.reconnect() {
				return ""
			}
			quiet = 1
			continue
		}
		w := r[1]
		switch {
		case w>>24 == 0xB0:
			return g.refused(w)
		case w>>24 == 0xA5:
			heard, quiet = true, 0
			g.status(w)
		default:
			quiet++
			if quiet == 1 || quiet == 10 { // for the Details: what the GBA answers instead of its status
				s.ui.Log("The GBA isn't answering as expected (%08X %08X).", r[0], r[1])
			}
			if !heard {
				g.setState("starting")
			} else if quiet > 2 {
				g.setState("away")
			}
		}
		// Not hearing the GBA: poll slowly. After half a second with no word, the GBA starts its
		// port over from the first bit (payload.c), which is what puts it back in step when the
		// link cable was pulled out in the middle of a word.
		wait := 250 * time.Millisecond
		if quiet > 0 {
			wait = 1200 * time.Millisecond
		}
		select {
		case <-s.cancel:
		case <-time.After(wait):
		}
	}
	return ""
}

// adapterGone: the GB-Link adapter was unplugged, isn't answering, or came back as another port.
func (g *gbaWC) adapterGone() bool {
	s := g.s
	if s.link == nil || s.link.Broken() {
		return true
	}
	if os.Getenv("GBLINK_PORT") != "" { // tests
		return false
	}
	p := FindGBLinkPort()
	return p == "" || p != s.link.name
}

// dropLink lets go of the adapter's old connection (closing it in the background: on Windows that
// can hang when the adapter has been unplugged).
func (g *gbaWC) dropLink() {
	s := g.s
	if l := s.link; l != nil {
		go l.Close()
	}
	s.link, s.helper = nil, nil
}

// waitForAdapter, while starting: asks for the adapter to be plugged in again and connects to it
// (false: Cancel).
func (g *gbaWC) waitForAdapter() bool {
	s := g.s
	g.dropLink()
	s.ui.Log("The GB-Link adapter was unplugged: waiting for it.")
	id := s.ui.ShowPrompt(Prompt{Kind: "error", Title: "GB-Link unplugged",
		Text:    "Plug the GB-Link adapter back in: this carries on by itself.",
		Buttons: []Button{{ID: "cancel", Label: "Cancel", Style: "secondary"}}})
	defer s.ui.ClearPrompt(id)
	for !s.cancelled() && !s.stopped.Load() {
		if _, ok := s.ui.Answer(id); ok {
			return false
		}
		if s.connect() == nil {
			s.ui.Log("GB-Link plugged in again.")
			return true
		}
		select {
		case <-s.cancel:
			return false
		case <-time.After(time.Second):
		}
	}
	return false
}

// reconnect waits for the GB-Link adapter to be plugged in again (false: Done was pressed).
func (g *gbaWC) reconnect() bool {
	s := g.s
	g.dropLink()
	g.setState("nolink")
	s.ui.Log("The GB-Link adapter stopped answering: waiting for it to be plugged in again.")
	for !s.cancelled() && !s.stopped.Load() {
		select {
		case <-s.cancel:
			return false
		case <-time.After(2 * time.Second):
		}
		if s.connect() == nil {
			s.link.SetMode(modeGBLink) // a freshly plugged-in adapter starts in its default mode
			s.link.SetTiming(wcGBAGap, 4)
			g.setState("away")
			return true
		}
	}
	return false
}

// refused: the multiboot program's answer when the cartridge isn't retail English Emerald.
func (g *gbaWC) refused(w uint32) string {
	code := string([]byte{byte(w), byte(w >> 8)})
	switch (w >> 16) & 0xFF {
	case 1:
		return "That cartridge isn't Pokémon Emerald in English (it starts with \"" + code + "\"). Wonder Cards on the GBA work with retail English Emerald. Switch the GBA off and back on to play."
	case 2:
		return "That Emerald cartridge isn't the retail English one (it's a modified or reproduction game). Wonder Cards on the GBA only work with retail English Emerald: use them in mGBA for other versions."
	}
	return "The GBA couldn't start Emerald with Wonder Cards. Switch it off, then try again."
}

func (g *gbaWC) setState(v string) {
	g.s.ui.update(func(st *State) { st.GBAWC = v })
}

// status: the GBA's status word: how the game is, and what happened to the last card.
func (g *gbaWC) status(w uint32) {
	switch (w >> 16) & 0xFF {
	case 2:
		g.setState("ready")
	default:
		g.setState("busy")
	}
	pid, code := byte(w>>8), int(w&0xFF)
	g.mu.Lock()
	job := g.jobs[pid]
	g.mu.Unlock()
	if job == nil {
		return
	}
	reply := func(status, text string) {
		g.mu.Lock()
		delete(g.jobs, pid)
		g.mu.Unlock()
		g.wc.reply(job.id, status, text)
	}
	switch code {
	case wcCodeWaiting, wcCodeRunning:
		if job.how != "add" && !job.waited {
			job.waited = true
			g.wc.reply(job.id, "wait", "")
		}
	case wcCodeAdded, wcCodeUsed:
		reply("ok", "")
	case wcCodeResidentOn:
		g.wc.mu.Lock()
		on := g.wc.resident
		g.wc.mu.Unlock()
		reply("busy", on)
	case wcErrNoGame:
		reply("no", "Load your game first (continue from the title screen), then walk around.")
	case wcErrNoScript:
		reply("no", "This card has no script, so there's nothing to use.")
	case wcErrJobBusy:
		reply("no", "Another card is being used: wait for it to finish.")
	case wcErrTimeout:
		reply("no", "The game was busy for 3 minutes: close any menu and walk around in the game, then try again.")
	case wcErrResidentUnknown:
		reply("no", "A card that keeps running is on, and the GBA doesn't know which. Reset the game (switch the GBA off) to change it.")
	case wcErrCk:
		// sent again by send()
	default:
		reply("no", "The GBA couldn't take this card.")
	}
}

// transmit (wondercards.go) queues a card for the cable.
func (g *gbaWC) transmit(id int, c WonderCard, how string, card, icon, data []byte) error {
	if len(card) != 332 || len(icon) != 2 || len(data) != 1000 {
		return fmt.Errorf("this card isn't made for English Emerald")
	}
	res := byte(0)
	if c.Resident {
		res = 1
		if how == "replace" {
			res = 2
		}
	}
	op := byte('A')
	if how != "add" {
		op = 'U'
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.pid++
	if g.pid == 0 || g.pid == 0xFF { // 0: nothing sent yet; 0xFF: the GBA's "none"
		g.pid = 1
	}
	pid := g.pid
	body := make([]byte, 0, 4*wcBodyWords)
	body = append(body, 0, res, pid, op)
	body = append(body, card...)
	body = append(body, icon...)
	body = append(body, 0, 0)
	body = append(body, data...)
	words := []uint32{0x57430000 | wcBodyWords}
	ck := uint32(0x5743)
	for i := 0; i < wcBodyWords; i++ {
		v := binary.LittleEndian.Uint32(body[4*i:])
		words = append(words, v)
		ck = (ck<<1 | ck>>31) ^ v
	}
	words = append(words, ck)
	g.jobs[pid] = &gbaJob{id: id, how: how}
	g.out = append(g.out, gbaPacket{pid: pid, words: words})
	return nil
}

// send puts a card over the cable, again if a word got lost on the way.
func (g *gbaWC) send(p gbaPacket) {
	s := g.s
	for try := 0; try < 6 && !s.cancelled(); try++ {
		if _, err := s.link.Words(p.words, 2*time.Second); err != nil {
			break
		}
		for k := 0; k < 12; k++ { // its answer: this card's id, unless the checksum didn't match
			time.Sleep(25 * time.Millisecond)
			r, err := s.link.Words([]uint32{0, 0}, 2*time.Second)
			if err != nil {
				break
			}
			if w := r[1]; w>>24 == 0xA5 && byte(w>>8) == p.pid {
				if w&0xFF == wcErrCk {
					break
				}
				g.status(w)
				return
			}
		}
		time.Sleep(800 * time.Millisecond) // quiet: the GBA drops a packet cut short, and gets back in step
	}
	g.mu.Lock()
	job := g.jobs[p.pid]
	delete(g.jobs, p.pid)
	g.mu.Unlock()
	if job != nil {
		g.wc.reply(job.id, "no", "The GBA didn't get the card. Walk around in the game (not in a battle or a menu), keep the link cable in, then try again.")
	}
}
