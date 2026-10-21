package main

import (
	"os"
	"strings"
	"testing"
)

func TestCells(t *testing.T) {
	if got := cells("Pokémon Œ", false); len(got) != 10 || got[3] != 0xE9 || got[8] != 'O' || got[9] != 'E' {
		t.Errorf("intl: %v", got)
	}
	got := cells("あア ー9é", true)
	want := []uint16{0x01, 0xFF, 0x7F + 47, 0xFF, ' ', 0x7F + 127, 0xFF, '9', 'e'}
	if len(got) != len(want) {
		t.Fatalf("jp: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("jp[%d] = %#x, want %#x (%v)", i, got[i], want[i], got)
		}
	}
	var c cellText
	putText(&c, true, 0, 39, 0, "あ") // half a kana doesn't fit: a space
	if c[0][39] != ' ' {
		t.Errorf("edge: %#x", c[0][39])
	}
	if w := textWidth("ポケモン Lv5", true); w != 12 {
		t.Errorf("width %d", w)
	}
	if p := padCells("ポケモン", 5, true); p != "ポケ " {
		t.Errorf("pad %q", p)
	}
}

func TestLangTables(t *testing.T) {
	if gameLang("BPEJ") != 'J' || gameLang("AXVD") != 'D' || gameLang("BPEE") != 'E' || gameLang("AGBJ") != 'E' {
		t.Error("gameLang")
	}
	l := luaTranslations('F')
	if !strings.Contains(l, `["WILD POKEMON"] = "POKÉMON SAUVAGE"`) {
		t.Errorf("lua: %.200s", l)
	}
	if luaTranslations('E') != "" {
		t.Error("english")
	}
	img := helperImageFor('J')
	if i := strings.Index(string(img), "GBLLANG="); i < 0 || img[i+8] != 'J' || helperImage[i+8] != 'E' {
		t.Error("helper image")
	}
	b := &Bridge{lang: 'D'}
	if b.tr("Encounter tracker") != "Begegnungszähler" || b.tr("nope") != "nope" {
		t.Error("tr")
	}
	// GBLINK_LUA_OUT=path:lang writes the script with that language's text (for the Lua tests)
	if v := os.Getenv("GBLINK_LUA_OUT"); v != "" {
		f := strings.SplitN(v, ":", 3)
		text := strings.Replace(strings.ReplaceAll(luaSource, "__PORT__", "47199"), "--@LANG@", luaTranslations(f[1][0]), 1)
		if len(f) == 3 && f[2] != "" {
			data, _ := os.ReadFile(f[2])
			fd, extra, err := findROMAddresses(data) // the finder even where the script knows the game
			if err != nil {
				t.Fatal(err)
			}
			text = strings.Replace(text, "--@OVERRIDE@", "local OVERRIDE = "+luaFound(data, fd, extra, gameByCode[string(data[0xAC:0xAF])]), 1)
		}
		os.WriteFile(f[0], []byte(text), 0o644)
	}
}
