package core

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Image uploads: the UI hands over an image picked, pasted or dropped into the message box,
// and gets back a link to put in the message. Two hosts:
//   - "irchatterbox" (default): i.irchatterbox.com shrinks it to fit 1000×1000, saves it as WebP
//     and keeps it for 7 days.
//   - "imgur": uploaded as-is with the user's own Imgur Client ID.

const (
	UploadHostIRChatterbox = "irchatterbox"
	UploadHostImgur        = "imgur"
	UploadHostCustom       = "custom" // your own server with the same API (Settings → Media)

	maxUploadBytes = 10 << 20 // also the limit on i.irchatterbox.com
)

// Endpoints are variables so tests can point them at a local server.
var (
	irchatterboxUploadURL = "https://i.irchatterbox.com/upload"
	imgurUploadURL        = "https://api.imgur.com/3/image"
	uploadClient          = &http.Client{Timeout: 90 * time.Second}
)

// UploadResult is what the UI gets back: the link to post, and when it stops working ("" = never).
type UploadResult struct {
	URL     string `json:"url"`
	Expires string `json:"expires"`
	Host    string `json:"host"`
}

var uploadTypes = map[string]string{
	"image/jpeg": "jpg",
	"image/png":  "png",
	"image/webp": "webp",
	"image/bmp":  "bmp",
}

// UploadImage uploads one image (base64, as read by the UI) to the host chosen in Settings → Media.
func (a *App) UploadImage(dataB64 string) (UploadResult, error) {
	set := a.store.Get().Settings
	if set.UploadHost == UploadHostCustom {
		endpoint := strings.TrimSpace(set.UploadURL)
		if p, err := url.Parse(endpoint); err != nil || p.Scheme != "https" || p.Host == "" {
			return UploadResult{}, errors.New("Set your upload server's https:// address in Settings → Media.")
		}
		return uploadImage(set.UploadHost, endpoint, dataB64)
	}
	return uploadImage(set.UploadHost, set.ImgurClientID, dataB64)
}

// uploadImage: for the custom host, extra is the server address; for Imgur, the Client ID.
func uploadImage(host, extra, dataB64 string) (UploadResult, error) {
	imgurClientID := extra
	// accept a whole data: URL as well as bare base64
	if i := strings.Index(dataB64, ";base64,"); i >= 0 && strings.HasPrefix(dataB64, "data:") {
		dataB64 = dataB64[i+len(";base64,"):]
	}
	if base64.StdEncoding.DecodedLen(len(dataB64)) > maxUploadBytes+3 {
		return UploadResult{}, errors.New("Images can be at most 10 MB.")
	}
	data, err := base64.StdEncoding.DecodeString(dataB64)
	if err != nil || len(data) == 0 {
		return UploadResult{}, errors.New("That file couldn't be read.")
	}
	if len(data) > maxUploadBytes {
		return UploadResult{}, errors.New("Images can be at most 10 MB.")
	}
	ext, ok := uploadTypes[http.DetectContentType(data)]
	if !ok {
		return UploadResult{}, errors.New("Only JPG, PNG, WebP and BMP images can be uploaded.")
	}

	switch host {
	case UploadHostImgur:
		return uploadToImgur(imgurClientID, data, ext)
	case "", UploadHostIRChatterbox:
		return uploadToIRChatterbox(irchatterboxUploadURL, data, ext)
	case UploadHostCustom:
		return uploadToIRChatterbox(extra, data, ext)
	default:
		return UploadResult{}, fmt.Errorf("Unknown image host %q. Pick one in Settings → Media.", host)
	}
}

// multipartImage builds a form with the image in field "image" (plus any extra fields). The
// file name is generic on purpose: the user's own file name never leaves the computer.
func multipartImage(data []byte, ext string, extra map[string]string) (*bytes.Buffer, string, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, v := range extra {
		if err := w.WriteField(k, v); err != nil {
			return nil, "", err
		}
	}
	part, err := w.CreateFormFile("image", "image."+ext)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(data); err != nil {
		return nil, "", err
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return &body, w.FormDataContentType(), nil
}

func postUpload(endpoint string, body *bytes.Buffer, contentType string, headers map[string]string) (*http.Response, []byte, error) {
	req, err := http.NewRequest(http.MethodPost, endpoint, body)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", "IRChatterbox/"+appVersion)
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := uploadClient.Do(req)
	if err != nil {
		u, _ := url.Parse(endpoint)
		return nil, nil, fmt.Errorf("Couldn't reach %s. Check your connection and try again.", u.Host)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, nil, errors.New("The upload was interrupted. Try again.")
	}
	return resp, b, nil
}

func uploadToIRChatterbox(endpoint string, data []byte, ext string) (UploadResult, error) {
	body, ct, err := multipartImage(data, ext, nil)
	if err != nil {
		return UploadResult{}, err
	}
	resp, b, err := postUpload(endpoint, body, ct, nil)
	if err != nil {
		return UploadResult{}, err
	}
	var r struct {
		OK      bool   `json:"ok"`
		URL     string `json:"url"`
		Expires string `json:"expires"`
		Error   string `json:"error"`
	}
	jsonErr := json.Unmarshal(b, &r)
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return UploadResult{}, errors.New("Too many uploads at once. Wait a minute and try again.")
	case jsonErr == nil && r.Error != "":
		return UploadResult{}, errors.New(r.Error)
	case resp.StatusCode != http.StatusOK || jsonErr != nil || !r.OK:
		return UploadResult{}, fmt.Errorf("The image host had a problem (%s). Try again in a moment.", resp.Status)
	}
	if !isHTTPSURL(r.URL) {
		return UploadResult{}, errors.New("The image host sent back an unexpected reply.")
	}
	return UploadResult{URL: r.URL, Expires: r.Expires, Host: UploadHostIRChatterbox}, nil
}

func uploadToImgur(clientID string, data []byte, ext string) (UploadResult, error) {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return UploadResult{}, errors.New("Add your Imgur Client ID in Settings → Media first.")
	}
	body, ct, err := multipartImage(data, ext, map[string]string{"type": "file"})
	if err != nil {
		return UploadResult{}, err
	}
	resp, b, err := postUpload(imgurUploadURL, body, ct, map[string]string{"Authorization": "Client-ID " + clientID})
	if err != nil {
		return UploadResult{}, err
	}
	var r struct {
		Success bool `json:"success"`
		Data    struct {
			Link  string          `json:"link"`
			Error json.RawMessage `json:"error"` // a string, or {"message": "..."}
		} `json:"data"`
	}
	jsonErr := json.Unmarshal(b, &r)
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return UploadResult{}, errors.New("Imgur didn't accept the Client ID. Check it in Settings → Media.")
	case resp.StatusCode == http.StatusTooManyRequests:
		return UploadResult{}, errors.New("Imgur's upload limit was reached. Try again later.")
	case jsonErr == nil && r.Success && isHTTPSURL(r.Data.Link):
		return UploadResult{URL: r.Data.Link, Host: UploadHostImgur}, nil
	}
	if msg := imgurError(r.Data.Error); msg != "" {
		return UploadResult{}, errors.New("Imgur: " + msg)
	}
	return UploadResult{}, fmt.Errorf("Imgur had a problem (%s). Try again in a moment.", resp.Status)
}

func imgurError(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var o struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &o) == nil {
		return o.Message
	}
	return ""
}

func isHTTPSURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "https" && u.Host != "" && !strings.ContainsAny(s, " \r\n")
}
