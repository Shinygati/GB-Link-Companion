// Wonder Cards for Pokémon Emerald, FireRed and LeafGreen, sent to the game running in mGBA (or, for
// retail English Emerald, straight to a real GBA: wcgba.go).
//
// The cards are the GB-Link team's (https://github.com/GB-Link/gblink-wondercards, GPL-3.0):
// every card of their page, as .wc3 files for every language's games, in assets/wondercards (one
// folder per game, language and revision: BPEE0, BPEF0, BPRJ1, ...) with index.json. Each is the
// card their page sends to that ROM: the GB-Link Team's own cards are made for each ROM.
//
// Two ways to send one (the mGBA script does the writing, see "Wonder Cards" in the script):
//   - Add it to the game: what Mystery Gift does when it receives a card. The deliveryman on
//     Pokémon Center 2F then hands over the gift, and the card shows under Mystery Gift.
//   - Use it now: the card's script runs straight away, as if the deliveryman handed it over,
//     and the game's own card (if any) stays as it was. One-off gifts stack this way. Cards that
//     keep running until the game is reset (speed, Walk Through Walls...) all use the same free
//     RAM, so only one of those can be on at a time.
//
// Only for ROMs that match No-Intro's good dumps: the cards' code is made for exactly those ROMs.
// The Japanese games' cards are shorter and their Mystery Gift save is laid out otherwise, so
// there a card is only used now (the script runs its RAM script where those games keep it).
package main

