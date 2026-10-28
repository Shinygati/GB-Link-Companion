// Moving Pokémon around in the Pokémon Bank's window, before anything is saved. Three modes, as
// the buttons at the top of the bank:
//   - Move: pick one Pokémon up and put it down (or drag it); one already there swaps places.
//   - Move Multi: choose several, then drag one of them, click an empty slot, or send them all
//     to the bank or the cartridge; they go into the empty slots from there on.
//   - Release: mark Pokémon, then release them (once you save, they're gone for good).
package main

import (
	"bytes"
	"fmt"
	"sort"
)

const (
	modeMove    = "move"
	modeMulti   = "multi"
	modeRelease = "release"
)

func (b *BankSession) get(r slotRef) []byte {
	if r.Bank {
		if r.Box < len(b.Bank.Boxes) {
			return b.Bank.Boxes[r.Box].Slots[r.Slot].bytes()
		}
		return nil
	}
	if pk3Empty(b.Cart[r.Box][r.Slot]) {
		return nil
	}
	return b.Cart[r.Box][r.Slot]
}

func (b *BankSession) meta(r slotRef) *BankMon {
	if r.Bank && r.Box < len(b.Bank.Boxes) {
		return b.Bank.Boxes[r.Box].Slots[r.Slot]
	}
	return nil
}

// Mon: the Pokémon in a slot, and where it came from (the bank's note), if any.
func (b *BankSession) Mon(r slotRef) (Pk3, *BankMon, bool) {
	p, ok := parsePk3(b.get(r))
	return p, b.meta(r), ok
}

// place puts a Pokémon (raw, nil for none) in a slot; meta is its bank note if it has one.
func (b *BankSession) place(r slotRef, raw []byte, meta *BankMon) {
	if r.Bank {
		switch {
		case raw == nil:
			b.Bank.Boxes[r.Box].Slots[r.Slot] = nil
		case meta != nil:
			b.Bank.Boxes[r.Box].Slots[r.Slot] = meta
		default:
			b.Bank.Boxes[r.Box].Slots[r.Slot] = newBankMon(raw, b.Code, b.owner)
		}
		return
	}
	if raw == nil {
		b.Cart[r.Box][r.Slot] = make([]byte, pk3Size)
	} else {
		b.Cart[r.Box][r.Slot] = append([]byte(nil), raw...)
	}
}

// fitsCart: why a Pokémon can't go on the cartridge, or "".
func fitsCart(raw []byte) string {
	if raw == nil {
		return ""
	}
	if p, _ := parsePk3(raw); !p.Valid() {
		return "That one can't go on the cartridge: its data is damaged (the game would show a Bad Egg). It can stay in the bank."
	}
	return ""
}

// editable: the boxes are loaded and nothing is being saved.
func (b *BankSession) editable() bool { return b.Ready && !b.Saving && !b.Loading && !b.Locked }

func (b *BankSession) shown(bank bool) int {
	if bank {
		return b.BankBox
	}
	return b.CartBox
}

// ------------------------------------------------------------------------------- modes
// SetMode switches between Move, Move Multi and Release. What was chosen in the other mode is let go.
func (b *BankSession) SetMode(m string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if m != modeMove && m != modeMulti && m != modeRelease || m == b.Mode {
		return
	}
	b.Mode, b.Held, b.Note = m, nil, ""
	b.Multi, b.Marked = map[slotRef]bool{}, map[slotRef]bool{}
	b.Rev++
}

// Tap: a click on a slot, in the mode the bank is in.
func (b *BankSession) Tap(r slotRef) {
	b.mu.Lock()
	defer b.mu.Unlock()
	has := b.get(r) != nil
	if has { // its summary shows even when nothing can be changed
		b.Sel = &r
		b.Rev++
	}
	if !b.editable() {
		return
	}
	b.Note = ""
	switch b.Mode {
	case modeMulti:
		if has {
			if b.Multi[r] {
				delete(b.Multi, r)
			} else {
				b.Multi[r] = true
			}
		} else if len(b.Multi) > 0 { // an empty slot: the chosen ones go there
			b.moveGroup(b.picked(b.Multi), r)
		}
	case modeRelease:
		if has {
			if b.Marked[r] {
				delete(b.Marked, r)
			} else {
				b.Marked[r] = true
			}
		}
	default:
		b.click(r)
	}
	b.Rev++
}

// Click: Move mode's click (kept for the tests and the old buttons).
func (b *BankSession) Click(r slotRef) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.editable() {
		return
	}
	b.Note = ""
	b.click(r)
	b.Rev++
}

