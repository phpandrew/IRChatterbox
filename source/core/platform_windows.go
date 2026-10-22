//go:build windows

package core

import (
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"fyne.io/systray"
	toast "git.sr.ht/~jackmordaunt/go-toast/v2"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed assets/tray.ico
var trayIcon []byte

//go:embed assets/tray-alert.ico
var trayAlertIcon []byte

//go:embed assets/icon-128.png
var toastIconPNG []byte

var (
	procFlashWindowEx    = user32.NewProc("FlashWindowEx")
	procGetLastInputInfo = user32.NewProc("GetLastInputInfo")
	procGetTickCount     = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetTickCount")
)

// ---------- system tray ----------

type tray struct {
	mu       sync.Mutex
	ready    bool
	alerting bool
	stop     chan struct{}
}

var theTray = &tray{}

func (a *App) startTray() {
	go func() {
		runtime.LockOSThread() // the tray's window and message loop live on this thread
		systray.Run(func() {
			systray.SetIcon(trayIcon)
			systray.SetTitle("IRChatterbox")
			systray.SetTooltip("IRChatterbox")
			systray.SetOnTapped(func() { a.showWindow() })
			mShow := systray.AddMenuItem("Show IRChatterbox", "")
			systray.AddSeparator()
			mQuit := systray.AddMenuItem("Quit", "")
			theTray.mu.Lock()
			theTray.ready = true
			theTray.mu.Unlock()
			go func() {
				for {
					select {
					case <-mShow.ClickedCh:
						a.showWindow()
					case <-mQuit.ClickedCh:
						a.quitNow()
					}
				}
			}()
		}, nil)
	}()
}

func stopTray() {
	theTray.mu.Lock()
	ready := theTray.ready
	theTray.mu.Unlock()
	if ready {
		systray.Quit()
	}
}

// setTrayAttention starts/stops the tray icon flashing and updates its tooltip.
func setTrayAttention(on bool, tooltip string) {
	t := theTray
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.ready {
		return
	}
	systray.SetTooltip(tooltip)
	if on == t.alerting {
		return
	}
	t.alerting = on
	if !on {
		if t.stop != nil {
			close(t.stop)
			t.stop = nil
		}
		systray.SetIcon(trayIcon)
		return
	}
	stop := make(chan struct{})
	t.stop = stop
	go func() {
		alt := false
		tick := time.NewTicker(600 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				alt = !alt
				if alt {
					systray.SetIcon(trayAlertIcon)
				} else {
					systray.SetIcon(trayIcon)
				}
			}
		}
	}()
}

// ---------- taskbar flashing ----------

type flashInfo struct {
	CbSize    uint32
	Hwnd      uintptr
	DwFlags   uint32
	UCount    uint32
	DwTimeout uint32
}

func flashTaskbar(on bool) {
	h := mainHWND()
	if h == 0 {
		return
	}
	fi := flashInfo{Hwnd: h}
	fi.CbSize = uint32(unsafe.Sizeof(fi))
	if on {
		fi.DwFlags = 0x3 | 0xC // FLASHW_ALL | FLASHW_TIMERNOFG: flash until the window comes to the front
	}
	procFlashWindowEx.Call(uintptr(unsafe.Pointer(&fi)))
}

// ---------- idle time (for auto-away) ----------

func systemIdle() time.Duration {
	var lii struct {
		CbSize uint32
		DwTime uint32
	}
	lii.CbSize = uint32(unsafe.Sizeof(lii))
	if r, _, _ := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&lii))); r == 0 {
		return 0
	}
	now, _, _ := procGetTickCount.Call()
	return time.Duration(uint32(now)-lii.DwTime) * time.Millisecond
}

// ---------- update restart ----------

// waitForPid blocks until the given process exits (used when an update restarts the app).
func waitForPid(pid uint32) {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	windows.WaitForSingleObject(h, 15000)
}

// ---------- toasts ----------

// pushToast shows a Windows notification. It goes through go-toast directly (rather than
// Wails' SendNotification) so the Windows sound can be silenced when the app plays its own.
// The payload format matches Wails', so clicking it still reaches OnNotificationResponse.
func (a *App) pushToast(title, body string, data map[string]any, silent bool) error {
	ensureToastIcon()
	payload := map[string]any{
		"action":  "DEFAULT_ACTION",
		"payload": map[string]any{"id": newID(), "title": title, "body": body, "data": data},
	}
	j, _ := json.Marshal(payload)
	n := toast.Notification{
		Title:               toastText(title),
		Body:                toastText(body),
		ActivationType:      toast.Foreground,
		ActivationArguments: base64.StdEncoding.EncodeToString(j),
		Audio:               toast.Default,
	}
	if silent {
		n.Audio = toast.Silent
	}
	return n.Push()
}

var toastIconOnce sync.Once

// ensureToastIcon writes the icon where Wails' toast setup expects it (it normally does
// this lazily on its first notification, which we bypass).
func ensureToastIcon() {
	toastIconOnce.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			return
		}
		app := filepath.Base(exe)
		k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes\AppUserModelId\`+app, registry.QUERY_VALUE)
		if err != nil {
			return
		}
		guid, _, err := k.GetStringValue("CustomActivator")
		k.Close()
		if err != nil || guid == "" {
			return
		}
		p := filepath.Join(os.TempDir(), app+guid+".png")
		if _, err := os.Stat(p); err != nil {
			os.WriteFile(p, toastIconPNG, 0o644)
		}
	})
}

var _ = wruntime.WindowShow