import (
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed assets/wondercards
var wcFS embed.FS

type WonderCard struct {
	Game        string `json:"game"` // BPEE0, BPRE1, ...
	Group       string `json:"group"`
	Label       string `json:"label"`
	Description string `json:"description"`
	File        string `json:"file"`
	Resident    bool   `json:"resident"` // keeps running until the game is reset
	Path        string `json:"-"`        // one of the player's own cards (wccustom.go): the file on disk
	Japanese    bool   `json:"-"`        // ...made for the Japanese games
}

var wcIndex = sync.OnceValue(func() []WonderCard {
	var ix []WonderCard
	if b, err := wcFS.ReadFile("assets/wondercards/index.json"); err == nil {
		json.Unmarshal(b, &ix)
	}
	return ix
})

// wcGameKey: the cards' folder for a ROM's header (code + revision), or "".
func wcGameKey(rom string) string {
	f, err := os.Open(rom)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := make([]byte, 0xC0)
	if _, err := f.ReadAt(h, 0); err != nil {
		return ""
	}
	key := fmt.Sprintf("%s%d", h[0xAC:0xB0], h[0xBC])
	for _, c := range wcIndex() {
		if c.Game == key {
			return key
		}
	}
	return ""
}

// wonderCardsFor: the cards for a game key, grouped as on GB-Link's page, then the player's own.
// ownOnly: a ROM hack, which takes only the player's own cards.
func wonderCardsFor(key string, ownOnly bool) []WonderCard {
	var out []WonderCard
	for _, c := range wcIndex() {
		if c.Game == key && !ownOnly {
			out = append(out, c)
		}
	}
	order := map[string]int{"Pokémon Emerald": 0, "FireRed / LeafGreen": 0, "Project Wonder (Goppier)": 1, "GB-Link Team": 2}
	sort.SliceStable(out, func(i, j int) bool { return order[out[i].Group] < order[out[j].Group] })
	if key == "" {
		return out
	}
	// the player's own cards (wccustom.go), for every game of their kind, after the GB-Link team's
	taken := map[string]bool{}
	for _, c := range out {
		taken[c.Label] = true
	}
	for _, c := range customWonderCards() {
		if c.Japanese != wcJapanese(key) {
			continue
		}
		c.Game = key
		for taken[c.Label] {
			c.Label += " (yours)"
		}
		taken[c.Label] = true
		out = append(out, c)
	}
	return out
}

// wcCheckROM: can this ROM take Wonder Cards? Returns the game key, or why not. hack: a ROM hack
// with a .sym that fits, which takes only the player's own cards (wcsym.go).
func wcCheckROM(g *Game) (key, why string, hack bool) {
	key = wcGameKey(g.ROM)
	if key == "" {
		if h := hackWonderCards(g.ROM, g.Code); h.key != "" { // a hack with a game code of its own
			return h.key, "", true
		}
		if c := strings.ToUpper(g.Code); strings.HasPrefix(c, "AXV") || strings.HasPrefix(c, "AXP") {
			return "", "Ruby and Sapphire have no Wonder Cards: Mystery Gift came with Emerald, FireRed and LeafGreen.", false
		}
		return "", "Wonder Cards are for Pokémon Emerald, FireRed and LeafGreen.", false
	}
	h, ok := hashes.get(g.ROM)
	if !ok {
		var err error
		if h, err = hashFile(g.ROM); err != nil {
			return "", "Couldn't check the ROM (" + err.Error() + ").", false
		}
	}
	if st, _ := classify(h.SHA1, g.Code); st != "ok" {
		hw := hackWonderCards(g.ROM, g.Code)
		if hw.key != "" {
			return hw.key, "", true
		}
		return "", hw.why, false
	}
	return key, "", false
}

// wc3Parts: a .wc3's card (332 bytes; the Japanese games' 164), icon species (2) and RAM script
// data (1000).
func wc3Parts(b []byte) (card, icon, data []byte, err error) {
	switch len(b) {
	case 0x58C:
		card, icon, data = b[4:4+332], b[0x15A:0x15C], b[0x1A4:0x1A4+1000]
	case 0x4E4: // Japanese
		card, icon, data = b[4:4+164], b[0xB2:0xB4], b[0xFC:0xFC+1000]
	default:
		return nil, nil, nil, fmt.Errorf("not a .wc3 file (%d bytes)", len(b))
	}
	flagID := int(card[0]) | int(card[1])<<8
	typ, bg, send := card[8]&3, (card[8]>>2)&15, card[8]>>6
	if flagID == 0 || typ >= 3 || send >= 3 || bg >= 8 || card[9] > 7 { // the games' ValidateWonderCard
		return nil, nil, nil, fmt.Errorf("this .wc3 holds no Wonder Card the games would accept")
	}
	return card, icon, data, nil
}

// wcSender is what a card is sent with: the mGBA script (Bridge), or the payload running inside
// Emerald on a real GBA (wcgba.go). The queue, the answers and what the player sees are the same.
type wcSender struct {
	mu       sync.Mutex
	ui       *UI
	next     int
	sent     map[int]wcRequest
	queue    []WonderCard // cards waiting to be used, one after another
	busy     bool         // a card is being used now
	resident string       // the card that keeps running, as far as this app knows
	// transmit sends one card; how: "add", "use" or "replace". It reports back through reply().
	transmit func(id int, c WonderCard, how string, card, icon, data []byte) error
	// noAnswer: what to tell the player when nothing at all comes back
	noAnswer string
}

// sendWonderCard adds the card to the game ("add"), uses it now ("use"), or uses it now in place of
// the card that keeps running ("replace").
func (w *wcSender) sendWonderCard(c WonderCard, how string) error {
	var raw []byte
	var err error
	if c.Path != "" { // one of the player's own cards
		raw, err = os.ReadFile(c.Path)
	} else {
		raw, err = wcFS.ReadFile(path.Join("assets/wondercards", c.File))
	}
	if err != nil {
		return err
	}
	card, icon, data, err := wc3Parts(raw)
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.next++
	id := w.next
	if w.sent == nil {
		w.sent = map[int]wcRequest{}
	}
	w.sent[id] = wcRequest{card: c, how: how}
	w.mu.Unlock()
	if err := w.transmit(id, c, how, card, icon, data); err != nil {
		w.mu.Lock()
		delete(w.sent, id)
		w.mu.Unlock()
		return err
	}
	go func() { // no word at all: say so, rather than "Sending…" for ever
		time.Sleep(8 * time.Second)
		w.mu.Lock()
		req, waiting := w.sent[id]
		if waiting && !req.heard {
			delete(w.sent, id)
			w.busy, w.queue = false, nil
		}
		w.mu.Unlock()
		if waiting && !req.heard {
			w.ui.update(func(st *State) { st.WCStatus = w.noAnswer })
		}
	}()
	return nil
}

// useWonderCards uses cards one after another (each waits for the one before to finish).
func (w *wcSender) useWonderCards(cards []WonderCard) error {
	w.mu.Lock()
	w.queue = append(w.queue, cards...)
	busy := w.busy
	w.mu.Unlock()
	if busy {
		return nil
	}
	return w.nextUse()
}

func (w *wcSender) nextUse() error {
	w.mu.Lock()
	if len(w.queue) == 0 {
		w.busy = false
		w.mu.Unlock()
		return nil
	}
	c := w.queue[0]
	w.queue = w.queue[1:]
	w.busy = true
	w.mu.Unlock()
	if err := w.sendWonderCard(c, "use"); err != nil {
		w.mu.Lock()
		w.busy, w.queue = false, nil
		w.mu.Unlock()
		return err
	}
	return nil
}

// replace uses the card in place of the one that keeps running.
func (w *wcSender) replace(c WonderCard) error {
	w.mu.Lock()
	busy := w.busy
	if !busy {
		w.busy = true
	}
	w.mu.Unlock()
	if busy {
		return fmt.Errorf("another card is being used: wait for it to finish")
	}
	if err := w.sendWonderCard(c, "replace"); err != nil {
		w.mu.Lock()
		w.busy = false
		w.mu.Unlock()
		return err
	}
	return nil
}

type wcRequest struct {
	card  WonderCard
	how   string
	heard bool // an answer came (the card may still be waiting for the game)
}

// reply: an answer for card id: "ok", "no" (text says why), "wait" (waiting for the player to be
// free to move) or "busy" (another card that keeps running is on; text names it if known).
func (w *wcSender) reply(id int, status, text string) {
	w.mu.Lock()
	req, ok := w.sent[id]
	if status != "wait" {
		delete(w.sent, id)
	} else if ok {
		req.heard = true
		w.sent[id] = req
	}
	if ok && status == "ok" && req.how != "add" && req.card.Resident {
		w.resident = req.card.Label
	}
	w.mu.Unlock()
	if !ok {
		return
	}
	ui := w.ui
	name := req.card.Label
	if req.how != "add" && status != "wait" {
		defer func() {
			if status != "ok" { // stop the rest: they may need this one
				w.mu.Lock()
				w.queue = nil
				w.mu.Unlock()
			}
			w.nextUse()
		}()
	}
	switch {
	case status == "wait":
		ui.update(func(st *State) {
			st.WCStatus = "Waiting for you to be free to move in the game, to use " + name + "…"
		})
	case status == "busy": // another card that keeps running is on: offer to replace it
		on := text
		if on == "" {
			on = "Another card that keeps running"
		}
		c := req.card
		ui.update(func(st *State) {
			st.WCStatus = on + " is on until the game is reset, and only one can run at a time. Replace it with " + name + "?"
			st.WCReplace = &c
		})
	case status == "ok" && req.how != "add":
		ui.Log("Used the Wonder Card %s.", name)
		ui.update(func(st *State) { st.WCStatus = "Used " + name + "." })
		ui.Toast("Used " + name)
	case status == "ok":
		ui.Log("Received the Wonder Card %s. Talk to the deliveryman on Pokémon Center 2F (go in again if you're there), and save in the game to keep it.", name)
		ui.update(func(st *State) {
			st.WCStatus = "Received " + name + ". Talk to the deliveryman in green on any Pokémon Center's 2F, and save in the game to keep the card."
		})
		ui.Toast("Received " + name)
	default:
		ui.Log("Wonder Card %s: %s", name, text)
		ui.update(func(st *State) { st.WCStatus = text })
	}
}

// mGBA: the script's line "W<A|U><id>,<card hex>,<icon hex>,<script data hex>,<resident 0/1/2>,<label>"
func (b *Bridge) wcTransmit(id int, c WonderCard, how string, card, icon, data []byte) error {
	b.mu.Lock()
	connected, canWC := len(b.clients) > 0, b.scriptWC
	b.mu.Unlock()
	if !connected {
		return fmt.Errorf("mGBA's script isn't connected")
	}
	if !canWC {
		return fmt.Errorf("mGBA is running an older copy of the script, without Wonder Cards. In mGBA, load gblink_controller.lua again " +
			"(Tools › Scripting… › File › Load script…) from where this app shows it, or close mGBA and press Play again")
	}
	mode, res := "A", "0"
	if how == "use" || how == "replace" {
		mode = "U"
	}
	if c.Resident {
		res = "1"
		if how == "replace" {
			res = "2"
		}
	}
	label := strings.NewReplacer("\n", " ", "\t", " ", ",", " ").Replace(c.Label)
	b.writeAll([]byte(fmt.Sprintf("W%s%d,%s,%s,%s,%s,%s\n", mode, id, hex.EncodeToString(card), hex.EncodeToString(icon),
		hex.EncodeToString(data), res, label)))
	return nil
}

// wcReply: the script's answer "w<id>\t<ok|no|wait|busy>\t<text>".
func (b *Bridge) wcReply(line string) {
	f := strings.SplitN(line[1:], "\t", 3)
	if len(f) < 2 {
		return
	}
	var id int
	fmt.Sscan(f[0], &id)
	text := ""
	if len(f) == 3 {
		text = f[2]
	}
	b.wc.reply(id, f[1], text)
}

// wcTarget: what the running game's cards go to (mGBA's script or the GBA), if it can take
// Wonder Cards now (else says why).
func (a *App) wcTarget() *wcSender {
	a.mu.Lock()
	s := a.session
	a.mu.Unlock()
	var w *wcSender
	if s != nil && s.gbawc != nil {
		w = &s.gbawc.wc
	} else if s != nil && s.bridge != nil {
		if key, why, _ := wcCheckROM(s.game); key == "" {
			a.ui.update(func(st *State) { st.WCStatus = why })
			return nil
		}
		w = &s.bridge.wc
	}
	if w == nil {
		a.ui.update(func(st *State) { st.WCStatus = "Start the game first." })
	}
	return w
}

// useWonderCards: the window's "Use now", for the ticked cards.
func (a *App) useWonderCards(cards []WonderCard) {
	resident := 0
	for _, c := range cards {
		if c.Resident {
			resident++
		}
	}
	if resident > 1 {
		a.ui.update(func(st *State) {
			st.WCStatus = "Only one card marked “until reset” can be on at a time (they use the same memory). Untick the others."
		})
		return
	}
	w := a.wcTarget()
	if w == nil {
		return
	}
	if err := w.useWonderCards(cards); err != nil {
		a.ui.update(func(st *State) { st.WCStatus = "Couldn't send it: " + err.Error() + "." })
	}
}

// replaceWonderCard: the window's "Replace": use the card in place of the one that keeps running.
func (a *App) replaceWonderCard(c WonderCard) {
	a.ui.update(func(st *State) { st.WCReplace, st.WCStatus = nil, "Sending "+c.Label+"…" })
	w := a.wcTarget()
	if w == nil {
		return
	}
	if err := w.replace(c); err != nil {
		a.ui.update(func(st *State) { st.WCStatus = "Couldn't send it: " + err.Error() + "." })
	}
}

// addWonderCard: the window's "Receive Wondercard" (a game keeps one card).
func (a *App) addWonderCard(c WonderCard) {
	w := a.wcTarget()
	if w == nil {
		return
	}
	if err := w.sendWonderCard(c, "add"); err != nil {
		a.ui.update(func(st *State) { st.WCStatus = "Couldn't send it: " + err.Error() + "." })
	}
}

// chooseWonderCards remembers the ticked cards for this game (in its .gblink.json).
func (a *App) chooseWonderCards(labels []string) {
	a.ui.update(func(st *State) { st.WCChosen = labels })
	a.mu.Lock()
	s := a.session
	a.mu.Unlock()
	if s == nil {
		return
	}
	if s.game == nil { // a GBA's own Emerald: nothing on this PC to remember it in
		return
	}
	s.linkMu.Lock()
	s.state.WCCards = labels
	s.game.saveState(s.state)
	s.linkMu.Unlock()
}

// wcGame: Pokémon Emerald, FireRed or LeafGreen (the games with Wonder Cards), by game code
func wcGame(code string) bool {
	c := strings.ToUpper(code)
	return strings.HasPrefix(c, "BPE") || strings.HasPrefix(c, "BPR") || strings.HasPrefix(c, "BPG")
}

// wcJapanese: the cards are for a Japanese game (they can only be used now)
func wcJapanese(key string) bool { return len(key) >= 4 && key[3] == 'J' }
