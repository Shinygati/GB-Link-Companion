// The GBA's screens in the Pokémon game's language: French, German, Italian, Spanish or Japanese
// (kana), from assets/lang.json. The window stays in English.
//
//   - the GBA helper: GB-Link Companion sets its language before sending it (the letter after
//     "GBLLANG=" in the program), and tells a helper that's already running (MSG_LANG)
//   - the mGBA script's screens: their text is put into the script (--@LANG@)
//   - this app's own text on the status screens (the menu, hints, the encounter tracker): tr()
//
// On the status screens the text is ASCII and Latin-1 (codes = themselves), or with the Japanese
// font ASCII and kana; a kana is two cells wide: its code, then 0xFF.
package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed assets/lang.json
var langJSON []byte

type langTables struct {
	Langs []string            `json:"langs"` // F D I S J: the order of each text's translations
	Kana  string              `json:"kana"`
	Lua   map[string][]string `json:"lua"`
	App   map[string][]string `json:"app"`
	GBA   map[string][]string `json:"gba"`
}

var langData = sync.OnceValue(func() *langTables {
	var t langTables
	json.Unmarshal(langJSON, &t)
	return &t
})

// gameLang: the language of a Pokémon game from its code (AXVF, BPEJ, ...): E, F, D, I, S or J.
// Other games (and unknown languages) are E.
func gameLang(code string) byte {
	if testLang != 0 {
		return testLang
	}
	if len(code) != 4 || gameByCode[strings.ToUpper(code[:3])] == "" {
		return 'E'
	}
	switch c := code[3]; c {
	case 'F', 'D', 'I', 'S', 'J':
		return c
	}
	return 'E'
}

var testLang byte // tests: GBLINK_TEST_LANG (testhooks_on.go)

var langNamesEN = map[byte]string{'E': "English", 'F': "French", 'D': "German", 'I': "Italian", 'S': "Spanish", 'J': "Japanese"}

func langIndex(lang byte) int {
	for i, l := range langData().Langs {
		if l[0] == lang {
			return i
		}
	}
	return -1
}

func lookup(table map[string][]string, lang byte, s string) string {
	i := langIndex(lang)
	if t, ok := table[s]; ok && i >= 0 && i < len(t) && t[i] != "" {
		return t[i]
	}
	return s
}

// tr: this app's text on the status screens, in the game's language
func (b *Bridge) tr(s string) string { return lookup(langData().App, b.lang, s) }

// luaTranslations: the script's text for the game's language, as the script's TR table ("" for English)
func luaTranslations(lang byte) string {
	if langIndex(lang) < 0 {
		return ""
	}
	keys := make([]string, 0, len(langData().Lua))
	for k := range langData().Lua {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteString("local TR = {\n")
	for _, k := range keys {
		sb.WriteString("  [" + strconv.Quote(k) + "] = " + strconv.Quote(lookup(langData().Lua, lang, k)) + ",\n")
	}
	sb.WriteString("}")
	return sb.String()
}

// helperImageFor: the GBA helper with its text in this language
func helperImageFor(lang byte) []byte {
	img := append([]byte(nil), helperImage...)
	if i := bytes.Index(img, []byte("GBLLANG=")); i >= 0 && i+8 < len(img) {
		img[i+8] = lang
	}
	return img
}

const msgLang = 5

// SetLanguage: the helper's text in this language (and its screen redrawn). Old helpers ignore it.
func (h *Helper) SetLanguage(lang byte) error {
	_, _, err := h.Command(cmdShow, []uint32{msgLang, uint32(lang)}, nil, 3*time.Second, []int{0}, time.Second)
	return err
}

// ------------------------------------------------------------------- text on the status screens
var kanaCode = sync.OnceValue(func() map[rune]uint16 {
	slots := []uint16{}
	for c := 0x01; c < 0x20; c++ {
		slots = append(slots, uint16(c))
	}
	slots = append(slots, 0x5C, 0x60)
	for c := 0x7F; c < 0xFF; c++ {
		slots = append(slots, uint16(c))
	}
	m := map[rune]uint16{}
	for i, r := range []rune(langData().Kana) {
		if i < len(slots) {
			m[r] = slots[i]
		}
	}
	return m
})

// letters the Japanese font doesn't have, as ones it does
var foldLatin = func() map[rune]string {
	m := map[rune]string{'…': "...", '‘': "'", '’': "'", '“': "\"", '”': "\"", 'Œ': "OE", 'œ': "oe", '×': "x", '·': ".",
		'¥': "Y", '`': "'", '\\': "/", '　': "  ", '♂': "M", '♀': "F", '▶': ">"}
	groups := map[string]string{"A": "ÀÁÂÃÄÅ", "a": "àáâãäåª", "C": "Ç", "c": "ç", "E": "ÈÉÊË", "e": "èéêë", "I": "ÌÍÎÏ",
		"i": "ìíîï", "N": "Ñ", "n": "ñ", "O": "ÒÓÔÕÖØ", "o": "òóôõöøº", "U": "ÙÚÛÜ", "u": "ùúûü", "Y": "Ý", "y": "ýÿ",
		"ss": "ß", "!": "¡", "?": "¿"}
	for to, from := range groups {
		for _, r := range from {
			m[r] = to
		}
	}
	return m
}()

// cells: s as the status screen's codes, two for each kana with the Japanese font (code, 0xFF)
func cells(s string, jp bool) []uint16 {
	var out []uint16
	for _, r := range s {
		if jp {
			if c, ok := kanaCode()[r]; ok {
				out = append(out, c, 0xFF)
				continue
			}
			if r >= 0x20 && r < 0x7F && r != '\\' && r != '`' {
				out = append(out, uint16(r))
				continue
			}
		} else if r >= 0x20 && r < 0x7F || r >= 0xA0 && r <= 0xFF {
			out = append(out, uint16(r))
			continue
		}
		if f, ok := foldLatin[r]; ok {
			out = append(out, cells(f, jp)...)
			continue
		}
		out = append(out, '?')
	}
	return out
}

// textWidth: how many cells s takes on the status screens
func textWidth(s string, jp bool) int { return len(cells(s, jp)) }

// padCells: s padded with spaces (or cut) to n cells
func padCells(s string, n int, jp bool) string {
	w := 0
	var sb strings.Builder
	for _, r := range s {
		rw := textWidth(string(r), jp)
		if w+rw > n {
			break
		}
		sb.WriteRune(r)
		w += rw
	}
	return sb.String() + strings.Repeat(" ", n-w)
}
