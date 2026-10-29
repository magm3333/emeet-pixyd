//go:build linux

package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	errorfamily "github.com/larsartmann/go-error-family"
)

const (
	rtspShutdownTimeout = 4 * time.Second
	rtspErrorMaxBytes   = 512
	rtspURLFileSuffix   = ".rtsp-url"
)

type rtspPublishState string

const (
	rtspStateStopped    rtspPublishState = "stopped"
	rtspStateStarting   rtspPublishState = "starting"
	rtspStatePublishing rtspPublishState = "publishing"
	rtspStateStopping   rtspPublishState = "stopping"
	rtspStatePaused     rtspPublishState = "paused"
	rtspStateError      rtspPublishState = "error"
)

type rtspPublisherProcess struct {
	cancel        context.CancelFunc
	done          chan struct{}
	frames        *rtspFrameFeed
	stopRequested bool // guarded by d.rtspMu
}

type rtspFrameFeed struct {
	mu          sync.Mutex
	nextID      uint64
	subscribers map[uint64]chan []byte
}

func newRTSPFrameFeed() *rtspFrameFeed {
	return &rtspFrameFeed{subscribers: make(map[uint64]chan []byte)}
}

func (f *rtspFrameFeed) subscribe() (<-chan []byte, func()) {
	f.mu.Lock()
	f.nextID++
	id := f.nextID
	frames := make(chan []byte, 1)
	f.subscribers[id] = frames
	f.mu.Unlock()

	return frames, func() {
		f.mu.Lock()
		delete(f.subscribers, id)
		f.mu.Unlock()
	}
}

func (f *rtspFrameFeed) publish(frame []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, frames := range f.subscribers {
		select {
		case frames <- frame:
		default:
			select {
			case <-frames:
			default:
			}
			select {
			case frames <- frame:
			default:
			}
		}
	}
}

//nolint:gochecknoglobals // redacts RTSP userinfo from subprocess diagnostics
var rtspUserInfoPattern = regexp.MustCompile(`(?i)(rtsps?://)[^/\s@]+@`)

func parseRTSPDestination(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "\r\n\t\x00") {
		return "", "", errorfamily.NewRejection("rtsp.url_invalid", "enter a valid RTSP URL")
	}

	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "rtsp" && parsed.Scheme != "rtsps") || parsed.Hostname() == "" || parsed.Path == "" {
		return "", "", errorfamily.NewRejection("rtsp.url_invalid", "URL must use rtsp:// or rtsps:// and include a host and path")
	}

	destination := *parsed
	destination.User = nil
	destination.RawQuery = ""
	destination.Fragment = ""

	return parsed.String(), destination.String(), nil
}

func (d *Daemon) loadRTSPURL() string {
	path := d.config.StateFile() + rtspURLFileSuffix
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("failed to read saved RTSP destination", "error", err)
		}

		return ""
	}

	target, _, parseErr := parseRTSPDestination(string(data))
	if parseErr != nil {
		slog.Warn("saved RTSP destination is invalid; ignoring it", "error", parseErr)

		return ""
	}

	return target
}

func (d *Daemon) saveRTSPURL(target string) error {
	if err := d.ensureStateDir(); err != nil {
		return err
	}

	path := d.config.StateFile() + rtspURLFileSuffix
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(target), 0o600); err != nil {
		return errorfamily.WrapInfrastructuref(err, "rtsp.url_save", "save RTSP destination")
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		if removeErr := os.Remove(tmp); removeErr != nil && !os.IsNotExist(removeErr) {
			slog.Debug("failed to remove temporary RTSP destination", "error", removeErr)
		}

		return errorfamily.WrapInfrastructuref(err, "rtsp.url_permissions", "secure saved RTSP destination")
	}
	if err := os.Rename(tmp, path); err != nil {
		if removeErr := os.Remove(tmp); removeErr != nil && !os.IsNotExist(removeErr) {
			slog.Debug("failed to remove temporary RTSP destination", "error", removeErr)
		}

		return errorfamily.WrapInfrastructuref(err, "rtsp.url_rename", "replace saved RTSP destination")
	}

	return nil
}

func redactRTSPError(message, target, destination string) string {
	if target != "" {
		message = strings.ReplaceAll(message, target, destination)
	}
	message = rtspUserInfoPattern.ReplaceAllString(message, `${1}***@`)
	message = strings.TrimSpace(message)
	if len(message) > rtspErrorMaxBytes {
		message = message[:rtspErrorMaxBytes] + "…"
	}

	return message
}

