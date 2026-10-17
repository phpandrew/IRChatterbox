//go:build windows

package core

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// PowerShell (Windows PowerShell 5.1, which can call WinRT) reads the Windows media
// sessions — the same info shown in the volume flyout — for every player that reports it.
const smtcScript = `
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
Add-Type -AssemblyName System.Runtime.WindowsRuntime
$asTask = ([System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object { $_.Name -eq 'AsTask' -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation` + "`" + `1' })[0]
function Await($op, $type) { $t = $asTask.MakeGenericMethod($type).Invoke($null, @($op)); $t.Wait(-1) | Out-Null; $t.Result }
$null = [Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager, Windows.Media.Control, ContentType = WindowsRuntime]
$mgr = Await ([Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager]::RequestAsync()) ([Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager])
$out = @()
foreach ($s in $mgr.GetSessions()) {
  $p = Await ($s.TryGetMediaPropertiesAsync()) ([Windows.Media.Control.GlobalSystemMediaTransportControlsSessionMediaProperties])
  $out += [pscustomobject]@{ app = $s.SourceAppUserModelId; artist = $p.Artist; title = $p.Title; album = $p.AlbumTitle; status = $s.GetPlaybackInfo().PlaybackStatus.ToString() }
}
ConvertTo-Json -InputObject @($out) -Compress
`

type smtcSession struct {
	App    string `json:"app"`
	Artist string `json:"artist"`
	Title  string `json:"title"`
	Album  string `json:"album"`
	Status string `json:"status"`
}

func nowPlayingSMTC() (NowPlaying, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", smtcScript)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	out, err := cmd.Output()
	if err != nil {
		return NowPlaying{}, errors.New("Windows media controls unavailable")
	}
	var sessions []smtcSession
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &sessions); err != nil {
		return NowPlaying{}, errors.New("couldn't read Windows media controls")
	}
	// prefer MusicBee, then anything playing, then anything with a title
	best := -1
	score := func(s smtcSession) int {
		v := 0
		if s.Title != "" {
			v++
		}
		if s.Status == "Playing" {
			v += 2
		}
		if strings.Contains(strings.ToLower(s.App), "musicbee") {
			v += 4
		}
		return v
	}
	for i, s := range sessions {
		if s.Title != "" && (best < 0 || score(s) > score(sessions[best])) {
			best = i
		}
	}
	if best < 0 {
		return NowPlaying{}, errNothingPlaying
	}
	s := sessions[best]
	player := s.App
	if strings.Contains(strings.ToLower(player), "musicbee") {
		player = "MusicBee"
	} else {
		player = strings.TrimSuffix(filepath.Base(strings.ReplaceAll(player, "!", `\`)), ".exe")
	}
	return NowPlaying{Artist: s.Artist, Title: s.Title, Album: s.Album, Player: player, Raw: s.Artist + " - " + s.Title}, nil
}

var (
	user32                   = windows.NewLazySystemDLL("user32.dll")
	procEnumWindows          = user32.NewProc("EnumWindows")
	procGetWindowTextW       = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW = user32.NewProc("GetWindowTextLengthW")
	procIsWindowVisible      = user32.NewProc("IsWindowVisible")
)

// nowPlayingWindow reads the title of MusicBee's main window.
func nowPlayingWindow() (NowPlaying, error) {
	var titles []string
	cb := windows.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		if v, _, _ := procIsWindowVisible.Call(hwnd); v == 0 {
			return 1
		}
		var pid uint32
		windows.GetWindowThreadProcessId(windows.HWND(hwnd), &pid)
		h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
		if err != nil {
			return 1
		}
		buf := make([]uint16, windows.MAX_PATH)
		n := uint32(len(buf))
		err = windows.QueryFullProcessImageName(h, 0, &buf[0], &n)
		windows.CloseHandle(h)
		if err != nil || !strings.EqualFold(filepath.Base(windows.UTF16ToString(buf[:n])), "MusicBee.exe") {
			return 1
		}
		l, _, _ := procGetWindowTextLengthW.Call(hwnd)
		if l == 0 {
			return 1
		}
		tb := make([]uint16, l+1)
		procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&tb[0])), uintptr(len(tb)))
		titles = append(titles, windows.UTF16ToString(tb))
		return 1
	})
	procEnumWindows.Call(cb, 0)
	if len(titles) == 0 {
		return NowPlaying{}, errors.New("MusicBee isn't running")
	}
	// the main window is the one with the longest title (others are "MusicBee" or empty)
	best := titles[0]
	for _, t := range titles {
		if len(t) > len(best) {
			best = t
		}
	}
	return parseWindowTitle(best)
}
