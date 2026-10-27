package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCustomWonderCards(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GBLINK_COMPANION_HOME", home)
	reloadCustomWC()
	src := t.TempDir()
	b, err := wcFS.ReadFile("assets/wondercards/BPEE0/001.wc3")
	if err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(src, "My Aurora.wc3")
	os.WriteFile(good, b, 0o644)
	bad := filepath.Join(src, "junk.wc3")
	os.WriteFile(bad, []byte("nope"), 0o644)
	jp, _ := wcFS.ReadFile("assets/wondercards/BPEJ0/001.wc3")
	jpPath := filepath.Join(src, "jp.wc3")
	os.WriteFile(jpPath, jp, 0o644)

	n, errs := importWonderCards([]string{good, bad, jpPath})
	if n != 2 || len(errs) != 1 {
		t.Fatalf("imported %d, errors %v", n, errs)
	}
	// the same name, a different card: kept as a second file
	b2, _ := wcFS.ReadFile("assets/wondercards/BPEE0/002.wc3")
	os.WriteFile(good, b2, 0o644)
	if n, _ := importWonderCards([]string{good}); n != 1 || len(customWonderCards()) != 3 {
		t.Fatalf("second import: %d, %d cards", n, len(customWonderCards()))
	}
	en := wonderCardsFor("BPEE0", false)
	var mine []WonderCard
	for _, c := range en {
		if c.Group == customWCGroup {
			mine = append(mine, c)
		}
	}
	if len(mine) != 2 || !strings.Contains(mine[0].Description, "AURORATICKET") {
		t.Fatalf("English list: %+v", mine)
	}
	if got := wonderCardsFor("BPEJ0", false); got[len(got)-1].Label != "jp" {
		t.Fatalf("Japanese list ends with %q", got[len(got)-1].Label)
	}
	// it can be sent: the bytes come from the file
	var sent []byte
	ws := &wcSender{ui: newUI(nil), transmit: func(id int, c WonderCard, how string, card, icon, data []byte) error {
		sent = card
		return nil
	}}
	if err := ws.sendWonderCard(mine[0], "add"); err != nil || len(sent) != 332 {
		t.Fatalf("send: %v, %d bytes", err, len(sent))
	}
	if err := removeCustomWonderCard(mine[0].File); err != nil || len(customWonderCards()) != 2 {
		t.Fatalf("remove: %v, %d left", err, len(customWonderCards()))
	}
}
