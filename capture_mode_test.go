//go:build linux

package main

import (
	"encoding/json/v2"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LarsArtmann/emeet-pixyd/internal/pixy"
	"github.com/starfederation/datastar-go/datastar"
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

func TestHandleCaptureModeConflictReturnsCurrentModeWhileRTSPIsActive(t *testing.T) {
	t.Parallel()

	d, _, server := newStreamDaemon(t)
	d.mu.Lock()
	d.rtspPublisher = &rtspPublisherProcess{done: make(chan struct{}), frames: newRTSPFrameFeed()}
	d.rtspState = rtspStatePublishing
	d.mu.Unlock()

	response := post(t, server.URL+"/api/capture-mode", "application/json", strings.NewReader(`{"mode":"4k30"}`))
	defer response.Body.Close()
	assertStatusCode(t, response, 409)

	var result captureModeResponse
	if err := json.Unmarshal([]byte(getBody(t, response)), &result); err != nil {
		t.Fatalf("decode capture mode conflict: %v", err)
	}
	if result.Mode != defaultCaptureModeID || !result.RTSPActive {
		t.Errorf("capture mode conflict response = %+v, want current mode %q and RTSP active", result, defaultCaptureModeID)
	}
	if got := d.currentCaptureMode(pixy.ModelOriginal).ID; got != defaultCaptureModeID {
		t.Errorf("capture mode after conflict = %q, want %q", got, defaultCaptureModeID)
	}
}

func TestWebCaptureModeSelectorDisabledWhileRTSPPublishing(t *testing.T) {
	t.Parallel()

	d, _, server := newStreamDaemon(t)
	d.mu.Lock()
	d.rtspPublisher = &rtspPublisherProcess{done: make(chan struct{}), frames: newRTSPFrameFeed()}
	d.rtspState = rtspStatePublishing
	d.mu.Unlock()

	response := get(t, server.URL+"/")
	defer response.Body.Close()
	page := getBody(t, response)
	start := strings.Index(page, `<select id="capture-mode-select"`)
	if start < 0 {
		t.Fatal("capture mode selector not found")
	}
	end := strings.Index(page[start:], ">")
	if end < 0 || !strings.Contains(page[start:start+end], `disabled=""`) {
		t.Fatal("capture mode selector should be disabled during RTSP publishing")
	}
}

func TestPatchPanelRefreshesCaptureSelectorWithRTSPState(t *testing.T) {
	t.Parallel()

	mode, ok := captureModeForModel(pixy.ModelOriginal, defaultCaptureModeID)
	if !ok {
		t.Fatalf("default capture mode %q not found", defaultCaptureModeID)
	}

	for _, publishing := range []bool{false, true} {
		t.Run(map[bool]string{false: "paused", true: "publishing"}[publishing], func(t *testing.T) {
			t.Parallel()

			status := webStatus{
				Online:         true,
				Device:         "/dev/video0",
				CaptureMode:    mode,
				CaptureModes:   captureModesForModel(pixy.ModelOriginal),
				RTSPPublishing: publishing,
			}
			response := httptest.NewRecorder()
			request := httptest.NewRequest("GET", "/events", nil)
			server := &webServer{}
			server.patchPanel(datastar.NewSSE(response, request), status)

			body := response.Body.String()
			patchStart := strings.LastIndex(body, `id="preview-resolution"`)
			if patchStart < 0 {
				t.Fatal("SSE did not patch the preview resolution control")
			}
			selectStart := strings.Index(body[patchStart:], `<select id="capture-mode-select"`)
			if selectStart < 0 {
				t.Fatal("preview resolution patch omitted the mode selector")
			}
			selectStart += patchStart
			selectEnd := strings.Index(body[selectStart:], ">")
			if selectEnd < 0 {
				t.Fatal("mode selector opening tag is incomplete")
			}
			hasDisabled := strings.Contains(body[selectStart:selectStart+selectEnd], `disabled=""`)
			if hasDisabled != publishing {
				t.Errorf("selector disabled = %t, RTSP publishing = %t", hasDisabled, publishing)
			}
		})
	}
}

func TestRTSPPausePanelPatchReenablesResolutionSelector(t *testing.T) {
	t.Parallel()

	d, _, _ := newStreamDaemon(t)
	d.mu.Lock()
	d.rtspState = rtspStatePaused
	d.mu.Unlock()
	server := &webServer{daemon: d}
	status := server.getWebStatusWithPTZ(t.Context())
	response := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/events", nil)
	server.patchPanel(datastar.NewSSE(response, request), status)

	body := response.Body.String()
	patchStart := strings.LastIndex(body, `id="preview-resolution"`)
	if patchStart < 0 {
		t.Fatal("pause response did not patch preview resolution controls")
	}
	selectStart := strings.Index(body[patchStart:], `<select id="capture-mode-select"`)
	if selectStart < 0 {
		t.Fatal("pause response omitted capture mode selector")
	}
	selectStart += patchStart
	selectEnd := strings.Index(body[selectStart:], ">")
	if selectEnd < 0 {
		t.Fatal("capture mode selector opening tag is incomplete")
	}
	if strings.Contains(body[selectStart:selectStart+selectEnd], `disabled=""`) {
		t.Fatal("capture mode selector remains disabled after RTSP pause")
	}
}
