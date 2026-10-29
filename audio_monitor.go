//go:build linux

package main

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

const pixyALSAInput = "plughw:CARD=PIXY,DEV=0"

type audioMonitorLogBuffer struct {
	bytes.Buffer
	limit int
}

func (b *audioMonitorLogBuffer) Write(p []byte) (int, error) {
	length := len(p)
	remaining := b.limit - b.Len()
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = b.Buffer.Write(p)
	}

	return length, nil
}

func audioMonitorFFmpegArgs(input string) []string {
	return []string{
		"-hide_banner",
		"-loglevel", "error",
		"-nostdin",
		"-f", "alsa",
		"-thread_queue_size", "256",
		"-i", input,
		"-vn",
		"-ac", "1",
		"-ar", "48000",
		"-c:a", "libopus",
		"-application", "audio",
		"-b:a", "64k",
		"-frame_duration", "20",
		"-flush_packets", "1",
		"-f", "ogg",
		"pipe:1",
	}
}

func (s *webServer) handleAudioMonitor(responseWriter http.ResponseWriter, request *http.Request) {
	s.daemon.audioMonitorMu.Lock()
	if s.daemon.audioMonitorActive {
		s.daemon.audioMonitorMu.Unlock()
		http.Error(responseWriter, "audio monitor is already active", http.StatusConflict)

		return
	}
	s.daemon.audioMonitorActive = true
	s.daemon.audioMonitorMu.Unlock()
	defer func() {
		s.daemon.audioMonitorMu.Lock()
		s.daemon.audioMonitorActive = false
		s.daemon.audioMonitorMu.Unlock()
	}()

	flusher, ok := responseWriter.(http.Flusher)
	if !ok {
		http.Error(responseWriter, "streaming is not supported", http.StatusInternalServerError)

		return
	}

	responseController := http.NewResponseController(responseWriter)
	if err := responseController.SetWriteDeadline(time.Time{}); err != nil {
		slog.Debug("could not clear write deadline for audio monitor", "error", err)
	}

	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		slog.Warn("audio monitor cannot find ffmpeg", "error", err)
		http.Error(responseWriter, "audio monitor unavailable", http.StatusServiceUnavailable)

		return
	}

	ctx, cancel := context.WithCancel(request.Context())
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpegPath, audioMonitorFFmpegArgs(pixyALSAInput)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		slog.Warn("audio monitor cannot create ffmpeg output", "error", err)
		http.Error(responseWriter, "audio monitor unavailable", http.StatusServiceUnavailable)

		return
	}
	stderr := &audioMonitorLogBuffer{limit: 2048}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		slog.Warn("audio monitor could not start ffmpeg", "error", err)
		http.Error(responseWriter, "audio monitor unavailable", http.StatusServiceUnavailable)

		return
	}
	waited := false
	defer func() {
		cancel()
		if !waited {
			logAudioMonitorFailure(ctx, cmd.Wait(), stderr.String())
		}
	}()

	buffer := make([]byte, 4096)
	n, readErr := stdout.Read(buffer)
	if n == 0 {
		waitErr := cmd.Wait()
		waited = true
		logAudioMonitorFailure(ctx, waitErr, stderr.String())
		http.Error(responseWriter, "PIXY audio input is unavailable", http.StatusServiceUnavailable)

		return
	}

	responseWriter.Header().Set("Content-Type", "audio/ogg; codecs=opus")
	responseWriter.Header().Set("Cache-Control", "no-store")
	responseWriter.Header().Set("X-Content-Type-Options", "nosniff")
	responseWriter.WriteHeader(http.StatusOK)

	for {
		if _, err := responseWriter.Write(buffer[:n]); err != nil {
			break
		}
		flusher.Flush()
		if readErr != nil {
			break
		}

		n, readErr = stdout.Read(buffer)
		if n == 0 && readErr != nil {
			break
		}
	}

	waitErr := cmd.Wait()
	waited = true
	logAudioMonitorFailure(ctx, waitErr, stderr.String())
}

func logAudioMonitorFailure(ctx context.Context, waitErr error, stderr string) {
	if ctx.Err() != nil || waitErr == nil {
		return
	}
	message := strings.TrimSpace(stderr)
	if len(message) > 512 {
		message = message[:512]
	}
	slog.Warn("audio monitor process exited", "error", waitErr, "ffmpeg_error", message)
}
