//go:build linux

package main

import (
	"context"
	"embed"
	"encoding/json/v2"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"strconv"
	"time"

	"github.com/LarsArtmann/emeet-pixyd/internal/pixy"
	"github.com/a-h/templ"
	"github.com/dustin/go-humanize"
	errorfamily "github.com/larsartmann/go-error-family"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/starfederation/datastar-go/datastar"
)

const (
	maxStreamBufferSize = 10 * 1024 * 1024
	maxBodyBytes        = 1 << 10

	staticCacheMaxAge = 7 * 24 * time.Hour

	toastTrackingEnabled = "Tracking enabled"
	toastCameraIdle      = "Camera idle"
	toastPrivacyOn       = "Privacy mode on"
	toastCameraCentered  = "Camera centered"
	toastStateSynced     = "State synced"
	toastProbedDevices   = "Probed devices"
	toastAudioChanged    = "Audio mode changed"
	toastSpeedChanged    = "Motor speed updated"
	toastGestureToggled  = "Gesture toggled"
	toastAutoToggled     = "Auto mode toggled"
)

// toastType is the CSS class suffix for toast notifications.
type actionToastInfo struct {
	msg  string
	kind toastType
}

//nolint:gochecknoglobals
var actionToasts = map[string]actionToastInfo{
	cmdTrack:         {toastTrackingEnabled, toastTypeSuccess},
	cmdIdle:          {toastCameraIdle, toastTypeSuccess},
	cmdPrivacy:       {toastPrivacyOn, toastTypeSuccess},
	cmdCenter:        {toastCameraCentered, toastTypeSuccess},
	cmdSync:          {toastStateSynced, toastTypeSuccess},
	cmdProbe:         {toastProbedDevices, toastTypeSuccess},
	cmdToggleGesture: {toastGestureToggled, toastTypeInfo},
	cmdToggleAuto:    {toastAutoToggled, toastTypeInfo},
}

//go:embed static
var staticFS embed.FS

func formatLastSynced(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return humanize.Time(t)
}

type webServer struct {
	daemon *Daemon
}

func (s *webServer) getWebStatus(ctx context.Context) webStatus {
	snap := s.daemon.snapshot()
	//nolint:exhaustruct
	status := webStatus{
		Camera:       snap.Camera,
		Audio:        snap.Audio,
		Gesture:      snap.Gesture,
		InCall:       snap.InCall,
		Auto:         snap.Auto,
		Online:       snap.Online,
		Controllable: snap.Controllable,
		Device:       snap.Device,
		Model:        string(snap.Model),
		Error:        snap.Error,
		LastSynced:   formatLastSynced(snap.LastSynced),
		Version:      buildVersion,
		PresetNames:  snap.Presets,
		TrackMode:    snap.TrackMode,
		Speeds:       snap.Speeds,
		CaptureMode:  s.daemon.currentCaptureMode(snap.Model),
		CaptureModes: captureModesForModel(snap.Model),

		Routes:            snap.Routes,
		RecordingRoute:    snap.RecordingRoute,
		RecordingSteps:    snap.RecordingSteps,
		PendingRouteSteps: snap.PendingRouteSteps,
		PlayingRoute:      snap.PlayingRoute,
		RouteBusy:         snap.RouteBusy,
	}

	if status.Online {
		status.PTZValues = pixy.PTZValues{Pan: 0, Tilt: 0, Zoom: pixy.ZoomDefault}
	}

	// Battery reads take d.mu themselves (v2Read guards), so they happen
	// AFTER the outer lock is released — never nest the RLocks.
	if reading, ok := s.daemon.powerStatus(ctx); ok {
		status.Battery = reading.String()
	}

	return status
}

