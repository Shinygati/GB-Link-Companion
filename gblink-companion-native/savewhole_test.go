package main

import (
	"bytes"
	"math/rand"
	"testing"
	"time"
)

// The helper (simulated) reads and writes a whole 128 KB flash save, both banks, the way pull and
// push do it now (acknowledged commands, every block written).
func TestWholeFlash128SaveRoundTrip(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	cart := make([]byte, 0x20000)
	r.Read(cart)
	port := startFakeGBA(t, "flash128", cart)
	l, err := OpenGBLink(port)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.Close)
	if err := l.Multiboot(helperImage, 5*time.Second, nil); err != nil {
		t.Fatal("multiboot:", err)
	}
	h := &Helper{link: l, gap: 36}
	if !h.Alive(8 * time.Second) {
		t.Fatal("the helper didn't answer")
	}
	info, err := h.SaveInfo()
	if err != nil || info.Type != 4 {
		t.Fatalf("SaveInfo: %+v %v", info, err)
	}
	got, err := h.ReadSave(4, func(int, int) {})
	if err != nil || !bytes.Equal(got, cart) {
		t.Fatalf("read: %v (equal %v)", err, bytes.Equal(got, cart))
	}
	fresh := make([]byte, len(cart))
	r.Read(fresh)
	n, err := h.WriteSave(4, fresh, nil, func(int, int) {})
	if err != nil || n != 32 {
		t.Fatalf("write: %d blocks, %v", n, err)
	}
	back, err := h.ReadSave(4, func(int, int) {})
	if err != nil || !bytes.Equal(back, fresh) {
		t.Fatalf("read back: %v (equal %v)", err, bytes.Equal(back, fresh))
	}
}
