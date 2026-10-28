// The Pokémon Bank's boxes on this PC (a folder for each box, a PKHeX .pk3 file for each
// Pokémon: bankfiles.go), and the note kept while a change is being written to a cartridge
// (bank-pending.json).
//
// The order of a change: the note is written first (the bank as it will be, and which Pokémon
// go each way); then the cartridge's save; then, only once the cartridge has been read back and
// matches, the bank's folders. If the app stops in between, the note is finished the next time that
// cartridge is opened, by looking at which Pokémon the cartridge has: none are lost.
package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	bankVersion   = 1
	bankStartSize = 10 // boxes in a new bank
	bankMaxBoxes  = 100
	bankBackups   = 30 // copies of the bank kept in Backups (one for each save)
)

type BankMon struct {
	Data  string `json:"data"`            // the 80 bytes as the games store them, in hex
	From  string `json:"from,omitempty"`  // the game it came from (its header code)
	Owner string `json:"owner,omitempty"` // the save it came from (Gen3Save.Owner)
	Added string `json:"added,omitempty"`
}

type BankBox struct {
	Name  string               `json:"name"`
	Slots [g3BoxSlots]*BankMon `json:"slots"`
}

type Bank struct {
	Version int       `json:"version"`
	Boxes   []BankBox `json:"boxes"`
}

func (m *BankMon) bytes() []byte {
	if m == nil {
		return nil
	}
	b, err := hex.DecodeString(m.Data)
	if err != nil || len(b) != pk3Size {
		return nil
	}
	return b
}

func newBankMon(b []byte, from, owner string) *BankMon {
	return &BankMon{Data: hex.EncodeToString(b[:pk3Size]), From: from, Owner: owner, Added: time.Now().Format("2006-01-02")}
}

func bankDir() string {
	d := filepath.Join(configDir(), "Pokemon Bank")
	os.MkdirAll(d, 0o755)
	return d
}

func bankPath() string        { return filepath.Join(bankDir(), "bank.json") }
func bankPendingPath() string { return filepath.Join(bankDir(), "bank-pending.json") }

func newBank() Bank {
	b := Bank{Version: bankVersion}
	b.addBoxes(bankStartSize)
	return b
}

func (b *Bank) addBoxes(n int) {
	for i := 0; i < n && len(b.Boxes) < bankMaxBoxes; i++ {
		b.Boxes = append(b.Boxes, BankBox{Name: fmt.Sprintf("BANK %d", len(b.Boxes)+1)})
	}
}

func (b Bank) clone() Bank {
	c := Bank{Version: b.Version, Boxes: make([]BankBox, len(b.Boxes))}
	for i, box := range b.Boxes {
		c.Boxes[i].Name = box.Name
		for j, m := range box.Slots {
			if m != nil {
				mm := *m
				c.Boxes[i].Slots[j] = &mm
			}
		}
	}
	return c
}

// count: how many Pokémon the bank holds.
func (b Bank) count() int {
	n := 0
	for _, box := range b.Boxes {
		for _, m := range box.Slots {
			if m != nil {
				n++
			}
		}
	}
	return n
}

// put stores a Pokémon in the first free slot, adding a box if they're all full.
func (b *Bank) put(m *BankMon) bool {
	for {
		for i := range b.Boxes {
			for j := range b.Boxes[i].Slots {
				if b.Boxes[i].Slots[j] == nil {
					b.Boxes[i].Slots[j] = m
					return true
				}
			}
		}
		if len(b.Boxes) >= bankMaxBoxes {
			return false
		}
		b.addBoxes(1)
	}
}

// loadBank reads the bank's folders (a bank.json from an older version is turned into them).
func loadBank() (Bank, error) {
	recoverBankFolders()
	dir := bankBoxesDir()
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if old, err := os.ReadFile(bankPath()); err == nil { // the old single file
			var b Bank
			if err := json.Unmarshal(old, &b); err != nil {
				return Bank{}, fmt.Errorf("the old bank file (%s) is damaged: %v", bankPath(), err)
			}
			if len(b.Boxes) == 0 {
				b.addBoxes(bankStartSize)
			}
			if err := saveBank(b); err != nil {
				return Bank{}, err
			}
			os.Rename(bankPath(), bankPath()+".old")
			return b, nil
		}
		return newBank(), nil
	}
	b, warn, err := readBankFolder(dir)
	bankLoadWarnings = warn
	if err != nil {
		return Bank{}, err
	}
	return b, nil
}