// click picks a Pokémon up, or puts the one held down there (swapping with what's there).
func (b *BankSession) click(r slotRef) {
	if b.Held == nil {
		if b.get(r) != nil {
			b.Held, b.Sel = &r, &r
		} else {
			b.Sel = nil
		}
		return
	}
	from := *b.Held
	b.Held = nil
	if from != r {
		b.swap(from, r)
	}
}

// swap moves the Pokémon in from to to, and the one in to (if any) to from.
func (b *BankSession) swap(from, to slotRef) bool {
	moving, there := b.get(from), b.get(to)
	if moving == nil {
		return false
	}
	why := ""
	if !to.Bank {
		why = fitsCart(moving)
	}
	if why == "" && !from.Bank {
		why = fitsCart(there)
	}
	if why != "" {
		b.Note = why
		return false
	}
	fromMeta, thereMeta := b.meta(from), b.meta(to)
	b.place(to, moving, fromMeta)
	b.place(from, there, thereMeta)
	b.Sel = &to
	return true
}

// Drop: a Pokémon dragged from one slot to another. In Move Multi, dragging one of the chosen
// Pokémon takes them all.
func (b *BankSession) Drop(from, to slotRef) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.editable() || from == to {
		return
	}
	b.Note, b.Held = "", nil
	if b.Mode == modeMulti && b.Multi[from] && len(b.Multi) > 1 {
		b.moveGroup(b.picked(b.Multi), to)
	} else if b.swap(from, to) && b.Mode == modeMulti {
		b.Multi = map[slotRef]bool{} // moved: nothing's chosen any more
	}
	b.Rev++
}

// picked: the chosen slots in order (the cartridge's first, then the bank's, box by box).
func (b *BankSession) picked(set map[slotRef]bool) []slotRef {
	var out []slotRef
	for r, on := range set {
		if on && b.get(r) != nil {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		x, y := out[i], out[j]
		if x.Bank != y.Bank {
			return !x.Bank
		}
		if x.Box != y.Box {
			return x.Box < y.Box
		}
		return x.Slot < y.Slot
	})
	return out
}

// moveGroup moves Pokémon into the empty slots from to onwards (on to's side: the following
// boxes too). If there isn't room, nothing moves.
func (b *BankSession) moveGroup(refs []slotRef, to slotRef) {
	if len(refs) == 0 {
		return
	}
	type mon struct {
		raw  []byte
		meta *BankMon
	}
	var mons []mon
	for _, r := range refs {
		raw := b.get(r)
		if !to.Bank {
			if why := fitsCart(raw); why != "" {
				b.Note = "One of them can't go on the cartridge: its data is damaged (a Bad Egg). Leave it out and try again."
				return
			}
		}
		mons = append(mons, mon{append([]byte(nil), raw...), b.meta(r)})
	}
	for _, r := range refs { // their old slots count as free
		b.place(r, nil, nil)
	}
	var dest []slotRef
	box, slot := to.Box, to.Slot
	added := 0
	for len(dest) < len(mons) {
		boxes := g3BoxCount
		if to.Bank {
			boxes = len(b.Bank.Boxes)
		}
		if box >= boxes {
			if !to.Bank || len(b.Bank.Boxes) >= bankMaxBoxes {
				break
			}
			b.Bank.addBoxes(1)
			added++
		}
		r := slotRef{Bank: to.Bank, Box: box, Slot: slot}
		if b.get(r) == nil {
			dest = append(dest, r)
		}
		if slot++; slot == g3BoxSlots {
			box, slot = box+1, 0
		}
	}
	if len(dest) < len(mons) { // no room: put them back
		for i, r := range refs {
			b.place(r, mons[i].raw, mons[i].meta)
		}
		if added > 0 {
			b.Bank.Boxes = b.Bank.Boxes[:len(b.Bank.Boxes)-added]
		}
		where := "on the cartridge"
		if to.Bank {
			where = "in the bank"
		}
		b.Note = fmt.Sprintf("There isn't room for %d Pokémon from there %s. Start further back, or make some room.", len(mons), where)
		return
	}
	b.Multi = map[slotRef]bool{} // moved: nothing's chosen any more
	for i, r := range dest {
		b.place(r, mons[i].raw, mons[i].meta)
	}
	b.Sel = &dest[0]
	if dest[0].Box != b.shown(to.Bank) { // show where they went
		if to.Bank {
			b.BankBox = dest[0].Box
		} else {
			b.CartBox = dest[0].Box
		}
	}
	b.Note = fmt.Sprintf("Moved %d Pokémon.", len(mons))
}

