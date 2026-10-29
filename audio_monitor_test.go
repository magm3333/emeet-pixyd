//go:build linux

package main

import (
	"strings"
	"testing"
)

func TestAudioMonitorFFmpegArgsCapturePIXYAndStreamOpus(t *testing.T) {
	t.Parallel()

	args := strings.Join(audioMonitorFFmpegArgs(pixyALSAInput), " ")
	for _, want := range []string{
		"-f alsa",
		"-i " + pixyALSAInput,
		"-c:a libopus",
		"-f ogg",
		"pipe:1",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("audio monitor FFmpeg args %q missing %q", args, want)
		}
	}
}

func TestAudioMonitorRejectsSecondListener(t *testing.T) {
	t.Parallel()

	d, _, server := newStreamDaemon(t)
	d.audioMonitorMu.Lock()
	d.audioMonitorActive = true
	d.audioMonitorMu.Unlock()

	response := get(t, server.URL+"/api/audio/monitor")
	defer response.Body.Close()
	assertStatusCode(t, response, 409)
}
