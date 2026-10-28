// The Pokémon Bank on disk: a folder for each box ("Box 01 - BANK 1"), and in it a .pk3 file
// for each Pokémon ("Slot 05 - PIKACHU - 1A2B3C4D.pk3"), decrypted as PKHeX saves them, so
// PKHeX can open them and .pk3 files from PKHeX can be put in. A .pk3 without "Slot NN" in its
// name goes in the box's first free slot.
//
// Saving makes the new folders beside the old ones and swaps them in; the old ones are moved to
// Backups (one dated copy for each save). If the app stops while saving, the next load picks up
// whichever copy was whole.
package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const bankComplete = ".complete" // in a new copy of the folders once every file is written

func bankBoxesDir() string   { return filepath.Join(bankDir(), "Boxes") }
func bankBackupsDir() string { return filepath.Join(bankDir(), "Backups") }

// bankLoadWarnings: files the last load couldn't use (they're kept in Backups after the next save)
var bankLoadWarnings []string

var (
	boxDirName  = regexp.MustCompile(`^Box (\d+)(?: - (.*))?$`)
	slotFileNum = regexp.MustCompile(`(?i)^Slot (\d+)\b`)
)

// fileSafe makes a name safe for a file name on every system.
func fileSafe(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, s)
	s = strings.TrimRight(strings.TrimSpace(s), ". ")
	if s == "" {
		s = "Pokemon"
	}
	return s
}

func pk3FileName(slot int, raw []byte) string {
	p, _ := parsePk3(raw)
	nick := decodeGameText(p.Nick, p.Japanese())
	if p.BadEgg {
		nick = "Bad Egg"
	}
	return fmt.Sprintf("Slot %02d - %s - %08X.pk3", slot+1, fileSafe(nick), p.PID)
}

// writeDurably writes a file and makes sure it's on the disk.
func writeDurably(path string, data []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// writeBankFolder writes the whole bank into root (a new folder).
func writeBankFolder(root string, b Bank) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	for i, box := range b.Boxes {
		dir := filepath.Join(root, fmt.Sprintf("Box %02d - %s", i+1, fileSafe(box.Name)))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		for j, m := range box.Slots {
			raw := m.bytes()
			if raw == nil {
				continue
			}
			path := filepath.Join(dir, pk3FileName(j, raw))
			if err := writeDurably(path, pk3ToFile(raw)); err != nil {
				return err
			}
			if t, err := time.ParseInLocation("2006-01-02", m.Added, time.Local); err == nil {
				os.Chtimes(path, t, t) // the file's date: when it went in the bank
			}
		}
	}
	return writeDurably(filepath.Join(root, bankComplete), []byte("ok\n"))
}

// readBankFolder reads the bank from its folders. warnings lists the files it couldn't use.
func readBankFolder(root string) (Bank, []string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return Bank{}, nil, fmt.Errorf("couldn't read the bank's folders: %v", err)
	}
	type boxDir struct {
		num        int
		name, path string
	}
	var dirs []boxDir
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		d := boxDir{num: 1 << 30, name: e.Name(), path: filepath.Join(root, e.Name())}
		if m := boxDirName.FindStringSubmatch(e.Name()); m != nil {
			d.num, _ = strconv.Atoi(m[1])
			d.name = strings.TrimSpace(m[2])
		}
		dirs = append(dirs, d)
	}
	sort.SliceStable(dirs, func(i, j int) bool {
		if dirs[i].num != dirs[j].num {
			return dirs[i].num < dirs[j].num
		}
		return dirs[i].path < dirs[j].path
	})
	if len(dirs) > bankMaxBoxes {
		dirs = dirs[:bankMaxBoxes]
	}
	b := Bank{Version: bankVersion}
	var warn []string
	var spare []*BankMon // no room in their own box
	for i, d := range dirs {
		name := d.name
		if name == "" {
			name = fmt.Sprintf("BANK %d", i+1)
		}
		box := BankBox{Name: name}
		files, _ := os.ReadDir(d.path)
		var loose []*BankMon // no slot number in the name
		for _, f := range files {
			if f.IsDir() || !strings.EqualFold(filepath.Ext(f.Name()), ".pk3") {
				continue
			}
			path := filepath.Join(d.path, f.Name())
			data, err := os.ReadFile(path)
			var raw []byte
			if err == nil {
				raw, err = pk3FromFile(data)
			}
			if err != nil {
				warn = append(warn, fmt.Sprintf("%s: %v", filepath.Join(filepath.Base(d.path), f.Name()), err))
				continue
			}
			m := &BankMon{Data: hex.EncodeToString(raw)}
			if info, err := f.Info(); err == nil {
				m.Added = info.ModTime().Format("2006-01-02")
			}
			if s := slotFileNum.FindStringSubmatch(f.Name()); s != nil {
				if n, _ := strconv.Atoi(s[1]); n >= 1 && n <= g3BoxSlots && box.Slots[n-1] == nil {
					box.Slots[n-1] = m
					continue
				}
			}
			loose = append(loose, m)
		}
		for _, m := range loose {
			placed := false
			for k := range box.Slots {
				if box.Slots[k] == nil {
					box.Slots[k], placed = m, true
					break
				}
			}
			if !placed {
				spare = append(spare, m)
			}
		}
		b.Boxes = append(b.Boxes, box)
	}
	if len(b.Boxes) == 0 {
		b.addBoxes(bankStartSize)
	}
	for _, m := range spare {
		if !b.put(m) {
			warn = append(warn, "the bank is full: a Pokémon couldn't be placed")
		}
	}
	return b, warn, nil
}

// backupBankFolder moves an old copy of the folders to Backups, dated, and keeps the last ones.
func backupBankFolder(old string) {
	dir := bankBackupsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	os.Remove(filepath.Join(old, bankComplete))
	dst := filepath.Join(dir, time.Now().Format("2006-01-02 15-04-05"))
	for n := 2; ; n++ {
		if _, err := os.Stat(dst); os.IsNotExist(err) {
			break
		}
		dst = filepath.Join(dir, time.Now().Format("2006-01-02 15-04-05")+fmt.Sprintf(" (%d)", n))
	}
	if os.Rename(old, dst) != nil {
		return
	}
	entries, _ := os.ReadDir(dir)
	var copies []string
	for _, e := range entries {
		if e.IsDir() {
			copies = append(copies, e.Name()) // the names sort by date
		}
	}
	sort.Strings(copies)
	for len(copies) > bankBackups {
		os.RemoveAll(filepath.Join(dir, copies[0]))
		copies = copies[1:]
	}
}

// recoverBankFolders tidies up after a save that was cut short: the new copy is used if it was
// finished and the old one had already been moved away; otherwise the old one stays.
func recoverBankFolders() {
	dir := bankBoxesDir()
	next, prev := dir+".new", dir+".old"
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	if exists(next) {
		if !exists(dir) && exists(filepath.Join(next, bankComplete)) {
			if os.Rename(next, dir) == nil {
				os.Remove(filepath.Join(dir, bankComplete))
			}
		} else {
			os.RemoveAll(next)
		}
	}
	if exists(prev) {
		if exists(dir) {
			backupBankFolder(prev)
		} else {
			os.Rename(prev, dir)
		}
	}
}
