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
	"path"
	"strings"
	"time"
)

// Image uploads: the UI hands over an image picked, pasted or dropped into the message box,
// and gets back a link to put in the message. Two hosts:
//   - "irchatterbox" (default): i.irchatterbox.com shrinks it to fit 1000×1000, saves it as WebP
//     and keeps it for 7 days.
//   - "imgur": uploaded as-is with the user's own Imgur Client ID.
//   - "catbox": uploaded as-is to catbox.moe, kept until deleted; with the user's userhash
//     it lands in their Catbox account (where they can delete it), without one it's anonymous.

const (
	UploadHostIRChatterbox = "irchatterbox"
	UploadHostImgur        = "imgur"
	UploadHostCatbox       = "catbox"
	UploadHostCustom       = "custom" // your own server with the same API (Settings → Media)

	maxUploadBytes = 10 << 20 // also the limit on i.irchatterbox.com
)

// Endpoints are variables so tests can point them at a local server.
var (
	irchatterboxUploadURL = "https://i.irchatterbox.com/upload"
	imgurUploadURL        = "https://api.imgur.com/3/image"
	catboxUploadURL       = "https://catbox.moe/user/api.php"
	uploadClient          = &http.Client{Timeout: 90 * time.Second}
)

// UploadResult is what the UI gets back: the link to post, and when it stops working ("" = never).
type UploadResult struct {
	URL     string `json:"url"`
	Expires string `json:"expires"`
	Host    string `json:"host"`

	del uploadDelete // how to delete it later; kept in the upload history, never sent to the UI
}

// uploadDelete is what a host hands back for deleting an upload.
type uploadDelete struct {
	URL, Token, ID string // irchatterbox / own server: POST id+token to URL
	ImgurHash      string // Imgur's deletehash
	CatboxFile     string // Catbox file name, when uploaded with a userhash
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
	var res UploadResult
	var err error
	switch set.UploadHost {
	case UploadHostCustom:
		endpoint := strings.TrimSpace(set.UploadURL)
		if p, perr := url.Parse(endpoint); perr != nil || p.Scheme != "https" || p.Host == "" {
			return UploadResult{}, errors.New("Set your upload server's https:// address in Settings → Media.")
		}
		res, err = uploadImage(set.UploadHost, endpoint, dataB64)
	case UploadHostCatbox:
		res, err = uploadImage(set.UploadHost, set.CatboxUserHash, dataB64)
	default:
		res, err = uploadImage(set.UploadHost, set.ImgurClientID, dataB64)
	}
	if err == nil {
		a.recordUpload(res)
	}
	return res, err
}

// uploadImage: for the custom host, extra is the server address; for Imgur, the Client ID;
// for Catbox, the userhash (optional).
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
	case UploadHostCatbox:
		return uploadToCatbox(extra, data, ext)
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
	return multipartFile("image", data, ext, extra)
}

func multipartFile(field string, data []byte, ext string, extra map[string]string) (*bytes.Buffer, string, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, v := range extra {
		if err := w.WriteField(k, v); err != nil {
			return nil, "", err
		}
	}
	part, err := w.CreateFormFile(field, "image."+ext)
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
		OK          bool   `json:"ok"`
		ID          string `json:"id"`
		URL         string `json:"url"`
		Expires     string `json:"expires"`
		Error       string `json:"error"`
		DeleteToken string `json:"delete_token"`
		DeleteURL   string `json:"delete_url"`
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
	res := UploadResult{URL: r.URL, Expires: r.Expires, Host: UploadHostIRChatterbox}
	// a delete link is only trusted on the server we uploaded to
	if r.DeleteToken != "" && r.ID != "" && len(r.DeleteToken) <= 200 && len(r.ID) <= 64 && isHTTPSURL(r.DeleteURL) && sameHost(r.DeleteURL, endpoint) {
		res.del = uploadDelete{URL: r.DeleteURL, Token: r.DeleteToken, ID: r.ID}
	}
	return res, nil
}

func sameHost(a, b string) bool {
	ua, err1 := url.Parse(a)
	ub, err2 := url.Parse(b)
	return err1 == nil && err2 == nil && ua.Host != "" && strings.EqualFold(ua.Host, ub.Host)
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
			Link       string          `json:"link"`
			DeleteHash string          `json:"deletehash"`
			Error      json.RawMessage `json:"error"` // a string, or {"message": "..."}
		} `json:"data"`
	}
	jsonErr := json.Unmarshal(b, &r)
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return UploadResult{}, errors.New("Imgur didn't accept the Client ID. Check it in Settings → Media.")
	case resp.StatusCode == http.StatusTooManyRequests:
		return UploadResult{}, errors.New("Imgur's upload limit was reached. Try again later.")
	case jsonErr == nil && r.Success && isHTTPSURL(r.Data.Link):
		return UploadResult{URL: r.Data.Link, Host: UploadHostImgur, del: uploadDelete{ImgurHash: r.Data.DeleteHash}}, nil
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

// uploadToCatbox posts to catbox.moe's API (reqtype=fileupload, fileToUpload), which answers
// with the file's link as plain text, or an error message.
func uploadToCatbox(userhash string, data []byte, ext string) (UploadResult, error) {
	fields := map[string]string{"reqtype": "fileupload"}
	if h := strings.TrimSpace(userhash); h != "" {
		fields["userhash"] = h
	}
	body, ct, err := multipartFile("fileToUpload", data, ext, fields)
	if err != nil {
		return UploadResult{}, err
	}
	resp, b, err := postUpload(catboxUploadURL, body, ct, map[string]string{"Accept": "text/plain"})
	if err != nil {
		return UploadResult{}, err
	}
	reply := strings.TrimSpace(string(b))
	if resp.StatusCode == http.StatusOK && isHTTPSURL(reply) && !strings.ContainsAny(reply, " \t\r\n<>\"") {
		if u, err := url.Parse(reply); err == nil && (u.Host == "catbox.moe" || strings.HasSuffix(u.Host, ".catbox.moe")) {
			res := UploadResult{URL: reply, Host: UploadHostCatbox}
			if fields["userhash"] != "" {
				res.del.CatboxFile = path.Base(u.Path)
			}
			return res, nil
		}
	}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return UploadResult{}, errors.New("Catbox is getting too many uploads. Wait a minute and try again.")
	case strings.HasPrefix(reply, "http://") || strings.HasPrefix(reply, "https://"):
		return UploadResult{}, errors.New("Catbox sent back an unexpected reply.")
	case strings.Contains(strings.ToLower(reply), "userhash"):
		return UploadResult{}, errors.New("Catbox didn't accept the userhash. Check it in Settings → Media, or leave it empty to upload anonymously.")
	case resp.StatusCode < 500 && reply != "" && len(reply) < 200 && !strings.ContainsAny(reply, "<>"):
		return UploadResult{}, errors.New("Catbox: " + reply)
	}
	return UploadResult{}, fmt.Errorf("Catbox had a problem (%s). Try again in a moment.", resp.Status)
}