func (s *webServer) getWebStatusWithPTZ(ctx context.Context) webStatus {
	status := s.getWebStatus(ctx)
	if !status.Online {
		return status
	}

	dev := status.Device

	if values, valid := s.daemon.ptzCache.Get(); valid {
		status.Pan = values.Pan
		status.Tilt = values.Tilt
		status.Zoom = values.Zoom

		return status
	}

	ptz := s.daemon.deps.parsePTZ(ctx, dev)
	s.daemon.ptzCache.Set(ptz, ptzCacheTTL)

	status.Pan = ptz.Pan
	status.Tilt = ptz.Tilt
	status.Zoom = ptz.Zoom

	return status
}

func (s *webServer) handleIndex(responseWriter http.ResponseWriter, request *http.Request) {
	status := s.getWebStatusWithPTZ(request.Context())
	templ.Handler(page(status)).ServeHTTP(responseWriter, request) //nolint:contextcheck
}

func (s *webServer) handleHealth(responseWriter http.ResponseWriter, _ *http.Request) {
	snap := s.daemon.snapshot()

	status := http.StatusOK
	if !snap.Online {
		status = http.StatusServiceUnavailable
	}

	if err := writeJSON(responseWriter, status, healthResponse{
		Status:  boolStr(snap.Online, "ok", "offline"),
		Camera:  snap.Camera,
		Version: buildVersion,
	}); err != nil {
		slog.Debug("health response write failed", "err", err)
	}
}

type healthResponse struct {
	Status  string           `json:"status"`
	Camera  pixy.CameraState `json:"camera"`
	Version string           `json:"version"`
}

// statusResponse is the JSON contract served at GET /api/status for
// status-bar and desktop-shell widgets (Quickshell / DankMaterialShell, or any
// HTTP client). The field names are a stable public interface chosen by widget
// authors — rename with care. Unlike /api/health it always answers 200 while
// the daemon runs, even with the camera unplugged, so a widget can tell
// "daemon down" (connection refused) apart from "camera offline" (`online`
// false) without parsing error bodies.
type statusResponse struct {
	Camera       pixy.CameraState `json:"camera"`
	Device       string           `json:"device"`
	Model        string           `json:"model"`
	Online       bool             `json:"online"`
	Controllable bool             `json:"controllable"`
	InCall       bool             `json:"inCall"`
	Auto         pixy.AutoMode    `json:"auto"`
	Audio        pixy.AudioMode   `json:"audio"`
	Gesture      bool             `json:"gesture"`
	Battery      string           `json:"battery,omitzero"`
	Version      string           `json:"version"`
}

// handleStatusJSON serves the widget-facing status contract. It reuses
// getWebStatus so the payload carries the same state (and the TTL-cached
// battery reading) the web panel and Waybar already show.
func (s *webServer) handleStatusJSON(responseWriter http.ResponseWriter, request *http.Request) {
	status := s.getWebStatus(request.Context())

	resp := statusResponse{
		Camera:       status.Camera,
		Device:       status.Device,
		Model:        status.Model,
		Online:       status.Online,
		Controllable: status.Controllable,
		InCall:       status.InCall,
		Auto:         status.Auto,
		Audio:        status.Audio,
		Gesture:      status.Gesture,
		Battery:      status.Battery,
		Version:      status.Version,
	}

	if err := writeJSON(responseWriter, http.StatusOK, resp); err != nil {
		slog.Debug("status response write failed", "err", err)
	}
}

type captureModeRequest struct {
	Mode string `json:"mode"`
}

type captureModeResponse struct {
	Mode          string `json:"mode"`
	Label         string `json:"label"`
	ZoomAvailable bool   `json:"zoomAvailable"`
}

func (s *webServer) handleCaptureMode(responseWriter http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(responseWriter, request.Body, maxBodyBytes)

	body, readErr := io.ReadAll(request.Body)
	if readErr != nil {
		http.Error(responseWriter, "invalid request body", http.StatusBadRequest)

		return
	}

	var payload captureModeRequest
	if err := json.Unmarshal(body, &payload); err != nil || payload.Mode == "" {
		http.Error(responseWriter, "invalid capture mode", http.StatusBadRequest)

		return
	}

	snap := s.daemon.snapshot()
	if !snap.Online {
		http.Error(responseWriter, errStreamNoDevice.Error(), errorfamily.HTTPStatus(errStreamNoDevice))

		return
	}

	mode, ok := captureModeForModel(snap.Model, payload.Mode)
	if !ok {
		http.Error(responseWriter, "unsupported capture mode", http.StatusBadRequest)

		return
	}

	s.daemon.setCaptureMode(mode.ID)
	response := captureModeResponse{
		Mode:          mode.ID,
		Label:         mode.Label,
		ZoomAvailable: mode.ZoomAvailable,
	}
	if err := writeJSON(responseWriter, http.StatusOK, response); err != nil {
		slog.Debug("capture-mode response write failed", "err", err)
	}
}

