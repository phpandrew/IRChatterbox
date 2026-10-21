package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Shared by the desktop updater and Android's in-app update: download a release asset
// (hashing it on the way) and look up its expected hash in SHA256SUMS.txt.

func download(url, dest string, size int64, progress func(done, total int64)) (string, error) {
	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %s", resp.Status)
	}
	if resp.ContentLength > 0 {
		size = resp.ContentLength
	}
	f, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 256*1024)
	var done int64
	last := time.Now()
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return "", err
			}
			h.Write(buf[:n])
			done += int64(n)
			if time.Since(last) > 150*time.Millisecond {
				progress(done, size)
				last = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return "", rerr
		}
	}
	progress(size, size)
	return hex.EncodeToString(h.Sum(nil)), f.Sync()
}

func expectedSum(url, name string) (string, error) {
	resp, err := updateClient.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return f[0], nil
		}
	}
	return "", nil
}
