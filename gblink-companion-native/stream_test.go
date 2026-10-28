package main

import (
	"bufio"
	"bytes"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// startFakeGBA runs test/fake_gblink.py (a pretend GB-Link on a pseudo serial port, with the
// helper's real code in b2s/sim/sim behind it) and returns the port. The GBA is waiting for
// multiboot.
func startFakeGBA(t *testing.T, saveKind string, cart []byte, env ...string) string {
	t.Helper()
	sim, _ := filepath.Abs("../b2s/sim/sim")
	if _, err := os.Stat(sim); err != nil {
		t.Skip("the helper simulator isn't built (make -C ../b2s/sim)")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("needs python3")
	}
	dir := t.TempDir()
	cartFile := filepath.Join(dir, "cart.sav")
	os.WriteFile(cartFile, cart, 0o644)
	script := `import sys, time
sys.path.insert(0, 'test')
from fake_gblink import FakeGBLink
g = FakeGBLink(sys.argv[1], sys.argv[2])
g.power_on_bios()
print(g.port, flush=True)
sys.stdin.read()
g.power_off()
`
	cmd := exec.Command("python3", "-c", script, cartFile, saveKind)
	cmd.Env = append(os.Environ(), env...)
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { in.Close(); cmd.Wait() })
	port, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(port)
}

func connectHelper(t *testing.T, port string) *Helper {
	t.Helper()
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
	if !h.Streams() {
		t.Fatalf("helper version %08x doesn't stream", h.version)
	}
	return h
}

func TestStreamedSaveFlash128(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	cart := make([]byte, 0x20000)
	r.Read(cart)
	h := connectHelper(t, startFakeGBA(t, "flash128", cart))
	got, err := h.ReadSave(4, func(int, int) {})
	if err != nil {
		t.Fatal("read:", err)
	}
	if !bytes.Equal(got, cart) {
		t.Fatal("the save read doesn't match the cartridge")
	}
	// change two separate places (one across the 64 KB boundary) and write back only those
	fresh := append([]byte(nil), cart...)
	for i := 0x3000; i < 0x3100; i++ {
		fresh[i] ^= 0x5A
	}
	for i := 0xF800; i < 0x10900; i++ {
		fresh[i] = byte(i * 7)
	}
	n, err := h.WriteSave(4, fresh, cart, func(int, int) {})
	if err != nil {
		t.Fatal("write:", err)
	}
	if n != 3 {
		t.Errorf("wrote %d blocks, want 3", n)
	}
	back, err := h.ReadSave(4, func(int, int) {})
	if err != nil || !bytes.Equal(back, fresh) {
		t.Fatal("the save didn't read back as written", err)
	}
}

func TestStreamedSaveEEPROMAndSRAM(t *testing.T) {
	for _, c := range []struct {
		kind  string
		stype int
		size  int
		env   []string
	}{{"eeprom64k", 6, 0x2000, []string{"SIM_ROMSTR=EEPROM_V124"}}, {"sram32", 1, 0x8000, nil}} {
		t.Run(c.kind, func(t *testing.T) {
			h := connectHelper(t, startFakeGBA(t, c.kind, nil, c.env...))
			before, err := h.ReadSave(c.stype, func(int, int) {})
			if err != nil || len(before) != c.size {
				t.Fatal("read:", err, len(before))
			}
			fresh := make([]byte, c.size)
			rand.New(rand.NewSource(9)).Read(fresh)
			if _, err := h.WriteSave(c.stype, fresh, nil, func(int, int) {}); err != nil {
				t.Fatal("write:", err)
			}
			back, err := h.ReadSave(c.stype, func(int, int) {})
			if err != nil || !bytes.Equal(back, fresh) {
				t.Fatal("the save didn't read back as written", err)
			}
		})
	}
}

func TestStreamedROM(t *testing.T) {
	// a 1 MB game with blank (FF) space and a run of padding in it
	rom := make([]byte, 1<<20)
	rand.New(rand.NewSource(5)).Read(rom[:0x60000])
	for i := 0x60000; i < len(rom); i++ {
		rom[i] = 0xFF
	}
	for i := 0x80000; i < 0x84000; i += 4 {
		copy(rom[i:], []byte{1, 2, 3, 4})
	}
	copy(rom[4:], []byte{0x24, 0xFF, 0xAE, 0x51})
	romFile := filepath.Join(t.TempDir(), "rom.bin")
	os.WriteFile(romFile, rom, 0o644)
	h := connectHelper(t, startFakeGBA(t, "sram32", nil, "SIM_ROM_FILE="+romFile))
	got, err := h.streamReadROM(len(rom), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, rom) {
		t.Fatal("the ROM didn't arrive as it is")
	}
}

func TestSaveRanges(t *testing.T) {
	a := make([]byte, 0x20000)
	b := append([]byte(nil), a...)
	for _, off := range []int{0x1000, 0x2000, 0x10000, 0x1F000} {
		b[off+5] = 1
	}
	got := saveRanges(b, a, len(a))
	want := [][2]int{{0x1000, 0x3000}, {0x10000, 0x11000}, {0x1F000, 0x20000}}
	if len(got) != len(want) {
		t.Fatalf("%v", got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%v", got)
		}
	}
	all := saveRanges(b, nil, len(b))
	if len(all) != 2 || all[0] != [2]int{0, 0x10000} || all[1] != [2]int{0x10000, 0x20000} {
		t.Fatalf("%v", all)
	}
}