// handleStatusPanel renders the panel as plain HTML for testing and direct access.
// The DataStar UI receives panel updates via SSE patches from handleEvents and
// action handlers.
func (s *webServer) handleStatusPanel(responseWriter http.ResponseWriter, request *http.Request) {
	status := s.getWebStatusWithPTZ(request.Context())
	templ.Handler(statusPanel(status)).ServeHTTP(responseWriter, request) //nolint:contextcheck
}

// handleEvents is the persistent SSE connection. DataStar establishes this via
// data-init="@get('/api/events', {openWhenHidden: true})". It sends the current
// panel on connect, then patches on every state-change broadcast.
func (s *webServer) handleEvents(responseWriter http.ResponseWriter, request *http.Request) {
	// The server's global WriteTimeout would cut this persistent stream at 30s;
	// clear it so the connection lives until the client disconnects (mirrors the
	// MJPEG path in stream.go). An abandoned client still ends the handler via
	// sse.Context().Done().
	rc := http.NewResponseController(responseWriter)
	if dlErr := rc.SetWriteDeadline(time.Time{}); dlErr != nil {
		slog.Warn("could not clear write deadline; SSE may be cut off by server timeout", "error", dlErr)
	}

	sse := datastar.NewSSE(responseWriter, request)

	// Send initial panel and route-section state. The route section is outside
	// #status-panel so it needs its own morph target for out-of-band updates.
	status := s.getWebStatusWithPTZ(request.Context())
	if err := sse.PatchElementTempl(statusPanel(status)); err != nil { //nolint:contextcheck
		return
	}
	if err := sse.PatchElementTempl(routeSection(status)); err != nil { //nolint:contextcheck
		return
	}

	ch := s.daemon.broadcaster.Subscribe()
	defer s.daemon.broadcaster.Unsubscribe(ch)

	for {
		select {
		case <-sse.Context().Done():
			return
		case _, ok := <-ch:
			if !ok {
				return
			}

			refreshed := s.getWebStatusWithPTZ(request.Context())
			if err := sse.PatchElementTempl(statusPanel(refreshed)); err != nil { //nolint:contextcheck
				return
			}
			if err := sse.PatchElementTempl(routeSection(refreshed)); err != nil { //nolint:contextcheck
				return
			}
		}
	}
}

// patchPanel renders the status panel as a DataStar SSE element patch.
func (s *webServer) patchPanel(sse *datastar.ServerSentEventGenerator, status webStatus) {
	if err := sse.PatchElementTempl(statusPanel(status)); err != nil {
		slog.Debug("status panel patch failed", "err", err)
	}

	sendToastScript(sse, &status)
}

// sendToastScript dispatches a toast via ExecuteScript so the client-side
// auto-dismiss logic handles fading. strconv.Quote produces a safe JS string literal.
func sendToastScript(sse *datastar.ServerSentEventGenerator, status *webStatus) {
	msg := status.Toast

	tt := status.ToastType
	if status.Error != "" {
		msg = status.Error
		tt = toastTypeError
	}

	if msg == "" {
		return
	}

	script := fmt.Sprintf("window.__showToast(%s, %s)", strconv.Quote(msg), strconv.Quote(string(tt)))
	if err := sse.ExecuteScript(script); err != nil {
		slog.Debug("toast script dispatch failed", "err", err)
	}
}

