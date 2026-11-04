package login

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveDeviceStart checks the real GitHub device endpoint returns a code.
func TestLiveDeviceStart(t *testing.T) {
	if os.Getenv("SEARCH_TUI_LIVE") == "" {
		t.Skip("set SEARCH_TUI_LIVE=1 to hit the network")
	}
	p, _ := Lookup("github")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	dc, err := StartDevice(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dc.VerificationURI, "github.com/login/device") || len(dc.UserCode) < 6 {
		t.Fatalf("device code = %+v", dc)
	}
	t.Logf("user code %s at %s (interval %s, expires %s)", dc.UserCode, dc.VerificationURI, dc.Interval, dc.ExpiresIn)
}

// TestLiveKagiValidate checks the stored key against the real Search API.
func TestLiveKagiValidate(t *testing.T) {
	if os.Getenv("SEARCH_TUI_LIVE") == "" {
		t.Skip("set SEARCH_TUI_LIVE=1 to hit the network")
	}
	key := strings.TrimSpace(os.Getenv("SEARCH_TUI_KAGI_KEY"))
	if key == "" {
		t.Skip("set SEARCH_TUI_KAGI_KEY")
	}
	p, _ := Lookup("kagi")
	if _, _, _, err := Validate(context.Background(), p, map[string]string{"credential": key}); err != nil {
		t.Fatal(err)
	}
}
