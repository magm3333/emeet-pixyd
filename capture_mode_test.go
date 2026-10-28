//go:build linux

package main

import (
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/LarsArtmann/emeet-pixyd/internal/pixy"
)

func TestCaptureModesForModel(t *testing.T) {
	t.Parallel()

	if _, ok := captureModeForModel(pixy.ModelOriginal, defaultCaptureModeID); !ok {
		t.Fatalf("default mode %q is not available for PIXY", defaultCaptureModeID)
	}
	if mode, ok := captureModeForModel(pixy.ModelOriginal, "4k30"); !ok || mode.ZoomAvailable {
		t.Errorf("4k30 mode = %+v, available %t; want available without zoom", mode, ok)
	}
	if mode, ok := captureModeForModel(pixy.ModelOriginal, "1080p60"); !ok || mode.ZoomAvailable {
		t.Errorf("1080p60 mode = %+v, available %t; want available without zoom", mode, ok)
	}
	if mode, ok := captureModeForModel(pixy.ModelOriginal, defaultCaptureModeID); !ok || !mode.ZoomAvailable {
		t.Errorf("default mode = %+v, available %t; want zoom-compatible 1080p30", mode, ok)
	}
	if _, ok := captureModeForModel(pixy.Model2K, "4k30"); ok {
		t.Error("4k30 should not be offered for the PIXY 2K")
	}
}

func TestHandleCaptureModeChangesActiveProfile(t *testing.T) {
	t.Parallel()

	d, _, server := newStreamDaemon(t)
	response := post(t, server.URL+"/api/capture-mode", "application/json", strings.NewReader(`{"mode":"4k30"}`))
	defer response.Body.Close()
	assertStatusCode(t, response, 200)

	var result captureModeResponse
	if err := json.Unmarshal([]byte(getBody(t, response)), &result); err != nil {
		t.Fatalf("decode capture mode response: %v", err)
	}
	if result.Mode != "4k30" || result.ZoomAvailable {
		t.Errorf("capture mode response = %+v, want 4k30 with zoom unavailable", result)
	}
	if got := d.currentCaptureMode(pixy.ModelOriginal).ID; got != "4k30" {
		t.Errorf("current capture mode = %q, want 4k30", got)
	}

	page := get(t, server.URL+"/")
	defer page.Body.Close()
	assertResponseContains(t, page, `value="4k30" selected`, "selected capture mode")
}

func TestHandleCaptureModeRejectsUnsupportedModelMode(t *testing.T) {
	t.Parallel()

	d, _, server := newStreamDaemon(t)
	d.mu.Lock()
	d.model = pixy.Model2K
	d.mu.Unlock()

	response := post(t, server.URL+"/api/capture-mode", "application/json", strings.NewReader(`{"mode":"4k30"}`))
	defer response.Body.Close()
	assertStatusCode(t, response, 400)
	if got := d.currentCaptureMode(pixy.Model2K).ID; got != defaultCaptureModeID {
		t.Errorf("capture mode after rejected request = %q, want %q", got, defaultCaptureModeID)
	}
}
