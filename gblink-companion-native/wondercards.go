// Wonder Cards for Pokémon Emerald, FireRed and LeafGreen, sent to the game running in mGBA.
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

// wonderCardsFor: the cards for a game key, grouped as on GB-Link's page.
func wonderCardsFor(key string) []WonderCard {
	var out []WonderCard
	for _, c := range wcIndex() {
		if c.Game == key {
			out = append(out, c)
		}
	}
	order := map[string]int{"Pokémon Emerald": 0, "FireRed / LeafGreen": 0, "Project Wonder (Goppier)": 1, "GB-Link Team": 2}
	sort.SliceStable(out, func(i, j int) bool { return order[out[i].Group] < order[out[j].Group] })
	return out
}

// wcCheckROM: can this ROM take Wonder Cards? Returns the game key, or why not.
func wcCheckROM(g *Game) (string, string) {
	key := wcGameKey(g.ROM)
	if key == "" {
		if c := strings.ToUpper(g.Code); strings.HasPrefix(c, "AXV") || strings.HasPrefix(c, "AXP") {
			return "", "Ruby and Sapphire have no Wonder Cards: Mystery Gift came with Emerald, FireRed and LeafGreen."
		}
		return "", "Wonder Cards are for Pokémon Emerald, FireRed and LeafGreen."
	}
	h, ok := hashes.get(g.ROM)
	if !ok {
		var err error
		if h, err = hashFile(g.ROM); err != nil {
			return "", "Couldn't check the ROM (" + err.Error() + ")."
		}
	}
	if st, _ := classify(h.SHA1, g.Code); st != "ok" {
		return "", "Wonder Cards only go to verified ROMs (a good dump in the No-Intro list), and this one isn't."
	}
	return key, ""
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

// sendWonderCard asks the mGBA script to add the card to the game ("add"), use it now ("use"),
// or use it now in place of the card that keeps running ("replace").
func (b *Bridge) sendWonderCard(c WonderCard, how string) error {
	raw, err := wcFS.ReadFile(path.Join("assets/wondercards", c.File))
	if err != nil {
		return err
	}
	card, icon, data, err := wc3Parts(raw)
	if err != nil {
		return err
	}
	b.mu.Lock()
	b.wcNext++
	id := b.wcNext
	if b.wcSent == nil {
		b.wcSent = map[int]wcRequest{}
	}
	b.wcSent[id] = wcRequest{card: c, how: how}
	connected, canWC := len(b.clients) > 0, b.scriptWC
	b.mu.Unlock()
	if !connected {
		delete(b.wcSent, id)
		return fmt.Errorf("mGBA's script isn't connected")
	}
	if !canWC {
		b.mu.Lock()
		delete(b.wcSent, id)
		b.mu.Unlock()
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
	go func() { // no word from the script at all: say so, rather than "Sending…" for ever
		time.Sleep(8 * time.Second)
		b.mu.Lock()
		req, waiting := b.wcSent[id]
		if waiting && !req.heard {
			delete(b.wcSent, id)
			b.wcBusy, b.wcQueue = false, nil
		}
		b.mu.Unlock()
		if waiting && !req.heard {
			b.s.ui.update(func(st *State) {
				st.WCStatus = "No answer from mGBA's script. Load gblink_controller.lua again in mGBA (Tools › Scripting… › File › Load script…), then try again."
			})
		}
	}()
	return nil
}

// useWonderCards uses cards one after another (each waits for the one before to finish).
func (b *Bridge) useWonderCards(cards []WonderCard) error {
	b.mu.Lock()
	b.wcQueue = append(b.wcQueue, cards...)
	busy := b.wcBusy
	b.mu.Unlock()
	if busy {
		return nil
	}
	return b.wcNextUse()
}

func (b *Bridge) wcNextUse() error {
	b.mu.Lock()
	if len(b.wcQueue) == 0 {
		b.wcBusy = false
		b.mu.Unlock()
		return nil
	}
	c := b.wcQueue[0]
	b.wcQueue = b.wcQueue[1:]
	b.wcBusy = true
	b.mu.Unlock()
	if err := b.sendWonderCard(c, "use"); err != nil {
		b.mu.Lock()
		b.wcBusy, b.wcQueue = false, nil
		b.mu.Unlock()
		return err
	}
	return nil
}

type wcRequest struct {
	card  WonderCard
	how   string
	heard bool // the script answered (it may still be waiting for the game)
}

// wcReply: the script's answer "w<id>\t<ok|no|wait>\t<text>".
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
	b.mu.Lock()
	req, ok := b.wcSent[id]
	if f[1] != "wait" {
		delete(b.wcSent, id)
	} else if ok {
		req.heard = true
		b.wcSent[id] = req
	}
	b.mu.Unlock()
	if !ok {
		return
	}
	ui := b.s.ui
	name := req.card.Label
	if req.how != "add" && f[1] != "wait" {
		defer func() {
			if f[1] != "ok" { // stop the rest: they may need this one
				b.mu.Lock()
				b.wcQueue = nil
				b.mu.Unlock()
			}
			b.wcNextUse()
		}()
	}
	switch {
	case f[1] == "wait":
		ui.update(func(st *State) {
			st.WCStatus = "Waiting for you to be free to move in the game, to use " + name + "…"
		})
	case f[1] == "busy": // another card that keeps running is on: offer to replace it
		on := text
		if on == "" {
			on = "Another card that keeps running"
		}
		c := req.card
		ui.update(func(st *State) {
			st.WCStatus = on + " is on until the game is reset, and only one can run at a time. Replace it with " + name + "?"
			st.WCReplace = &c
		})
	case f[1] == "ok" && req.how != "add":
		ui.Log("Used the Wonder Card %s.", name)
		ui.update(func(st *State) { st.WCStatus = "Used " + name + "." })
		ui.Toast("Used " + name)
	case f[1] == "ok":
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

// wcSession: the running game, if it can take Wonder Cards now (else says why).
func (a *App) wcSession() *Session {
	a.mu.Lock()
	s := a.session
	a.mu.Unlock()
	if s == nil || s.bridge == nil {
		a.ui.update(func(st *State) { st.WCStatus = "Start the game first." })
		return nil
	}
	if key, why := wcCheckROM(s.game); key == "" {
		a.ui.update(func(st *State) { st.WCStatus = why })
		return nil
	}
	return s
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
	s := a.wcSession()
	if s == nil {
		return
	}
	if err := s.bridge.useWonderCards(cards); err != nil {
		a.ui.update(func(st *State) { st.WCStatus = "Couldn't send it: " + err.Error() + "." })
	}
}

// replaceWonderCard: the window's "Replace": use the card in place of the one that keeps running.
func (a *App) replaceWonderCard(c WonderCard) {
	a.ui.update(func(st *State) { st.WCReplace, st.WCStatus = nil, "Sending "+c.Label+"…" })
	s := a.wcSession()
	if s == nil {
		return
	}
	s.bridge.mu.Lock()
	busy := s.bridge.wcBusy
	if !busy {
		s.bridge.wcBusy = true
	}
	s.bridge.mu.Unlock()
	if busy {
		a.ui.update(func(st *State) { st.WCStatus = "Another card is being used: wait for it to finish." })
		return
	}
	if err := s.bridge.sendWonderCard(c, "replace"); err != nil {
		s.bridge.mu.Lock()
		s.bridge.wcBusy = false
		s.bridge.mu.Unlock()
		a.ui.update(func(st *State) { st.WCStatus = "Couldn't send it: " + err.Error() + "." })
	}
}

// addWonderCard: the window's "Receive Wondercard" (a game keeps one card).
func (a *App) addWonderCard(c WonderCard) {
	s := a.wcSession()
	if s == nil {
		return
	}
	if err := s.bridge.sendWonderCard(c, "add"); err != nil {
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
	s.linkMu.Lock()
	s.state.WCCards = labels
	s.game.saveState(s.state)
	s.linkMu.Unlock()
}

// wcJapanese: the cards are for a Japanese game (they can only be used now)
func wcJapanese(key string) bool { return len(key) >= 4 && key[3] == 'J' }