func (s *webServer) action(command string) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, request *http.Request) {
		request.Body = http.MaxBytesReader(responseWriter, request.Body, maxBodyBytes)

		result := s.daemon.handleCommand(request.Context(), command)

		slog.Debug("web action", "cmd", command, "response", result.String())

		status := s.getWebStatusWithPTZ(request.Context())
		toast, toastType := actionToast(command)
		applyResultToStatus(result, &status, toast, toastType)

		sse := datastar.NewSSE(responseWriter, request)
		s.patchPanel(sse, status) //nolint:contextcheck // templ rendering handles context internally
	}
}

func actionToast(command string) (string, toastType) {
	info, ok := actionToasts[command]
	if !ok {
		return "", ""
	}

	return info.msg, info.kind
}

func applyResultToStatus(result CommandResult, status *webStatus, toast string, tt toastType) {
	if result.IsError() {
		status.Error = result.String()
	} else {
		status.Toast = toast
		status.ToastType = tt
	}
}

func (s *webServer) handleAudio(responseWriter http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(responseWriter, request.Body, maxBodyBytes)
	mode := request.PathValue("mode")

	cmd := cmdAudio
	if mode != "" {
		cmd = cmdAudio + " " + mode
	}

	result := s.daemon.handleCommand(request.Context(), cmd)
	slog.Debug("web audio", "cmd", cmd, "response", result.String())

	status := s.getWebStatusWithPTZ(request.Context())

	toast := toastAudioChanged
	if !result.IsError() {
		toast = "Audio: " + string(status.Audio)
	}

	applyResultToStatus(result, &status, toast, toastTypeSuccess)

	sse := datastar.NewSSE(responseWriter, request)
	s.patchPanel(sse, status) //nolint:contextcheck // templ rendering handles context internally
}

// axisFromRequest parses the {axis} path segment and validates it as a PTZ
// axis, writing a 400 response and reporting false when it is missing or
// invalid. Shared by the web endpoints that route on an axis.
func axisFromRequest(responseWriter http.ResponseWriter, request *http.Request) (pixy.Axis, bool) {
	axis := pixy.Axis(request.PathValue("axis"))

	if string(axis) == "" || !ptzAxisValid(axis) {
		http.Error(responseWriter, "invalid axis", http.StatusBadRequest)

		return "", false
	}

	return axis, true
}

func (s *webServer) handlePTZ(responseWriter http.ResponseWriter, request *http.Request) {
	axis, valid := axisFromRequest(responseWriter, request)
	if !valid {
		return
	}

	var signals pixy.PTZValues
	if err := datastar.ReadSignals(request, &signals); err != nil {
		http.Error(responseWriter, "invalid signals", http.StatusBadRequest)

		return
	}

	val, _ := signals.Get(axis)

	info := ptzAxes[axis]
	intVal := info.Range.Clamp(val)
	result := s.daemon.handleCommand(request.Context(), string(axis)+" "+strconv.Itoa(intVal))
	slog.Debug("web ptz", "axis", axis, "val", intVal, "response", result.String())

	sse := datastar.NewSSE(responseWriter, request)

	if result.IsError() {
		status := s.getWebStatusWithPTZ(request.Context())
		applyResultToStatus(result, &status, "", "")
		s.patchPanel(sse, status) //nolint:contextcheck // templ rendering handles context internally

		return
	}

	status := s.getWebStatusWithPTZ(request.Context())
	if err := sse.MarshalAndPatchSignals(status.PTZValues); err != nil {
		slog.Debug("PTZ signal patch failed", "err", err)
	}
}

// presetAction builds a handler for a preset addressed by its complete path
// segment, preserving spaces in names while using the command's normal lock.
func (s *webServer) presetAction(verb string) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, request *http.Request) {
		name := request.PathValue("name")
		if name == "" {
			http.Error(responseWriter, "missing preset name", http.StatusBadRequest)

			return
		}

		s.patchNamedPresetResult(responseWriter, request, verb, name)
	}
}

