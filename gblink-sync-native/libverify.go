// Checking the games in the library against No-Intro's list of good dumps. Each ROM is hashed
// once in the background; hashes are remembered (by file size and date) in the settings folder.
package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type hashEntry struct {
	Size    int64  `json:"size"`
	ModTime int64  `json:"mtime"`
	SHA1    string `json:"sha1"`
	CRC     string `json:"crc"`
}

type hashCache struct {
	mu      sync.Mutex
	m       map[string]hashEntry
	queue   []string
	queued  map[string]bool
	running bool
}

var hashes = &hashCache{queued: map[string]bool{}}

func hashCachePath() string { return filepath.Join(configDir(), "rom-hashes.json") }

func (h *hashCache) load() {
	if h.m != nil {
		return
	}
	h.m = map[string]hashEntry{}
	if b, err := os.ReadFile(hashCachePath()); err == nil {
		json.Unmarshal(b, &h.m)
	}
}

func (h *hashCache) save() {
	b, _ := json.Marshal(h.m)
	os.WriteFile(hashCachePath(), b, 0o644)
}

// get returns the remembered hash if the file hasn't changed since.
func (h *hashCache) get(path string) (hashEntry, bool) {
	st, err := os.Stat(path)
	if err != nil {
		return hashEntry{}, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.load()
	e, ok := h.m[strings.ToLower(path)]
	return e, ok && e.Size == st.Size() && e.ModTime == st.ModTime().UnixNano()
}

func hashFile(path string) (hashEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return hashEntry{}, err
	}
	defer f.Close()
	st, _ := f.Stat()
	s, c := sha1.New(), crc32.NewIEEE()
	if _, err := io.Copy(io.MultiWriter(s, c), f); err != nil {
		return hashEntry{}, err
	}
	return hashEntry{Size: st.Size(), ModTime: st.ModTime().UnixNano(), SHA1: hex.EncodeToString(s.Sum(nil)), CRC: fmt.Sprintf("%08x", c.Sum32())}, nil
}

// queue hashes these ROMs in the background, then refreshes the library.
func (h *hashCache) enqueue(a *App, paths []string) {
	h.mu.Lock()
	for _, p := range paths {
		if !h.queued[p] {
			h.queued[p] = true
			h.queue = append(h.queue, p)
		}
	}
	start := !h.running && len(h.queue) > 0
	if start {
		h.running = true
	}
	h.mu.Unlock()
	if !start {
		return
	}
	go func() {
		done := 0
		for {
			h.mu.Lock()
			if len(h.queue) == 0 {
				h.running = false
				h.save()
				h.mu.Unlock()
				break
			}
			p := h.queue[0]
			h.queue = h.queue[1:]
			h.mu.Unlock()
			e, err := hashFile(p)
			h.mu.Lock()
			delete(h.queued, p)
			if err == nil {
				h.load()
				h.m[strings.ToLower(p)] = e
			}
			h.mu.Unlock()
			if done++; done%5 == 0 {
				a.refreshLibrary() // show results as they come
			}
		}
		a.refreshLibrary()
	}()
}

// classify a ROM's hash: ok (a good dump), bad (the game's known but this isn't a good dump of
// it) or unknown (not in the list); name is the No-Intro name for ok.
func classify(sha1, code string) (status, name string) {
	dat := currentDAT()
	if e := dat.Lookup(sha1); e != nil {
		return "ok", e.Name
	}
	if len(dat.WithSerial(code)) > 0 {
		return "bad", ""
	}
	return "unknown", ""
}