func rtspFFmpegArgs(device string, mode captureMode, target string) []string {
	return []string{
		"-hide_banner",
		"-loglevel", "error",
		"-f", "v4l2",
		"-input_format", mode.InputFormat,
		"-video_size", fmt.Sprintf("%dx%d", mode.Width, mode.Height),
		"-framerate", strconv.Itoa(mode.FPS),
		"-i", device,
		"-map", "0:v:0",
		"-an",
		"-c:v", "mjpeg",
		"-q:v", "5",
		"-vf", "scale=640:-1",
		"-f", "image2pipe",
		"pipe:1",
		"-map", "0:v:0",
		"-an",
		"-c:v", "libx264",
		"-preset", "ultrafast",
		"-tune", "zerolatency",
		"-pix_fmt", "yuv420p",
		"-g", strconv.Itoa(mode.FPS),
		"-f", "rtsp",
		"-rtsp_transport", "tcp",
		"-rw_timeout", "5000000",
		"-progress", "pipe:2",
		"-stats_period", "1",
		"-nostats",
		target,
	}
}

func (d *Daemon) startRTSPPublish(rawTarget string) CommandResult {
	target, destination, parseErr := parseRTSPDestination(rawTarget)
	d.rtspMu.Lock()
	defer d.rtspMu.Unlock()

	d.mu.RLock()
	active := d.rtspPublisher != nil
	shuttingDown := d.routeShuttingDown
	videoDev := d.videoDev
	model := d.model
	captureModeID := d.captureModeID
	d.mu.RUnlock()

	if active {
		return errResultMsg("RTSP emission is already active")
	}
	if shuttingDown {
		return errResultMsg(respRouteDaemonStopping)
	}
	if parseErr != nil {
		d.setRTSPResultLocked(rtspStateError, "", parseErr.Error())
		d.broadcastStateChanged()

		return errResult("rtsp.url_invalid", parseErr)
	}
	if err := d.saveRTSPURL(target); err != nil {
		d.setRTSPResultLocked(rtspStateError, destination, err.Error())
		d.broadcastStateChanged()

		return errResult("rtsp.url_save", err)
	}
	d.mu.Lock()
	d.rtspSavedURL = target
	d.mu.Unlock()
	if videoDev == "" {
		err := errorfamily.NewInfrastructure("rtsp.camera_offline", "camera video device is unavailable")
		d.setRTSPResultLocked(rtspStateError, destination, err.Error())
		d.broadcastStateChanged()

		return errResult("rtsp.camera_offline", err)
	}
	if _, err := exec.LookPath(ffmpegBin); err != nil {
		wrapped := errorfamily.WrapInfrastructuref(err, "rtsp.ffmpeg_missing", "ffmpeg is required for RTSP publishing")
		d.setRTSPResultLocked(rtspStateError, destination, wrapped.Error())
		d.broadcastStateChanged()

		return errResult("rtsp.ffmpeg_missing", wrapped)
	}

	// Mark the device as reserved before cancelling the preview. A new HTTP
	// stream that races with this transition will observe rtspStateStarting
	// and decline to open V4L2.
	d.mu.Lock()
	d.rtspState = rtspStateStarting
	d.rtspDestination = destination
	d.rtspError = ""
	activeStream := d.activeVideoStream
	d.mu.Unlock()
	d.broadcastStateChanged()

	if activeStream != nil {
		activeStream.cancel()
		<-activeStream.done
	}

	// Hold the same semaphore used by the HTTP preview for the entire FFmpeg
	// lifetime. This enforces the camera's single-open V4L2 constraint.
	select {
	case d.streamSema <- struct{}{}:
	case <-d.routePlaybackContext().Done():
		err := errorfamily.NewInfrastructure("rtsp.camera_busy", "camera capture did not become available")
		d.setRTSPResultLocked(rtspStateError, destination, err.Error())
		d.broadcastStateChanged()

		return errResult("rtsp.camera_busy", err)
	}
	releaseStream := true
	defer func() {
		if releaseStream {
			<-d.streamSema
		}
	}()

	mode, ok := captureModeForModel(model, captureModeID)
	if !ok {
		mode = d.currentCaptureMode(model)
	}

	ctx, cancel := context.WithCancel(d.routePlaybackContext())
	cmd := exec.CommandContext(ctx, ffmpegBin, rtspFFmpegArgs(videoDev, mode, target)...)
	stdout, pipeErr := cmd.StdoutPipe()
	if pipeErr != nil {
		cancel()
		wrapped := errorfamily.WrapInfrastructuref(pipeErr, "rtsp.stdout_pipe", "create ffmpeg preview pipe")
		d.setRTSPResultLocked(rtspStateError, destination, wrapped.Error())
		d.broadcastStateChanged()

		return errResult("rtsp.stdout_pipe", wrapped)
	}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}

		return cmd.Process.Signal(syscall.SIGTERM)
	}
	cmd.WaitDelay = rtspShutdownTimeout

	stderr, stderrPipeErr := cmd.StderrPipe()
	if stderrPipeErr != nil {
		cancel()
		wrapped := errorfamily.WrapInfrastructuref(stderrPipeErr, "rtsp.stderr_pipe", "create ffmpeg stderr pipe")
		d.setRTSPResultLocked(rtspStateError, destination, wrapped.Error())
		d.broadcastStateChanged()

		return errResult("rtsp.stderr_pipe", wrapped)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		wrapped := errorfamily.WrapInfrastructuref(err, "rtsp.start_failed", "start ffmpeg RTSP publisher")
		d.setRTSPResultLocked(rtspStateError, destination, wrapped.Error())
		d.broadcastStateChanged()

		return errResult("rtsp.start_failed", wrapped)
	}
	releaseStream = false

	publisher := &rtspPublisherProcess{
		cancel: cancel,
		done:   make(chan struct{}),
		frames: newRTSPFrameFeed(),
	}
	d.mu.Lock()
	d.rtspPublisher = publisher
	d.rtspState = rtspStateStarting
	d.rtspDestination = destination
	d.rtspError = ""
	d.mu.Unlock()
	d.broadcastStateChanged()

	go pumpRTSPPreview(publisher, stdout)
	go d.watchRTSPPublisher(publisher, cmd, stderr, target, destination)

	return okResult("RTSP publisher starting")
}

