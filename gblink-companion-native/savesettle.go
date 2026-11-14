// Waiting for mGBA to finish writing a save before it goes to the cartridge.
//
// mGBA keeps the .sav mapped into memory while it runs, so an in-game save lands in the file the
// moment the game writes it, a byte at a time, while the file's modified time only changes now and
// then (Windows writes mapped pages back when it likes; mGBA writes its clock footer after the
// save, for the games with a clock). Pokémon's save takes about two seconds of flash writes in
// mGBA, so a watcher that goes by the modified time can copy a save that's only half written: a
// few sectors of the new slot, quick to write to the cartridge. If the GBA is then switched off,
// the cartridge keeps a new slot that's half there, and the game says the save is corrupted and
// loads the one before. So a save only counts as made once the file's contents have stopped
// changing, and, for a Pokémon save, once neither slot is half written.
package main

import (
	"encoding/binary"
	"sync"
	"time"
)

const (
	saveQuietTime = 3 * time.Second  // no change in the save for this long: mGBA has finished
	gen3WaitLimit = 20 * time.Second // a slot that stays half written this long is left as it is
)

type saveSettle struct {
	mu    sync.Mutex
	hash  string    // the save part of the .sav, as last seen
	since time.Time // when it last changed
	tried bool      // the watcher has already tried to copy this one
}

// observe looks at the save as it is now. settled: it hasn't changed for long enough to be a
// finished save. first: and the watcher hasn't tried to copy it yet.
func (w *saveSettle) observe(save []byte, now time.Time) (settled, first bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if h := hash(save); h != w.hash {
		w.hash, w.since, w.tried = h, now, false
		return false, false
	}
	if !w.quietLocked(save, now) {
		return false, false
	}
	first = !w.tried
	w.tried = true
	return true, first
}

// quiet: this save is the one last seen, and it has stopped changing.
func (w *saveSettle) quiet(save []byte, now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return hash(save) == w.hash && w.quietLocked(save, now)
}

func (w *saveSettle) quietLocked(save []byte, now time.Time) bool {
	q := now.Sub(w.since)
	if w.since.IsZero() || q < saveQuietTime {
		return false
	}
	return q >= gen3WaitLimit || !gen3HalfWritten(save)
}

// gen3HalfWritten: a Gen 3 Pokémon save (Ruby, Sapphire, Emerald, FireRed, LeafGreen and hacks
// of them) caught while the game writes it: one slot whole, the other with some of its 14 sectors
// from this save and the rest from an older one, or not there yet. Anything else, including a
// save that isn't a Gen 3 one, is false.
func gen3HalfWritten(b []byte) bool {
	if len(b) < 2*g3SlotSize {
		return false
	}
	whole, mixed := false, false
	for slot := 0; slot < 2; slot++ {
		signed, ids := 0, 0
		var counter uint32
		same := true
		for i := 0; i < g3Sectors; i++ {
			sec := b[slot*g3SlotSize+i*g3SectorSize:][:g3SectorSize]
			if binary.LittleEndian.Uint32(sec[0xFF8:]) != g3Signature {
				continue
			}
			if id := int(binary.LittleEndian.Uint16(sec[0xFF4:])); id < g3Sectors {
				ids |= 1 << id
			}
			c := binary.LittleEndian.Uint32(sec[0xFFC:])
			if signed == 0 {
				counter = c
			} else if c != counter {
				same = false
			}
			signed++
		}
		switch {
		case signed == 0: // never saved to
		case signed == g3Sectors && ids == 1<<g3Sectors-1 && same:
			whole = true
		default:
			mixed = true
		}
	}
	return whole && mixed
}

// savePart: the part of the .sav that goes to the cartridge (mGBA adds its clock after it).
func (s *Session) savePart(data []byte) []byte {
	if st, ok := saveTypes[s.saveType]; ok && len(data) > st.Size {
		return data[:st.Size]
	}
	return data
}
