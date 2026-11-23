package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// Release notes: after an update the UI shows a "What's new" banner for the version now
// running, with the notes from its GitHub release.

type ReleaseNotes struct {
	Version   string `json:"version"`
	Name      string `json:"name"`
	Body      string `json:"body"` // GitHub-flavoured markdown, as written on the release
	URL       string `json:"url"`
	Published string `json:"published"`
	Current   string `json:"current"` // the version running now
	Error     string `json:"error,omitempty"`
}

var versionTagRe = regexp.MustCompile(`^[0-9]+(\.[0-9]+){0,3}$`)

// GetReleaseNotes fetches the notes for a release ("0.8.3"), or for the version running now
// when version is "". Dev builds, and versions without a release, get the latest release's.
func (a *App) GetReleaseNotes(version string) ReleaseNotes {
	if version == "" {
		version = appVersion
	}
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	rn := ReleaseNotes{Current: appVersion}
	var rel ghRelease
	var err error
	if versionTagRe.MatchString(version) {
		rel, err = fetchRelease("tags/v" + version)
		if errors.Is(err, errNoRelease) {
			rel, err = fetchRelease("latest")
		}
	} else {
		rel, err = fetchRelease("latest")
	}
	if err != nil {
		rn.Error = err.Error()
		return rn
	}
	rn.Version = strings.TrimPrefix(rel.TagName, "v")
	rn.Name = rel.Name
	rn.Body = rel.Body
	rn.URL = rel.HTMLURL
	rn.Published = rel.Published
	if len(rn.Body) > 64<<10 {
		rn.Body = rn.Body[:64<<10] + "\n\n…"
	}
	if !releaseURLOK(rn.URL) {
		rn.URL = "https://github.com/" + updateRepo + "/releases"
	}
	return rn
}

// DismissReleaseNotes puts the banner away for this version, or for good.
func (a *App) DismissReleaseNotes(version string, forever bool) {
	a.store.UpdateQuiet(func(c *Config) {
		c.Settings.ReleaseNotesSeen = version
		if forever {
			c.Settings.HideReleaseNotes = true
		}
	})
}

var errNoRelease = errors.New("no release found for this version")

func fetchRelease(which string) (ghRelease, error) {
	var rel ghRelease
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/"+updateRepo+"/releases/"+which, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "IRChatterbox/"+appVersion)
	resp, err := updateClient.Do(req)
	if err != nil {
		return rel, fmt.Errorf("couldn't reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return rel, errNoRelease
	}
	if resp.StatusCode != 200 {
		return rel, fmt.Errorf("GitHub returned %s", resp.Status)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rel); err != nil {
		return rel, err
	}
	if rel.Draft {
		return ghRelease{}, errNoRelease
	}
	return rel, nil
}
