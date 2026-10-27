// Your own Wonder Cards: .wc3 files the player adds (Settings › Pokémon Enhancements, or the
// Wonder Cards list's "Add your own…"). They're copied into the settings folder
// (<config>/wondercards) and show under "Your cards" in the list, next to the GB-Link team's.
//
// A .wc3 doesn't say which game it's for, so a card from the player is offered for every game of
// its kind: the English/European games' cards (0x58C bytes) for those games, the Japanese games'
// (0x4E4 bytes) for the Japanese ones. A card's script is made for one game and language, so the
// list says to use only cards made for the game being played.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const customWCGroup = "Your cards"

var customWC struct {
	mu     sync.Mutex
	loaded bool
	cards  []WonderCard
}

// customWCDir: where the player's own cards are kept.
func customWCDir() string {
	d := filepath.Join(configDir(), "wondercards")
	os.MkdirAll(d, 0o755)
	return d
}

// customWonderCards: the player's own cards (read from the folder once, then after each change).
func customWonderCards() []WonderCard {
	customWC.mu.Lock()
	defer customWC.mu.Unlock()
	if !customWC.loaded {
		customWC.cards = readCustomWC()
		customWC.loaded = true
	}
	return append([]WonderCard(nil), customWC.cards...)
}

func reloadCustomWC() {
	cards := readCustomWC()
	customWC.mu.Lock()
	customWC.cards, customWC.loaded = cards, true
	customWC.mu.Unlock()
}

func readCustomWC() []WonderCard {
	dir := customWCDir()
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []WonderCard
	for _, e := range ents {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".wc3") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		c, ok := customCard(e.Name(), p, b)
		if ok {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Label) < strings.ToLower(out[j].Label) })
	return out
}

// customCard: the list's entry for a .wc3 (named after the file; what the card says is its description).
func customCard(name, path string, b []byte) (WonderCard, bool) {
	card, _, _, err := wc3Parts(b)
	if err != nil {
		return WonderCard{}, false
	}
	jp := len(b) == 0x4E4
	desc := ""
	if !jp {
		desc = wcCardText(card)
	}
	if desc == "" {
		desc = "Your own card (" + name + ")."
	}
	if jp {
		desc += " For the Japanese games."
	}
	return WonderCard{
		Group:       customWCGroup,
		Label:       strings.TrimSuffix(name, filepath.Ext(name)),
		Description: desc,
		File:        "custom/" + name,
		Path:        path,
		Japanese:    jp,
	}, true
}

// wcCardText: what an English/European card says: its title, subtitle and text.
func wcCardText(card []byte) string {
	const lineLen, first = 40, 10 // the card's 8 lines of text start after its 10 bytes of header
	if len(card) < first+8*lineLen {
		return ""
	}
	set := gameCharset()["intl"]
	var lines []string
	for i := 0; i < 8; i++ {
		var sb strings.Builder
		for _, c := range card[first+i*lineLen : first+(i+1)*lineLen] {
			if c == 0xFF {
				break
			}
			if t, ok := set[c]; ok {
				sb.WriteString(t)
			}
		}
		if l := strings.Join(strings.Fields(sb.String()), " "); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	head := lines[0]
	if len(lines) > 1 {
		head += " · " + lines[1]
	}
	if len(lines) > 2 {
		return head + ": " + strings.Join(lines[2:], " ")
	}
	return head
}

// importWonderCards copies .wc3 files into the cards folder. Returns how many went in, and why
// the others didn't.
func importWonderCards(paths []string) (int, []string) {
	dir := customWCDir()
	n := 0
	var bad []string
	for _, p := range paths {
		name := filepath.Base(p)
		b, err := os.ReadFile(p)
		if err != nil {
			bad = append(bad, name+": "+err.Error())
			continue
		}
		if _, _, _, err := wc3Parts(b); err != nil {
			bad = append(bad, name+": "+err.Error())
			continue
		}
		if !strings.EqualFold(filepath.Ext(name), ".wc3") {
			name += ".wc3"
		}
		dst := filepath.Join(dir, name)
		if same, _ := filepath.Abs(p); same == dst {
			n++ // it's already there
			continue
		}
		if old, err := os.ReadFile(dst); err == nil && string(old) != string(b) { // a different card by that name
			stem := strings.TrimSuffix(name, filepath.Ext(name))
			for i := 2; ; i++ {
				dst = filepath.Join(dir, fmt.Sprintf("%s (%d).wc3", stem, i))
				if old, err := os.ReadFile(dst); err != nil || string(old) == string(b) {
					break
				}
			}
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			bad = append(bad, name+": "+err.Error())
			continue
		}
		n++
	}
	reloadCustomWC()
	return n, bad
}

// removeCustomWonderCard deletes one of the player's cards (by its list File, "custom/<name>").
func removeCustomWonderCard(file string) error {
	for _, c := range customWonderCards() {
		if c.File == file {
			err := os.Remove(c.Path)
			reloadCustomWC()
			return err
		}
	}
	return nil
}

// addCustomWonderCards: the "Add .wc3 files…" buttons.
func (a *App) addCustomWonderCards() {
	if !pickMu.TryLock() {
		return
	}
	defer pickMu.Unlock()
	paths, err := pickFiles("Choose Wonder Card files", "Wonder Cards (*.wc3)", "*.wc3", "")
	if err != nil {
		a.ui.Toast(err.Error())
		return
	}
	if len(paths) == 0 {
		return
	}
	n, bad := importWonderCards(paths)
	switch {
	case len(bad) > 0 && n == 0:
		a.ui.Toast("Couldn't add it: " + bad[0])
	case len(bad) > 0:
		a.ui.Toast(fmt.Sprintf("Added %d; %d weren't Wonder Cards (%s)", n, len(bad), bad[0]))
	case n == 1:
		a.ui.Toast("Wonder Card added")
	default:
		a.ui.Toast(fmt.Sprintf("Added %d Wonder Cards", n))
	}
	for _, b := range bad {
		a.ui.Log("Wonder Card not added: %s", b)
	}
	a.redraw()
}

func (a *App) removeCustomWonderCard(file string) {
	if err := removeCustomWonderCard(file); err != nil {
		a.ui.Toast("Couldn't remove it: " + err.Error())
		return
	}
	a.ui.Toast("Wonder Card removed")
	a.redraw()
}