// patchPresetResult is the shared tail of every preset web handler: run the
// command, refresh the panel status, and morph the panel with the outcome.
func (s *webServer) patchPresetResult(responseWriter http.ResponseWriter, request *http.Request, command string) {
	result := s.daemon.handleCommand(request.Context(), command)
	s.patchPresetCommandResult(responseWriter, request, result)
}

func (s *webServer) patchNamedPresetResult(
	responseWriter http.ResponseWriter,
	request *http.Request,
	verb, name string,
) {
	result := s.daemon.handlePresetWithLock(request.Context(), []string{cmdPreset, verb, name})
	s.patchPresetCommandResult(responseWriter, request, result)
}

func (s *webServer) patchPresetCommandResult(
	responseWriter http.ResponseWriter,
	request *http.Request,
	result CommandResult,
) {
	status := s.getWebStatusWithPTZ(request.Context())
	applyResultToStatus(result, &status, result.String(), toastTypeSuccess)

	sse := datastar.NewSSE(responseWriter, request)
	s.patchPanel(sse, status) //nolint:contextcheck // templ rendering handles context internally
}

func (s *webServer) handlePresetSave(responseWriter http.ResponseWriter, request *http.Request) {
	name := request.PathValue("name")

	err := pixy.ValidatePresetName(name)
	if err != nil {
		http.Error(responseWriter, err.Error(), errorfamily.HTTPStatus(err))

		return
	}

	s.patchNamedPresetResult(responseWriter, request, presetSave, name)
}

// handlePresetPull implements POST /api/preset/pull — the web surface for
// sweeping hardware motor slots into software presets (TODO #141). The sweep
// is read-only on the hardware side (nothing moves), so unlike push it needs
// no browser confirm() gate.
func (s *webServer) handlePresetPull(responseWriter http.ResponseWriter, request *http.Request) {
	s.patchPresetResult(responseWriter, request, cmdPreset+" "+presetPull)
}

// --- "Modo recorrido" (route record/save/play) web surface ---

func (s *webServer) handleRouteRecordStart(responseWriter http.ResponseWriter, request *http.Request) {
	result := s.daemon.handleCommand(request.Context(), cmdRoute+" "+routeRecord+" "+routeRecordStart)
	s.patchRouteCommandResult(responseWriter, request, result)
}

func (s *webServer) handleRouteRecordStop(responseWriter http.ResponseWriter, request *http.Request) {
	result := s.daemon.handleCommand(request.Context(), cmdRoute+" "+routeRecord+" "+routeRecordStop)
	s.patchRouteCommandResult(responseWriter, request, result)
}

func (s *webServer) handleRouteRecordDiscard(responseWriter http.ResponseWriter, request *http.Request) {
	result := s.daemon.handleCommand(request.Context(), cmdRoute+" "+routeRecord+" "+routeRecordDiscard)
	s.patchRouteCommandResult(responseWriter, request, result)
}

func (s *webServer) handleRouteSaveWeb(responseWriter http.ResponseWriter, request *http.Request) {
	name := request.PathValue("name")
	if name == "" {
		http.Error(responseWriter, "missing route name", http.StatusBadRequest)

		return
	}

	s.patchRouteCommandResult(responseWriter, request, s.daemon.handleRouteSave(name))
}

func (s *webServer) handleRouteDeleteWeb(responseWriter http.ResponseWriter, request *http.Request) {
	name := request.PathValue("name")
	if name == "" {
		http.Error(responseWriter, "missing route name", http.StatusBadRequest)

		return
	}

	s.patchRouteCommandResult(responseWriter, request, s.daemon.handleRouteDelete(name))
}

// patchRouteCommandResult refreshes both the route controls (outside the
// status-panel morph target) and the status panel/toast after each route action.
func (s *webServer) patchRouteCommandResult(
	responseWriter http.ResponseWriter,
	request *http.Request,
	result CommandResult,
) {
	status := s.getWebStatusWithPTZ(request.Context())
	applyResultToStatus(result, &status, result.String(), toastTypeSuccess)

	sse := datastar.NewSSE(responseWriter, request)
	if err := sse.PatchElementTempl(routeSection(status)); err != nil {
		slog.Debug("route section patch failed", "err", err)
	}
	s.patchPanel(sse, status)
}