// SendTo moves the chosen Pokémon that aren't already there to the bank or the cartridge,
// from the start of the box shown there.
func (b *BankSession) SendTo(bank bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.editable() {
		return
	}
	if !bank && !b.CartLoaded {
		b.Note = "Load a cartridge first."
		b.Rev++
		return
	}
	var refs []slotRef
	for _, r := range b.picked(b.Multi) {
		if r.Bank != bank {
			refs = append(refs, r)
		}
	}
	b.Note = ""
	if len(refs) == 0 {
		b.Note = "They're all there already."
	} else {
		b.moveGroup(refs, slotRef{Bank: bank, Box: b.shown(bank)})
	}
	b.Rev++
}

// SelectBox chooses every Pokémon in the box shown on that side (or, if they all are, none).
func (b *BankSession) SelectBox(bank bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.editable() {
		return
	}
	set := b.Multi
	if b.Mode == modeRelease {
		set = b.Marked
	}
	all := true
	var refs []slotRef
	for i := 0; i < g3BoxSlots; i++ {
		r := slotRef{Bank: bank, Box: b.shown(bank), Slot: i}
		if b.get(r) != nil {
			refs = append(refs, r)
			all = all && set[r]
		}
	}
	for _, r := range refs {
		if all {
			delete(set, r)
		} else {
			set[r] = true
		}
	}
	b.Rev++
}

// ClearPicks lets go of everything chosen or marked.
func (b *BankSession) ClearPicks() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Multi, b.Marked, b.Held = map[slotRef]bool{}, map[slotRef]bool{}, nil
	b.Rev++
}

func (b *BankSession) markedCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.picked(b.Marked))
}

// releaseMarked takes the marked Pokémon out (for good once saved; Undo brings them back).
func (b *BankSession) releaseMarked() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.editable() {
		return 0
	}
	refs := b.picked(b.Marked)
	for _, r := range refs {
		b.place(r, nil, nil)
	}
	b.Marked = map[slotRef]bool{}
	if b.Sel != nil && b.get(*b.Sel) == nil {
		b.Sel = nil
	}
	if len(refs) > 0 {
		b.Note = fmt.Sprintf("%d released. They're gone for good once you save (Undo changes brings them back until then).", len(refs))
	}
	b.Rev++
	return len(refs)
}

// Turn shows another box (bank: the bank's, else the cartridge's).
func (b *BankSession) Turn(bank bool, by int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if bank {
		n := len(b.Bank.Boxes)
		b.BankBox = ((b.BankBox+by)%n + n) % n
	} else {
		b.CartBox = ((b.CartBox+by)%g3BoxCount + g3BoxCount) % g3BoxCount
	}
	b.Rev++
}

// AddBox adds a box to the bank and shows it.
func (b *BankSession) AddBox() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.Saving || len(b.Bank.Boxes) >= bankMaxBoxes {
		return
	}
	b.Bank.addBoxes(1)
	b.BankBox = len(b.Bank.Boxes) - 1
	b.Rev++
}

func (b *BankSession) undo() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.Saving {
		return
	}
	b.Cart = copyBoxes(b.orig)
	b.Bank = b.origBank.clone()
	if b.BankBox >= len(b.Bank.Boxes) {
		b.BankBox = len(b.Bank.Boxes) - 1
	}
	b.Held, b.Sel, b.Note = nil, nil, "Your changes were undone."
	b.Multi, b.Marked = map[slotRef]bool{}, map[slotRef]bool{}
	b.Rev++
}

// --------------------------------------------------------------------------- what changed
type bankCounts struct {
	ToBank, ToCart, Released int
	Any                      bool // anything changed, even just the order
}

func cartIDs(boxes [g3BoxCount][g3BoxSlots][]byte) map[uint64][]byte {
	out := map[uint64][]byte{}
	for x := range boxes {
		for _, raw := range boxes[x] {
			if p, ok := parsePk3(raw); ok {
				out[p.Identity()] = raw
			}
		}
	}
	return out
}

func bankIDs(bank Bank) map[uint64]bool {
	out := map[uint64]bool{}
	for _, box := range bank.Boxes {
		for _, m := range box.Slots {
			if p, ok := parsePk3(m.bytes()); ok {
				out[p.Identity()] = true
			}
		}
	}
	return out
}

// counts: how many Pokémon go to the bank, to the cartridge, and are released. Call with mu held.
func (b *BankSession) counts() bankCounts {
	oc, nc := cartIDs(b.orig), cartIDs(b.Cart)
	ob, nb := bankIDs(b.origBank), bankIDs(b.Bank)
	var c bankCounts
	for id := range oc {
		if _, still := nc[id]; !still {
			if nb[id] {
				c.ToBank++
			} else {
				c.Released++
			}
		}
	}
	for id := range ob {
		if _, there := nc[id]; there {
			c.ToCart++
		} else if !nb[id] {
			c.Released++
		}
	}
	c.Any = b.cartChanged() || b.bankChanged()
	return c
}

func (b *BankSession) dirty() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cartChanged() || b.bankChanged()
}

