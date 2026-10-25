//go:build phpsync

package core

// Runs two devices against a real account site (php -S + MariaDB):
//   SYNC_API=http://127.0.0.1:8781/account/api.php SYNC_MAIL=/path/mail.log go test -tags phpsync -run PHP .

import (
	"os"
	"regexp"
	"testing"
)

func phpSignIn(t *testing.T, a *App, api, email string) SyncResult {
	if r := a.SyncSignInStart(api, email); r.Error != "" {
		t.Fatal(r.Error)
	}
	b, _ := os.ReadFile(os.Getenv("SYNC_MAIL"))
	codes := regexp.MustCompile(`(?m)^    (\d{6})$`).FindAllStringSubmatch(string(b), -1)
	r := a.SyncSignInFinish(codes[len(codes)-1][1])
	if r.Error != "" {
		t.Fatal(r.Error)
	}
	return r
}

func TestSyncAgainstPHP(t *testing.T) {
	api := os.Getenv("SYNC_API")
	a, b := newBackupApp(t), newBackupApp(t)
	a.SetNickColor("bob", "#ff8800")
	phpSignIn(t, a, api, "php-test@example.com")
	start := a.SyncStart(nil)
	if start.Error != "" {
		t.Fatal(start.Error)
	}
	if r := phpSignIn(t, b, api, "php-test@example.com"); !r.HasData {
		t.Fatal("no data")
	}
	if r := b.SyncJoin(start.Code, nil); r.Error != "" {
		t.Fatal(r.Error)
	}
	if b.store.Get().NickColors["bob"] != "#ff8800" {
		t.Fatal("colour didn't arrive")
	}
	b.SetNickColor("carol", "#00aa00")
	mustCycle(t, b, a)
	if a.store.Get().NickColors["carol"] != "#00aa00" {
		t.Fatal("colour didn't come back")
	}
	if st := a.GetSyncStatus(); len(st.Devices) != 2 {
		t.Fatalf("%+v", st.Devices)
	}
	b.SyncStop()
	a.SyncStop()
}
