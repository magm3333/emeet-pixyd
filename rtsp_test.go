//go:build linux

package main

import (
	"bytes"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LarsArtmann/emeet-pixyd/internal/pixy"
)

func TestParseRTSPDestinationRedactsCredentials(t *testing.T) {
	t.Parallel()

	const raw = "rtsp://publisher:top-secret@nv:8554/org1/testpixy?token=hidden"
	target, public, err := parseRTSPDestination(raw)
	if err != nil {
		t.Fatalf("parseRTSPDestination: %v", err)
	}
	if target != raw {
		t.Errorf("target = %q, want original destination", target)
	}
	if public != "rtsp://nv:8554/org1/testpixy" {
		t.Errorf("public destination = %q", public)
	}

	redacted := redactRTSPError("publish failed for "+raw+": unauthorized", target, public)
	if strings.Contains(redacted, "top-secret") || strings.Contains(redacted, "token=hidden") {
		t.Errorf("redacted error leaked credentials: %q", redacted)
	}
	if !strings.Contains(redacted, public) {
		t.Errorf("redacted error does not retain safe destination: %q", redacted)
	}
}

func TestParseRTSPDestinationRejectsInvalidSchemesAndPaths(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"", "http://nv:8554/path", "rtsp://nv:8554", "rtsp://nv:8554/path\n-injected"} {
		if target, public, err := parseRTSPDestination(value); err == nil {
			t.Errorf("parseRTSPDestination(%q) = %q, %q, nil", value, target, public)
		}
	}
}

func TestRTSPDestinationPersistsWithPrivatePermissions(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t, pixy.StatePrivacy, "", "")
	want := "rtsp://publisher:top-secret@nv:8554/org1/testpixy"
	if err := d.saveRTSPURL(want); err != nil {
		t.Fatalf("saveRTSPURL: %v", err)
	}
	if got := d.loadRTSPURL(); got != want {
		t.Fatalf("loadRTSPURL() = %q, want saved URL", got)
	}

	info, err := os.Stat(d.config.StateFile() + rtspURLFileSuffix)
	if err != nil {
		t.Fatalf("stat saved destination: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("saved RTSP destination permissions = %04o, want 0600", got)
	}
}

func TestRTSPFFmpegArgsUseV4L2H264AndRTSPOverTCP(t *testing.T) {
	t.Parallel()

	mode := captureMode{ID: "1080p30", InputFormat: "mjpeg", Width: 1920, Height: 1080, FPS: 30}
	args := rtspFFmpegArgs("/dev/video0", mode, "rtsp://nv:8554/org1/testpixy")
	joined := strings.Join(args, " ")
	for _, expected := range []string{
		"-f v4l2", "-input_format mjpeg", "-video_size 1920x1080", "-framerate 30",
		"-c:v mjpeg", "-f image2pipe", "pipe:1", "-c:v libx264", "-tune zerolatency",
		"-rtsp_transport tcp", "rtsp://nv:8554/org1/testpixy",
	} {
		if !strings.Contains(joined, expected) {
			t.Errorf("ffmpeg args %q do not contain %q", joined, expected)
		}
	}
}

func TestRTSPFrameFeedBroadcastsToAllSubscribers(t *testing.T) {
	t.Parallel()

	feed := newRTSPFrameFeed()
	first, unsubscribeFirst := feed.subscribe()
	defer unsubscribeFirst()
	second, unsubscribeSecond := feed.subscribe()
	defer unsubscribeSecond()

	want := []byte{0xff, 0xd8, 0x01, 0xff, 0xd9}
	feed.publish(want)
	for i, subscriber := range []<-chan []byte{first, second} {
		select {
		case got := <-subscriber:
			if !bytes.Equal(got, want) {
				t.Errorf("subscriber %d received %v, want %v", i, got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d did not receive the frame", i)
		}
	}
}

func TestIsFFmpegProgressLine(t *testing.T) {
	t.Parallel()

	for _, line := range []string{"frame=12", "out_time_us=400000", "progress=end"} {
		if !isFFmpegProgressLine(line) {
			t.Errorf("isFFmpegProgressLine(%q) = false, want true", line)
		}
	}
	for _, line := range []string{"[rtsp @ 0x1234] method SETUP failed: 401 Unauthorized", "Connection refused"} {
		if isFFmpegProgressLine(line) {
			t.Errorf("isFFmpegProgressLine(%q) = true, want false", line)
		}
	}
}

func TestWebRTSPPanelAndInvalidURLFeedback(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t, pixy.StatePrivacy, "", "")
	server := newTestWebServer(t, d)

	pageResp := get(t, server.URL+"/")
	defer pageResp.Body.Close()
	page := getBody(t, pageResp)
	assertContainsAll(t, page, []string{
		`id="rtsp-panel"`,
		`id="rtsp-url-input"`,
		`type="password"`,
		`id="rtsp-start-btn"`,
		`data-i18n="rtsp.title"`,
	})

	resp := post(
		t,
		server.URL+"/api/rtsp/start",
		"application/json",
		strings.NewReader(`{"rtspUrl":"https://publisher:top-secret@nv:8554/org1/testpixy"}`),
	)
	defer resp.Body.Close()
	assertStatusCode(t, resp, http.StatusOK) // DataStar actions report errors via SSE.
	body := getBody(t, resp)
	assertContains(t, body, `id="rtsp-status"`, "invalid RTSP target status is rendered")
	assertContains(t, body, "rtsp.url_invalid", "invalid RTSP target error is visible")
	assertNotContains(t, body, "top-secret", "RTSP credentials are never echoed into the web response")
}

func TestWebRTSPPanelRestoresSavedDestinationIntoMaskedInput(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t, pixy.StatePrivacy, "", "")
	d.rtspSavedURL = "rtsp://publisher:top-secret@nv:8554/org1/testpixy"
	server := newTestWebServer(t, d)
	response := get(t, server.URL+"/")
	defer response.Body.Close()
	page := getBody(t, response)

	assertContainsAll(t, page, []string{`type="password"`, "rtspUrl", "top-secret"})
}
