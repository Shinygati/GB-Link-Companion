// The app's version, and checking GitHub for a newer release
// (https://github.com/Shinygati/GB-Link-Companion/releases).
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// appVersion: bump it for each release; the release's tag should be the same ("v1.2.0").
const appVersion = "1.4.0"

const releasesAPI = "https://api.github.com/repos/Shinygati/GB-Link-Companion/releases/latest"
const releasesPage = "https://github.com/Shinygati/GB-Link-Companion/releases"

// UpdateView: what the window shows about updates.
type UpdateView struct {
	Checking bool
	Latest   string // the newest release's version, if known
	URL      string // its page
	Newer    bool   // newer than this one
	Text     string // the last check's result, for Settings
}

// versionParts: "v1.2.3" -> [1 2 3] (missing parts are 0; anything after a "-" or "+" is ignored)
func versionParts(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(strings.ToLower(v)), "v")
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	var out [3]int
	for i, p := range strings.SplitN(v, ".", 3) {
		n, _ := strconv.Atoi(p)
		out[i] = n
	}
	return out
}

func newerVersion(a, b string) bool { // a newer than b
	pa, pb := versionParts(a), versionParts(b)
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

// checkUpdates asks GitHub for the latest release. manual: from the Settings button (says
// what it found either way); otherwise quiet unless there's a newer one.
func (a *App) checkUpdates(manual bool) {
	a.ui.update(func(st *State) { st.Update.Checking = true })
	latest, url, err := latestRelease()
	a.ui.update(func(st *State) {
		st.Update.Checking = false
		switch {
		case err != nil:
			st.Update.Text = "Couldn't check for updates (" + err.Error() + ")."
		case latest == "":
			st.Update.Text = "No releases yet."
		default:
			st.Update.Latest, st.Update.URL = strings.TrimPrefix(latest, "v"), url
			st.Update.Newer = newerVersion(latest, appVersion)
			if st.Update.Newer {
				st.Update.Text = "Version " + st.Update.Latest + " is out."
			} else {
				st.Update.Text = "You have the latest version."
			}
		}
		st.Update.Text += " (checked " + time.Now().Format("3:04 pm") + ")"
	})
	if manual {
		a.ui.update(func(st *State) {
			if st.Update.Newer {
				st.Toast, st.ToastUntil = "Version "+st.Update.Latest+" is out", time.Now().Add(4*time.Second)
			}
		})
	}
}

func latestRelease() (version, url string, err error) {
	req, _ := http.NewRequest("GET", releasesAPI, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "GB-Link-Companion/"+appVersion)
	c := &http.Client{Timeout: 10 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("no connection")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound { // no releases (or the repository is private)
		return "", "", nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("GitHub said %s", resp.Status)
	}
	var r struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Draft   bool   `json:"draft"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", "", fmt.Errorf("couldn't read GitHub's answer")
	}
	if r.HTMLURL == "" {
		r.HTMLURL = releasesPage
	}
	return r.TagName, r.HTMLURL, nil
}
