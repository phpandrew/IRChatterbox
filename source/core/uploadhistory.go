package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Upload history (Settings → Media): the images this device uploaded, newest first, with
// whatever the host gave back for deleting them. Kept in <data>/uploads.json on this device
// only: it's not synced or backed up, because the delete codes can delete the images.

const maxUploadHistory = 300

type uploadRecord struct {
	URL     string `json:"url"`
	Host    string `json:"host"`
	At      string `json:"at"`                // RFC 3339, UTC
	Expires string `json:"expires,omitempty"` // when the host deletes it by itself

	DeleteURL   string `json:"deleteURL,omitempty"`
	DeleteToken string `json:"deleteToken,omitempty"`
	ImageID     string `json:"imageId,omitempty"`
	ImgurHash   string `json:"imgurDeleteHash,omitempty"`
	CatboxFile  string `json:"catboxFile,omitempty"`
}

// UploadItem is one row of the history as the UI sees it (no delete codes).
type UploadItem struct {
	URL       string `json:"url"`
	Host      string `json:"host"`
	At        string `json:"at"`
	Expires   string `json:"expires"`
	Expired   bool   `json:"expired"`
	CanDelete bool   `json:"canDelete"`
	// why it can't be deleted, when it can't: "expired", "anonymous" (Catbox without a
	// userhash), "unsupported" (the host gave nothing to delete with)
	NoDelete string `json:"noDelete,omitempty"`
}

var uploadHistMu sync.Mutex

func (a *App) uploadHistoryPath() string { return filepath.Join(a.store.dir, "uploads.json") }

func (a *App) loadUploads() []uploadRecord {
	var list []uploadRecord
	if b, err := os.ReadFile(a.uploadHistoryPath()); err == nil {
		_ = json.Unmarshal(b, &list)
	}
	return list
}

func (a *App) saveUploads(list []uploadRecord) error {
	if len(list) > maxUploadHistory {
		list = list[:maxUploadHistory]
	}
	b, err := json.MarshalIndent(list, "", " ")
	if err != nil {
		return err
	}
	tmp := a.uploadHistoryPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, a.uploadHistoryPath())
}

// recordUpload adds a finished upload to the top of the history.
func (a *App) recordUpload(r UploadResult) {
	if r.URL == "" {
		return
	}
	uploadHistMu.Lock()
	defer uploadHistMu.Unlock()
	rec := uploadRecord{URL: r.URL, Host: r.Host, At: time.Now().UTC().Format(time.RFC3339), Expires: r.Expires,
		DeleteURL: r.del.URL, DeleteToken: r.del.Token, ImageID: r.del.ID, ImgurHash: r.del.ImgurHash, CatboxFile: r.del.CatboxFile}
	list := append([]uploadRecord{rec}, a.loadUploads()...)
	if err := a.saveUploads(list); err != nil {
		log.Printf("upload history: %v", err)
	}
}

func (r uploadRecord) expired(now time.Time) bool {
	t, err := time.Parse(time.RFC3339, r.Expires)
	return err == nil && now.After(t)
}

func (r uploadRecord) item(now time.Time) UploadItem {
	it := UploadItem{URL: r.URL, Host: r.Host, At: r.At, Expires: r.Expires, Expired: r.expired(now)}
	switch {
	case it.Expired:
		it.NoDelete = "expired"
	case r.DeleteToken != "" || r.ImgurHash != "" || r.CatboxFile != "":
		it.CanDelete = true
	case r.Host == UploadHostCatbox:
		it.NoDelete = "anonymous"
	default:
		it.NoDelete = "unsupported"
	}
	return it
}

// GetUploadHistory lists this device's uploads, newest first.
func (a *App) GetUploadHistory() []UploadItem {
	uploadHistMu.Lock()
	defer uploadHistMu.Unlock()
	now := time.Now()
	out := []UploadItem{}
	for _, r := range a.loadUploads() {
		out = append(out, r.item(now))
	}
	return out
}

// ForgetUpload removes one image from the history without deleting it from the host.
func (a *App) ForgetUpload(link string) {
	uploadHistMu.Lock()
	defer uploadHistMu.Unlock()
	a.dropUpload(link)
}

// ClearUploadHistory forgets every upload (the images stay on their hosts).
func (a *App) ClearUploadHistory() {
	uploadHistMu.Lock()
	defer uploadHistMu.Unlock()
	if err := os.Remove(a.uploadHistoryPath()); err != nil && !os.IsNotExist(err) {
		log.Printf("upload history: %v", err)
	}
}

func (a *App) dropUpload(link string) {
	list := a.loadUploads()
	keep := list[:0]
	for _, r := range list {
		if r.URL != link {
			keep = append(keep, r)
		}
	}
	if err := a.saveUploads(keep); err != nil {
		log.Printf("upload history: %v", err)
	}
}