func (b *BankSession) cartChanged() bool {
	for x := range b.Cart {
		for i := range b.Cart[x] {
			if !bytes.Equal(b.Cart[x][i], b.orig[x][i]) {
				return true
			}
		}
	}
	return false
}

func (b *BankSession) bankChanged() bool {
	if len(b.Bank.Boxes) != len(b.origBank.Boxes) {
		return true
	}
	for x := range b.Bank.Boxes {
		for i := range b.Bank.Boxes[x].Slots {
			m, o := b.Bank.Boxes[x].Slots[i], b.origBank.Boxes[x].Slots[i]
			if (m == nil) != (o == nil) || (m != nil && m.Data != o.Data) {
				return true
			}
		}
	}
	return false
}

// moves: the Pokémon leaving the cartridge (for the bank, or released) and arriving on it.
func (b *BankSession) moves() (deposited, withdrawn [][]byte) {
	before, after := cartIDs(b.orig), cartIDs(b.Cart)
	for id, raw := range before {
		if _, ok := after[id]; !ok {
			deposited = append(deposited, raw)
		}
	}
	for id, raw := range after {
		if _, ok := before[id]; !ok {
			withdrawn = append(withdrawn, raw)
		}
	}
	return
}

// ------------------------------------------------------------------------- for the window
// bankView: what the window draws, copied out from behind the lock.
type bankView struct {
	Ready, Saving, Loading      bool
	CartLoaded, Locked          bool
	Mode                        string
	GameName, Trainer           string
	TID                         int
	CartBox, BankBox, BankBoxes int
	CartName, BankName          string
	Cart, BankSlots             [g3BoxSlots]*Pk3
	CartPick, BankPick          [g3BoxSlots]bool // chosen (Move Multi) or marked (Release)
	Held, Sel                   *slotRef
	SelMon                      *Pk3
	SelMeta                     *BankMon
	Note                        string
	Counts                      bankCounts
	Picked                      int // how many are chosen or marked
	InBank, PartyCount          int
	Data                        *bankData
	Icons                       *bankIcons
	Rev                         uint64
}

func (b *BankSession) view() bankView {
	b.mu.Lock()
	defer b.mu.Unlock()
	v := bankView{Ready: b.Ready, Saving: b.Saving, Loading: b.Loading, CartLoaded: b.CartLoaded, Locked: b.Locked, Mode: b.Mode, GameName: b.GameName, Trainer: b.Trainer, TID: b.TID,
		CartBox: b.CartBox, BankBox: b.BankBox, BankBoxes: len(b.Bank.Boxes), Note: b.Note,
		PartyCount: b.PartyCount, Data: b.Data, Icons: b.Icons, Rev: b.Rev}
	if !b.Ready {
		return v
	}
	v.CartName = b.BoxNames[b.CartBox]
	if !b.CartLoaded {
		v.CartName = ""
	}
	if b.BankBox < len(b.Bank.Boxes) {
		v.BankName = b.Bank.Boxes[b.BankBox].Name
	}
	set := b.Multi
	if b.Mode == modeRelease {
		set = b.Marked
	}
	for i := 0; i < g3BoxSlots; i++ {
		c, k := slotRef{Box: b.CartBox, Slot: i}, slotRef{Bank: true, Box: b.BankBox, Slot: i}
		if p, _, ok := b.Mon(c); ok {
			v.Cart[i] = &p
		}
		if p, _, ok := b.Mon(k); ok {
			v.BankSlots[i] = &p
		}
		v.CartPick[i], v.BankPick[i] = set[c] && v.Cart[i] != nil, set[k] && v.BankSlots[i] != nil
	}
	v.Picked = len(b.picked(set))
	if b.Held != nil {
		h := *b.Held
		v.Held = &h
	}
	if b.Sel != nil {
		s := *b.Sel
		v.Sel = &s
		if p, m, ok := b.Mon(s); ok {
			v.SelMon = &p
			if m != nil {
				mm := *m
				v.SelMeta = &mm
			}
		}
	}
	v.Counts = b.counts()
	v.InBank = b.Bank.count()
	return v
}

// SelectRefs chooses (Move Multi) or marks (Release) the Pokémon in these slots: the ones a box
// dragged over the slots covered. add keeps what was chosen before (Ctrl or Shift held).
func (b *BankSession) SelectRefs(refs []slotRef, add bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.editable() || b.Mode == modeMove {
		return
	}
	set := &b.Multi
	if b.Mode == modeRelease {
		set = &b.Marked
	}
	if !add {
		*set = map[slotRef]bool{}
	}
	for _, r := range refs {
		if b.get(r) != nil {
			(*set)[r] = true
			b.Sel = &r
		}
	}
	b.Note = ""
	b.Rev++
}