func pumpRTSPPreview(publisher *rtspPublisherProcess, stdout io.ReadCloser) {
	defer func() {
		if closeErr := stdout.Close(); closeErr != nil {
			slog.Debug("RTSP preview pipe close failed", "error", closeErr)
		}
	}()
	reader := bufio.NewReaderSize(stdout, streamBufSize)
	var buffer bytes.Buffer
	for {
		frame, err := extractJPEGFrame(reader, &buffer)
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
				slog.Debug("RTSP preview frame reader stopped", "error", err)
			}
			return
		}
		publisher.frames.publish(frame)
	}
}

func (d *Daemon) setRTSPResultLocked(state rtspPublishState, destination, message string) {
	d.mu.Lock()
	d.rtspState = state
	d.rtspDestination = destination
	d.rtspError = message
	d.mu.Unlock()
}

func (d *Daemon) watchRTSPPublisher(
	publisher *rtspPublisherProcess,
	cmd *exec.Cmd,
	stderr io.ReadCloser,
	target string,
	destination string,
) {
	var lines []string
	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(make([]byte, 4096), 64*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "frame=") && strings.TrimPrefix(line, "frame=") != "0" {
			d.markRTSPPublishing(publisher)
		}
		if !isFFmpegProgressLine(line) {
			lines = append(lines, line)
			if len(lines) > 8 {
				lines = lines[1:]
			}
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		slog.Debug("RTSP ffmpeg stderr scan failed", "error", redactRTSPError(scanErr.Error(), target, destination))
	}

	waitErr := cmd.Wait()
	<-d.streamSema

	d.rtspMu.Lock()
	d.mu.Lock()
	if d.rtspPublisher == publisher {
		d.rtspPublisher = nil
		if publisher.stopRequested {
			d.rtspState = rtspStatePaused
			d.rtspError = ""
		} else {
			message := "RTSP publisher exited unexpectedly"
			if len(lines) > 0 {
				message = strings.Join(lines, "\n")
			} else if waitErr != nil {
				message = waitErr.Error()
			}
			d.rtspState = rtspStateError
			d.rtspError = redactRTSPError(message, target, destination)
			slog.Warn("RTSP publisher exited", "destination", destination, "error", d.rtspError)
		}
	}
	d.mu.Unlock()
	d.rtspMu.Unlock()
	close(publisher.done)
	d.broadcastStateChanged()
}

func isFFmpegProgressLine(line string) bool {
	key, _, isProgressField := strings.Cut(line, "=")
	return isProgressField && !strings.ContainsAny(key, " []\t")
}

func (d *Daemon) markRTSPPublishing(publisher *rtspPublisherProcess) {
	d.mu.Lock()
	if d.rtspPublisher == publisher && d.rtspState == rtspStateStarting {
		d.rtspState = rtspStatePublishing
	}
	d.mu.Unlock()
	d.broadcastStateChanged()
}

func (d *Daemon) stopRTSPPublish() CommandResult {
	d.rtspMu.Lock()
	d.mu.Lock()
	publisher := d.rtspPublisher
	if publisher == nil {
		d.mu.Unlock()
		d.rtspMu.Unlock()

		return errResultMsg("RTSP emission is not active")
	}
	publisher.stopRequested = true
	d.rtspState = rtspStateStopping
	d.mu.Unlock()
	d.rtspMu.Unlock()

	publisher.cancel()
	select {
	case <-publisher.done:
		return okResult("RTSP emission paused")
	case <-time.After(rtspShutdownTimeout + time.Second):
		return errResultMsg("RTSP publisher did not stop before the timeout")
	}
}

func (d *Daemon) stopRTSPOnShutdown() {
	d.rtspMu.Lock()
	d.mu.Lock()
	publisher := d.rtspPublisher
	if publisher != nil {
		publisher.stopRequested = true
		d.rtspState = rtspStateStopping
	}
	d.mu.Unlock()
	d.rtspMu.Unlock()
	if publisher != nil {
		publisher.cancel()
		<-publisher.done
	}
}

func (d *Daemon) rtspActive() bool {
	d.mu.RLock()
	active := d.rtspPublisher != nil
	d.mu.RUnlock()

	return active
}