// writeFileSafely writes a file so it's either the old or the new one, never half of one.
func writeFileSafely(path string, data []byte) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// saveBank writes the bank's folders: the new ones are made beside the old, then swapped in, and
// the old ones go to Backups (the last bankBackups are kept).
func saveBank(b Bank) error {
	recoverBankFolders()
	dir := bankBoxesDir()
	next, prev := dir+".new", dir+".old"
	os.RemoveAll(next)
	if err := writeBankFolder(next, b); err != nil {
		os.RemoveAll(next)
		return fmt.Errorf("couldn't write the bank's folders: %v", err)
	}
	if _, err := os.Stat(dir); err == nil {
		if err := os.Rename(dir, prev); err != nil {
			os.RemoveAll(next)
			return fmt.Errorf("couldn't replace the bank's folders (is one of them open?): %v", err)
		}
	}
	if err := os.Rename(next, dir); err != nil {
		os.Rename(prev, dir)
		return fmt.Errorf("couldn't replace the bank's folders: %v", err)
	}
	os.Remove(filepath.Join(dir, bankComplete))
	if _, err := os.Stat(prev); err == nil {
		backupBankFolder(prev)
	}
	return nil
}

// ------------------------------------------------------------------------- the pending note
type bankJournal struct {
	Version   int      `json:"version"`
	Code      string   `json:"code"`      // the game
	Owner     string   `json:"owner"`     // the save (Gen3Save.Owner)
	Bank      Bank     `json:"bank"`      // the bank once the cartridge has the change
	Deposited []string `json:"deposited"` // Pokémon going from the cartridge to the bank (hex)
	Withdrawn []string `json:"withdrawn"` // Pokémon going from the bank to the cartridge (hex)
	Started   string   `json:"started"`
}

func loadJournal() (*bankJournal, error) {
	data, err := os.ReadFile(bankPendingPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var j bankJournal
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, fmt.Errorf("the Pokémon Bank's unfinished change (%s) is damaged: %v", bankPendingPath(), err)
	}
	return &j, nil
}

func saveJournal(j *bankJournal) error {
	data, err := json.MarshalIndent(j, "", " ")
	if err != nil {
		return err
	}
	return writeFileSafely(bankPendingPath(), data)
}

func clearJournal() error {
	err := os.Remove(bankPendingPath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// identitiesOn: every Pokémon on a save, in the boxes and the party.
func identitiesOn(s *Gen3Save) map[uint64]bool {
	have := map[uint64]bool{}
	boxes := s.Boxes()
	for b := range boxes {
		for _, raw := range boxes[b] {
			if p, ok := parsePk3(raw); ok {
				have[p.Identity()] = true
			}
		}
	}
	for _, raw := range s.Party() {
		if p, ok := parsePk3(raw); ok {
			have[p.Identity()] = true
		}
	}
	return have
}

// finishJournal works out the bank after a change that was interrupted, from the cartridge's
// save as it is now: a Pokémon that was going to the bank but is still on the cartridge stays
// there (it's taken out of the bank), and one that was going to the cartridge but isn't on it
// goes back in the bank. Then nothing is lost, whether the cartridge was written or not.
func finishJournal(j *bankJournal, s *Gen3Save) (Bank, string) {
	bank := j.Bank.clone()
	have := identitiesOn(s)
	kept, back := 0, 0
	for _, h := range j.Deposited {
		raw, _ := hex.DecodeString(h)
		p, ok := parsePk3(raw)
		if !ok || !have[p.Identity()] {
			continue
		}
		for i := range bank.Boxes { // still on the cartridge: not in the bank
			for k, m := range bank.Boxes[i].Slots {
				if q, ok := parsePk3(m.bytes()); m != nil && ok && q.Identity() == p.Identity() {
					bank.Boxes[i].Slots[k] = nil
					kept++
				}
			}
		}
	}
	for _, h := range j.Withdrawn {
		raw, _ := hex.DecodeString(h)
		p, ok := parsePk3(raw)
		if !ok || have[p.Identity()] {
			continue
		}
		if bank.put(newBankMon(raw, j.Code, j.Owner)) { // not on the cartridge: back in the bank
			back++
		}
	}
	msg := "The last change was written to the cartridge, so the bank has been brought up to date."
	if kept > 0 || back > 0 {
		msg = fmt.Sprintf("The last change didn't reach the cartridge completely. The bank was matched to what's on it: "+
			"%d Pokémon stayed on the cartridge and %d went back in the bank.", kept, back)
	}
	return bank, msg
}