// DeleteUpload deletes an image from its host and from the history. Returns "" when done,
// or a message to show. An image the host says is already gone counts as deleted.
func (a *App) DeleteUpload(link string) string {
	uploadHistMu.Lock()
	var rec *uploadRecord
	for _, r := range a.loadUploads() {
		if r.URL == link {
			r := r
			rec = &r
			break
		}
	}
	uploadHistMu.Unlock()
	if rec == nil {
		return "That image isn't in your upload history."
	}
	if it := rec.item(time.Now()); !it.CanDelete {
		switch it.NoDelete {
		case "expired":
			return "That image has expired and was already deleted by the host."
		case "anonymous":
			return "Catbox can't delete uploads made without a userhash."
		}
		return "This host didn't give a way to delete the image."
	}
	err := a.deleteFromHost(*rec)
	if errors.Is(err, errAlreadyGone) {
		err = nil
	}
	if err != nil {
		return err.Error()
	}
	uploadHistMu.Lock()
	a.dropUpload(link)
	uploadHistMu.Unlock()
	return ""
}

var (
	errAlreadyGone = errors.New("already gone")
	imgurHashRe    = regexp.MustCompile(`^[A-Za-z0-9]{1,64}$`)
	catboxFileRe   = regexp.MustCompile(`^[A-Za-z0-9]{1,32}\.[A-Za-z0-9]{1,8}$`)
)

func (a *App) deleteFromHost(r uploadRecord) error {
	switch {
	case r.DeleteToken != "":
		return deleteFromIRChatterbox(r)
	case r.ImgurHash != "":
		return deleteFromImgur(a.store.Get().Settings.ImgurClientID, r.ImgurHash)
	case r.CatboxFile != "":
		return deleteFromCatbox(a.store.Get().Settings.CatboxUserHash, r.CatboxFile)
	}
	return errors.New("This host didn't give a way to delete the image.")
}

func deleteFromIRChatterbox(r uploadRecord) error {
	if !isHTTPSURL(r.DeleteURL) || r.ImageID == "" {
		return errors.New("The delete address for this image isn't valid.")
	}
	form := url.Values{"id": {r.ImageID}, "token": {r.DeleteToken}}
	req, err := http.NewRequest(http.MethodPost, r.DeleteURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	b, status, err := doDelete(req)
	if err != nil {
		return err
	}
	var reply struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	jsonErr := json.Unmarshal(b, &reply)
	switch {
	case status == http.StatusOK && jsonErr == nil && reply.OK:
		return nil
	case status == http.StatusNotFound && jsonErr == nil:
		return errAlreadyGone
	case jsonErr == nil && reply.Error != "" && len(reply.Error) < 200:
		return errors.New(reply.Error)
	case status == http.StatusTooManyRequests:
		return errors.New("Too many requests. Wait a minute and try again.")
	}
	return fmt.Errorf("The image host had a problem (%d). Try again in a moment.", status)
}

func deleteFromImgur(clientID, hash string) error {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return errors.New("Add your Imgur Client ID in Settings → Media to delete Imgur images.")
	}
	if !imgurHashRe.MatchString(hash) {
		return errors.New("The delete code for this image isn't valid.")
	}
	req, err := http.NewRequest(http.MethodDelete, strings.TrimRight(imgurUploadURL, "/")+"/"+hash, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Client-ID "+clientID)
	_, status, err := doDelete(req)
	if err != nil {
		return err
	}
	switch {
	case status == http.StatusOK:
		return nil
	case status == http.StatusNotFound:
		return errAlreadyGone
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return errors.New("Imgur didn't accept the Client ID. Check it in Settings → Media.")
	case status == http.StatusTooManyRequests:
		return errors.New("Imgur's limit was reached. Try again later.")
	}
	return fmt.Errorf("Imgur had a problem (%d). Try again in a moment.", status)
}

func deleteFromCatbox(userhash, file string) error {
	userhash = strings.TrimSpace(userhash)
	if userhash == "" {
		return errors.New("Add your Catbox userhash in Settings → Media to delete Catbox images.")
	}
	if !catboxFileRe.MatchString(file) {
		return errors.New("The file name for this image isn't valid.")
	}
	body, ct, err := multipartFields(map[string]string{"reqtype": "deletefiles", "userhash": userhash, "files": file})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, catboxUploadURL, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", ct)
	b, status, err := doDelete(req)
	if err != nil {
		return err
	}
	reply := strings.TrimSpace(string(b))
	low := strings.ToLower(reply)
	switch {
	case status == http.StatusOK && strings.Contains(low, "success"):
		return nil
	case strings.Contains(low, "userhash"):
		return errors.New("Catbox didn't accept the userhash. Check it in Settings → Media (it must be the account the image was uploaded with).")
	case strings.Contains(low, "doesn't exist") || strings.Contains(low, "does not exist") || strings.Contains(low, "not found"):
		return errAlreadyGone
	case status < 500 && reply != "" && len(reply) < 200 && !strings.ContainsAny(reply, "<>"):
		return errors.New("Catbox: " + reply)
	}
	return fmt.Errorf("Catbox had a problem (%d). Try again in a moment.", status)
}

func doDelete(req *http.Request) ([]byte, int, error) {
	req.Header.Set("User-Agent", "IRChatterbox/"+appVersion)
	resp, err := uploadClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("Couldn't reach %s. Check your connection and try again.", req.URL.Host)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	return b, resp.StatusCode, nil
}

// multipartFields builds a form with text fields only (Catbox wants multipart even for deletes).
func multipartFields(fields map[string]string) (*bytes.Buffer, string, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			return nil, "", err
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return &body, w.FormDataContentType(), nil
}