// routePlayRequest is the JSON body for POST /api/route/play/{name}: the
// loop/ping-pong choice the client already confirmed with the user before
// sending the request.
type routePlayRequest struct {
	Loop     bool `json:"loop"`
	PingPong bool `json:"pingpong"`
}

func (s *webServer) handleRoutePlayWeb(responseWriter http.ResponseWriter, request *http.Request) {
	name := request.PathValue("name")
	if name == "" {
		http.Error(responseWriter, "missing route name", http.StatusBadRequest)

		return
	}

	request.Body = http.MaxBytesReader(responseWriter, request.Body, maxBodyBytes)

	body, readErr := io.ReadAll(request.Body)
	if readErr != nil {
		http.Error(responseWriter, "invalid request body", http.StatusBadRequest)

		return
	}

	var payload routePlayRequest

	if len(body) > 0 {
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(responseWriter, "invalid route play options", http.StatusBadRequest)

			return
		}
	}

	query := request.URL.Query()
	for _, option := range []struct {
		name   string
		target *bool
	}{
		{name: "loop", target: &payload.Loop},
		{name: "pingpong", target: &payload.PingPong},
	} {
		if raw := query.Get(option.name); raw != "" {
			value, err := strconv.ParseBool(raw)
			if err != nil {
				http.Error(responseWriter, "invalid route play options", http.StatusBadRequest)

				return
			}
			*option.target = value
		}
	}

	opts := routePlayOptions{Loop: payload.Loop, PingPong: payload.PingPong}
	result := s.daemon.handleRoutePlay(request.Context(), name, opts)
	s.patchRouteCommandResult(responseWriter, request, result)
}

func (s *webServer) handleRouteStopWeb(responseWriter http.ResponseWriter, request *http.Request) {
	result := s.daemon.handleRouteStop()
	s.patchRouteCommandResult(responseWriter, request, result)
}

func newWebMux(server *webServer) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", cachingFS{handler: http.FileServer(http.FS(staticFS))})
	mux.HandleFunc("GET /{$}", server.handleIndex)
	mux.HandleFunc("GET /panel", server.handleStatusPanel)
	mux.HandleFunc("GET /api/events", server.handleEvents)
	mux.HandleFunc("GET /api/health", server.handleHealth)
	mux.HandleFunc("GET /api/status", server.handleStatusJSON)
	mux.HandleFunc("POST /api/capture-mode", server.handleCaptureMode)
	mux.HandleFunc("POST /api/track", server.action(cmdTrack))
	mux.HandleFunc("POST /api/"+cmdIdle, server.action(cmdIdle))
	mux.HandleFunc("POST /api/privacy", server.action(cmdPrivacy))
	mux.HandleFunc("POST /api/toggle-privacy", server.action(cmdTogglePrivacy))
	mux.HandleFunc("POST /api/audio/{mode}", server.handleAudio)
	mux.HandleFunc("POST /api/gesture", server.action(cmdToggleGesture))
	mux.HandleFunc("POST /api/auto", server.action(cmdToggleAuto))
	mux.HandleFunc("POST /api/center", server.action(cmdCenter))
	mux.HandleFunc("POST /api/sync", server.action(cmdSync))
	mux.HandleFunc("POST /api/probe", server.action(cmdProbe))
	mux.HandleFunc("POST /api/preset/save/{name}", server.handlePresetSave)
	mux.HandleFunc("POST /api/preset/load/{name}", server.presetAction(presetLoad))
	mux.HandleFunc("POST /api/preset/delete/{name}", server.presetAction(presetDelete))
	// push moves the physical camera — the UI gates it behind a browser confirm().
	mux.HandleFunc("POST /api/preset/push/{name}", server.presetAction(presetPush))
	mux.HandleFunc("POST /api/preset/pull", server.handlePresetPull)
	mux.HandleFunc("POST /api/route/record/start", server.handleRouteRecordStart)
	mux.HandleFunc("POST /api/route/record/stop", server.handleRouteRecordStop)
	mux.HandleFunc("POST /api/route/record/discard", server.handleRouteRecordDiscard)
	mux.HandleFunc("POST /api/route/save/{name}", server.handleRouteSaveWeb)
	mux.HandleFunc("POST /api/route/delete/{name}", server.handleRouteDeleteWeb)
	mux.HandleFunc("POST /api/route/play/{name}", server.handleRoutePlayWeb)
	mux.HandleFunc("POST /api/route/stop", server.handleRouteStopWeb)
	mux.HandleFunc("POST /api/ptz/{axis}", server.handlePTZ)
	mux.HandleFunc("POST /api/speed/{axis}", server.handleSpeed)
	mux.HandleFunc("POST /api/tracking/{variant}", server.handleTrackingVariant)
	mux.HandleFunc("POST /api/ptz/", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "missing axis", http.StatusBadRequest)
	})
	mux.HandleFunc("GET /api/snapshot", server.handleSnapshot)
	mux.HandleFunc("GET /api/stream", server.handleStream)
	mux.Handle("GET /metrics", promhttp.Handler())

	if server.daemon.config.Debug {
		mux.HandleFunc("GET /debug/pprof/", pprof.Index)
		mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
	}

	return mux
}

