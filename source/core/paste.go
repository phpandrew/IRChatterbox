package core

// Long pastes as a link: the text goes to paste.rs (a free, public paste site) and only the
// link is sent to the channel. Used from the paste confirmation in the message box.

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	pasteURL      = "https://paste.rs/"
	pasteMaxBytes = 512 * 1024
)

type PasteResult struct {
	URL   string `json:"url"`
	Error string `json:"error,omitempty"`
}

var pasteClient = &http.Client{Timeout: 20 * time.Second}

// UploadPaste uploads text and returns its public link.
func (a *App) UploadPaste(text string) PasteResult {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if strings.TrimSpace(text) == "" {
		return PasteResult{Error: "Nothing to upload"}
	}
	if len(text) > pasteMaxBytes {
		return PasteResult{Error: "That paste is too big (512 KB at most)"}
	}
	u, err := uploadPaste(pasteURL, text)
	if err != nil {
		return PasteResult{Error: "Couldn't upload the paste: " + err.Error()}
	}
	return PasteResult{URL: u}
}

func uploadPaste(endpoint, text string) (string, error) {
	req, _ := http.NewRequest("POST", endpoint, strings.NewReader(text))
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	req.Header.Set("User-Agent", "IRChatterbox/"+appVersion)
	resp, err := pasteClient.Do(req)
	if err != nil {
		return "", errors.New("the paste site couldn't be reached")
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	link := strings.TrimSpace(string(b))
	if resp.StatusCode == 206 {
		return "", errors.New("the paste was too big for the paste site")
	}
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return "", errors.New("the paste site said " + resp.Status)
	}
	if !strings.HasPrefix(link, "https://") || strings.ContainsAny(link, " \r\n") {
		return "", errors.New("the paste site sent an unexpected reply")
	}
	return link, nil
}
