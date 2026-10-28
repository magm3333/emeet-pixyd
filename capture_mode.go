//go:build linux

package main

import "github.com/LarsArtmann/emeet-pixyd/internal/pixy"

const defaultCaptureModeID = "1080p30"

// captureMode describes one MJPEG mode advertised by the EMEET PIXY's V4L2
// interface. Digital zoom is documented as unavailable in 4K and 1080p60.
type captureMode struct {
	ID            string
	Label         string
	InputFormat   string
	Width         int
	Height        int
	FPS           int
	ZoomAvailable bool
}

// captureModes lists the MJPEG profiles advertised by the PIXY 4K. The 2K
// model filters out the 4K option below.
//
//nolint:gochecknoglobals // immutable registry of supported V4L2 capture modes
var captureModes = []captureMode{
	{ID: "4k30", Label: "4K · 3840×2160 · 30 fps (sin zoom)", InputFormat: "mjpeg", Width: 3840, Height: 2160, FPS: 30},
	{ID: "1440p30", Label: "2560×1440 · 30 fps", InputFormat: "mjpeg", Width: 2560, Height: 1440, FPS: 30, ZoomAvailable: true},
	{ID: "1080p60", Label: "1080p · 60 fps (sin zoom)", InputFormat: "mjpeg", Width: 1920, Height: 1080, FPS: 60},
	{ID: "1080p30", Label: "1080p · 30 fps (zoom hasta 1,5×)", InputFormat: "mjpeg", Width: 1920, Height: 1080, FPS: 30, ZoomAvailable: true},
	{ID: "1280x960p30", Label: "1280×960 · 30 fps", InputFormat: "mjpeg", Width: 1280, Height: 960, FPS: 30, ZoomAvailable: true},
	{ID: "720p60", Label: "720p · 60 fps", InputFormat: "mjpeg", Width: 1280, Height: 720, FPS: 60, ZoomAvailable: true},
	{ID: "720p30", Label: "720p · 30 fps", InputFormat: "mjpeg", Width: 1280, Height: 720, FPS: 30, ZoomAvailable: true},
	{ID: "576p60", Label: "1024×576 · 60 fps", InputFormat: "mjpeg", Width: 1024, Height: 576, FPS: 60, ZoomAvailable: true},
	{ID: "576p30", Label: "1024×576 · 30 fps", InputFormat: "mjpeg", Width: 1024, Height: 576, FPS: 30, ZoomAvailable: true},
	{ID: "960x720p30", Label: "960×720 · 30 fps", InputFormat: "mjpeg", Width: 960, Height: 720, FPS: 30, ZoomAvailable: true},
	{ID: "800x600p30", Label: "800×600 · 30 fps", InputFormat: "mjpeg", Width: 800, Height: 600, FPS: 30, ZoomAvailable: true},
	{ID: "480p30", Label: "640×480 · 30 fps", InputFormat: "mjpeg", Width: 640, Height: 480, FPS: 30, ZoomAvailable: true},
	{ID: "360p60", Label: "640×360 · 60 fps", InputFormat: "mjpeg", Width: 640, Height: 360, FPS: 60, ZoomAvailable: true},
	{ID: "360p30", Label: "640×360 · 30 fps", InputFormat: "mjpeg", Width: 640, Height: 360, FPS: 30, ZoomAvailable: true},
}

func captureModesForModel(model pixy.Model) []captureMode {
	modes := make([]captureMode, 0, len(captureModes))

	for _, mode := range captureModes {
		if model == pixy.Model2K && mode.Width > 2560 {
			continue
		}

		modes = append(modes, mode)
	}

	return modes
}

func captureModeForModel(model pixy.Model, id string) (captureMode, bool) {
	for _, mode := range captureModesForModel(model) {
		if mode.ID == id {
			return mode, true
		}
	}

	return captureMode{}, false
}

func (d *Daemon) currentCaptureMode(model pixy.Model) captureMode {
	d.mu.RLock()
	id := d.captureModeID
	d.mu.RUnlock()

	if mode, ok := captureModeForModel(model, id); ok {
		return mode
	}

	mode, _ := captureModeForModel(model, defaultCaptureModeID)

	return mode
}

func (d *Daemon) setCaptureMode(id string) {
	d.mu.Lock()
	d.captureModeID = id
	d.mu.Unlock()
}