// speedSignals mirrors the client-side speed slider signals
// ($speedPan/$speedTilt/$speedZoom) sent by DataStar in the POST body.
type speedSignals struct {
	SpeedPan  float64 `json:"speedPan"`
	SpeedTilt float64 `json:"speedTilt"`
	SpeedZoom float64 `json:"speedZoom"`
}

func (s speedSignals) get(axis pixy.Axis) float64 {
	switch axis {
	case pixy.AxisPan:
		return s.SpeedPan
	case pixy.AxisTilt:
		return s.SpeedTilt
	case pixy.AxisZoom:
		return s.SpeedZoom
	default:
		return 0
	}
}

// handleSpeed implements POST /api/speed/{axis} — the web surface for the
// HID motor-speed command. Values arrive as DataStar signals and are
// dispatched through the same `speed` command path as the CLI, so validation
// and device behavior have a single definition.
func (s *webServer) handleSpeed(responseWriter http.ResponseWriter, request *http.Request) {
	axis, valid := axisFromRequest(responseWriter, request)
	if !valid {
		return
	}

	var signals speedSignals
	if err := datastar.ReadSignals(request, &signals); err != nil {
		http.Error(responseWriter, "invalid signals", http.StatusBadRequest)

		return
	}

	speed := signals.get(axis)
	speedCmd := cmdSpeed + " " + string(axis) + " " + strconv.FormatFloat(speed, 'f', -1, 64)
	result := s.daemon.handleCommand(request.Context(), speedCmd)

	slog.Debug("web speed", "axis", axis, "speed", speed, "response", result.String())

	status := s.getWebStatusWithPTZ(request.Context())

	applyResultToStatus(result, &status, toastSpeedChanged, toastTypeSuccess)

	sse := datastar.NewSSE(responseWriter, request)
	s.patchPanel(sse, status) //nolint:contextcheck // templ rendering handles context internally
}

// handleTrackingVariant implements POST /api/tracking/{variant} — the web
// picker for the tracking variant, routed through the same command path as
// the CLI.
func (s *webServer) handleTrackingVariant(responseWriter http.ResponseWriter, request *http.Request) {
	variant := request.PathValue("variant")

	result := s.daemon.handleCommand(request.Context(), cmdTracking+" "+variant)

	slog.Debug("web tracking variant", "variant", variant, "response", result.String())

	status := s.getWebStatus(request.Context())

	toast, tt := actionToast(cmdTracking)
	applyResultToStatus(result, &status, toast, tt)

	sse := datastar.NewSSE(responseWriter, request)
	s.patchPanel(sse, status) //nolint:contextcheck // templ rendering handles context internally
}
