//go:build linux

package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/LarsArtmann/emeet-pixyd/internal/pixy"
)

// "Modo recorrido": record a sequence of manual PTZ moves (pan/tilt/zoom,
// at whatever speed was configured, with the real gaps between gestures)
// and replay it later — optionally looping, optionally reversing direction
// at the end of each lap (ping-pong) instead of restarting from step 0.
const (
	respRouteUsage            = "usage: route <record start|record stop|record discard|save|delete|play|stop|list> [name]"
	respRouteNotFound         = "route not found"
	respRouteAlreadyPlaying   = "a route is already playing"
	respRouteAlreadyRecording = "already recording a route"
	respRouteNothingPending   = "no recorded route awaiting a name"
	respRouteEmptyRecording   = "route recording captured no movement"
	respRouteNotPlaying       = "no route is currently playing"
	respRouteCameraBusy       = "camera mode is locked while a route is active"
	respRouteDaemonStopping   = "daemon is shutting down"

	cmdRoute = "route"

	routeRecord = "record"
	routeSave   = "save"
	routeDelete = "delete"
	routePlay   = "play"
	routeStop   = "stop"
	routeList   = "list"

	routeRecordStart   = "start"
	routeRecordStop    = "stop"
	routeRecordDiscard = "discard"

	routePlayLoopFlag     = "loop"
	routePlayPingPongFlag = "pingpong"

	minRouteParts = 3
)

func isValidRouteSubcmd(s string) bool {
	switch s {
	case routeRecord, routeSave, routeDelete, routePlay, routeStop, routeList:
		return true
	default:
		return false
	}
}

// handleRouteCommand dispatches `route <subcommand> ...`. Unlike presets,
// no subcommand needs v4l2Mu/hidMu held at dispatch time: record/save/
// delete/list only touch d.mu-guarded state, and play's actual hardware
// writes happen in a background goroutine that takes v4l2Mu/hidMu per step
// (route_playback.go) so the dispatching command/HTTP request returns
// immediately.
func (d *Daemon) handleRouteCommand(ctx context.Context, parts []string) CommandResult {
	if len(parts) < minCmdParts || !isValidRouteSubcmd(parts[1]) {
		return okResult(respRouteUsage)
	}

	switch parts[1] {
	case routeList:
		return d.handleRouteList()

	case routeRecord:
		if len(parts) < minCmdParts+1 {
			return okResult(respRouteUsage)
		}

		return d.handleRouteRecord(ctx, parts[2])

	case routeSave:
		if len(parts) < minRouteParts {
			return errResultMsg("route save: missing name")
		}

		return d.handleRouteSave(parts[2])

	case routeDelete:
		if len(parts) < minRouteParts {
			return errResultMsg("route delete: missing name")
		}

		return d.handleRouteDelete(parts[2])

	case routeStop:
		return d.handleRouteStop()

	case routePlay:
		if len(parts) < minRouteParts {
			return errResultMsg("route play: missing name")
		}

		opts := routePlayOptions{
			Loop:     slicesContainsFold(parts[3:], routePlayLoopFlag),
			PingPong: slicesContainsFold(parts[3:], routePlayPingPongFlag),
		}

		return d.handleRoutePlay(ctx, parts[2], opts)
	}

	return okResult(respRouteUsage)
}

// slicesContainsFold reports whether needle appears in haystack, compared
// case-insensitively (CLI flag tokens).
func slicesContainsFold(haystack []string, needle string) bool {
	for _, s := range haystack {
		if strings.EqualFold(s, needle) {
			return true
		}
	}

	return false
}

func (d *Daemon) handleRouteList() CommandResult {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(d.state.Routes) == 0 {
		return okResult("no routes saved")
	}

	var b strings.Builder
	b.WriteString("routes:")

	for _, name := range d.state.Routes.SortedNames() {
		fmt.Fprintf(&b, "\n  %s: %d steps", name, len(d.state.Routes[name].Steps))
	}

	return okResult(b.String())
}

// handleRouteRecord implements `route record <start|stop|discard>`.
func (d *Daemon) handleRouteRecord(ctx context.Context, action string) CommandResult {
	switch action {
	case routeRecordStart:
		return d.startRouteRecording(ctx)
	case routeRecordStop:
		return d.stopRouteRecording(ctx)
	case routeRecordDiscard:
		return d.discardRouteRecording(ctx)
	default:
		return okResult(respRouteUsage)
	}
}

// routeTrackingRestore records the camera mode before route control disables
// AI tracking. Only a previously tracking camera needs a hardware restore.
type routeTrackingRestore struct {
	camera    pixy.CameraState
	suspended bool
}

func (d *Daemon) routeBusyLocked() bool {
	return d.routeRecording != nil || d.routePlaybackCancel != nil || d.routePlaybackStopping
}

func (d *Daemon) routeBusy() bool {
	d.mu.RLock()
	busy := d.routeBusyLocked()
	d.mu.RUnlock()

	return busy
}

// suspendCameraTrackingForRoute is called with hidMu held. It snapshots the
// exact camera mode and switches AI tracking off only if it was active.
func (d *Daemon) suspendCameraTrackingForRoute(ctx context.Context) (routeTrackingRestore, error) {
	d.mu.RLock()
	previous := d.state.Camera
	d.mu.RUnlock()

	restore := routeTrackingRestore{camera: previous}
	if previous != pixy.StateTracking {
		return restore, nil
	}

	if err := d.deps.setTracking(ctx, pixy.StateIdle); err != nil {
		return restore, err
	}

	restore.suspended = true

	return restore, nil
}

// restoreCameraTrackingAfterRoute is called with hidMu held, after the route
// stops capturing or moving. It restores the exact prior camera mode.
func (d *Daemon) restoreCameraTrackingAfterRoute(ctx context.Context, restore routeTrackingRestore) error {
	if !restore.suspended {
		return nil
	}

	return d.deps.setTracking(context.WithoutCancel(ctx), restore.camera)
}

func (d *Daemon) startRouteRecording(ctx context.Context) CommandResult {
	// Serialize the snapshot/write/state transition with manual and automatic
	// camera mode changes.
	d.hidMu.Lock()
	defer d.hidMu.Unlock()

	d.mu.RLock()
	busy := d.routeBusyLocked()
	recording := d.routeRecording != nil
	playing := d.routePlaybackCancel != nil || d.routePlaybackStopping
	shuttingDown := d.routeShuttingDown
	d.mu.RUnlock()
	if shuttingDown {
		return errResultMsg(respRouteDaemonStopping)
	}

	if recording {
		return errResultMsg(respRouteAlreadyRecording)
	}
	if playing || busy {
		return errResultMsg(respRouteAlreadyPlaying)
	}

	restore, err := d.suspendCameraTrackingForRoute(ctx)
	if err != nil {
		return errResult("route record start", err)
	}

	now := time.Now()
	d.mu.Lock()
	d.routeRecording = &routeRecordingState{
		startedAt:       now,
		lastAt:          now,
		trackingRestore: restore,
	}
	d.routePending = nil
	d.mu.Unlock()
	d.broadcastStateChanged()

	return okResult("route recording started")
}

func (d *Daemon) stopRouteRecording(ctx context.Context) CommandResult {
	d.hidMu.Lock()
	defer d.hidMu.Unlock()

	d.mu.Lock()

	rec := d.routeRecording
	if rec == nil {
		d.mu.Unlock()

		return errResultMsg("not recording a route")
	}

	d.routeRecording = nil
	if len(rec.steps) > 0 {
		d.routePending = &pixy.Route{Steps: rec.steps}
	}

	count := len(rec.steps)
	d.mu.Unlock()

	restoreErr := d.restoreCameraTrackingAfterRoute(ctx, rec.trackingRestore)
	d.broadcastStateChanged()
	if restoreErr != nil {
		return errResult("route record restore", restoreErr)
	}
	if count == 0 {
		return errResultMsg(respRouteEmptyRecording)
	}

	return okResult(fmt.Sprintf("route recording stopped: %d steps captured, save with a name", count))
}

func (d *Daemon) discardRouteRecording(ctx context.Context) CommandResult {
	d.hidMu.Lock()
	defer d.hidMu.Unlock()

	d.mu.Lock()

	if d.routeRecording == nil && d.routePending == nil {
		d.mu.Unlock()

		return errResultMsg("nothing to discard")
	}

	rec := d.routeRecording
	d.routeRecording = nil
	d.routePending = nil
	d.mu.Unlock()

	if rec != nil {
		if err := d.restoreCameraTrackingAfterRoute(ctx, rec.trackingRestore); err != nil {
			d.broadcastStateChanged()

			return errResult("route discard restore", err)
		}
	}
	d.broadcastStateChanged()

	return okResult("route recording discarded")
}

// stopRouteRecordingOnShutdown abandons the in-memory capture and restores
// the user's previous camera mode before daemon shutdown.
func (d *Daemon) stopRouteRecordingOnShutdown() {
	d.hidMu.Lock()
	d.mu.Lock()
	rec := d.routeRecording
	d.routeRecording = nil
	d.mu.Unlock()

	if rec != nil {
		if err := d.restoreCameraTrackingAfterRoute(context.Background(), rec.trackingRestore); err != nil {
			slog.Error("route recording: failed to restore camera mode on shutdown", "mode", rec.trackingRestore.camera, "error", err)
		}
	}
	d.hidMu.Unlock()
	d.broadcastStateChanged()
}

// handleRouteSave persists the pending recorded route (captured by `route
// record stop`) under name, mirroring the preset save two-phase flow.
func (d *Daemon) handleRouteSave(name string) CommandResult {
	if err := pixy.ValidateRouteName(name); err != nil {
		return errResultMsg(err.Error())
	}

	d.mu.Lock()

	if d.routePending == nil {
		d.mu.Unlock()

		return errResultMsg(respRouteNothingPending)
	}

	if d.state.Routes == nil {
		d.state.Routes = pixy.NewRouteMap()
	}

	if _, exists := d.state.Routes[name]; !exists && d.state.Routes.IsFull() {
		d.mu.Unlock()

		return errResultMsg(fmt.Sprintf("route limit reached (%d)", pixy.MaxRoutes))
	}

	route := *d.routePending
	d.state.Routes[name] = route
	d.routePending = nil
	d.saveStateOrLog("failed to save state")
	stepCount := len(route.Steps)
	d.mu.Unlock()

	d.broadcastStateChanged()

	return okResult(fmt.Sprintf("route %q saved: %d steps", name, stepCount))
}

func (d *Daemon) handleRouteDelete(name string) CommandResult {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, ok := d.state.Routes[name]; !ok {
		return errResultMsg(respRouteNotFound)
	}

	delete(d.state.Routes, name)
	d.saveStateOrLog("failed to save state")
	d.broadcastStateChanged()

	return okResult(fmt.Sprintf("route %q deleted", name))
}

// handleRoutePlay starts playback of a saved route in the background and
// returns immediately — the move/wait sequence runs in a goroutine
// (route_playback.go) so the triggering command/HTTP request is not held
// open for the route's (potentially long) duration.
func (d *Daemon) handleRoutePlay(ctx context.Context, name string, opts routePlayOptions) CommandResult {
	d.hidMu.Lock()
	defer d.hidMu.Unlock()

	d.mu.Lock()
	if d.routeShuttingDown {
		d.mu.Unlock()

		return errResultMsg(respRouteDaemonStopping)
	}

	route, ok := d.state.Routes[name]
	if !ok {
		d.mu.Unlock()

		return errResultMsg(respRouteNotFound)
	}

	if len(route.Steps) == 0 {
		d.mu.Unlock()

		return errResultMsg(respRouteEmptyRecording)
	}

	if d.routeRecording != nil {
		d.mu.Unlock()

		return errResultMsg(respRouteAlreadyRecording)
	}

	if d.routePlaybackCancel != nil || d.routePlaybackStopping {
		d.mu.Unlock()

		return errResultMsg(respRouteAlreadyPlaying)
	}

	videoDev := d.videoDev
	d.mu.Unlock()

	if videoDev == "" {
		return errResultMsg(respDeviceNotFound)
	}

	restore, err := d.suspendCameraTrackingForRoute(ctx)
	if err != nil {
		return errResult("route play start", err)
	}

	d.startRoutePlayback(name, route, opts, restore)

	suffix := ""
	if opts.Loop {
		suffix = " (loop"

		if opts.PingPong {
			suffix += ", ping-pong"
		}

		suffix += ")"
	}

	return okResult(fmt.Sprintf("route %q playing%s", name, suffix))
}

// recordRouteStep appends a step to the in-progress route recording, if
// any. ptz is the full current position (post-move, or unchanged for a
// speed-only change); the persisted speed preference in effect right now
// and the wait since the previous step are captured alongside it. No-op
// when not recording or once the recording has hit pixy.MaxRouteSteps
// (further manual moves keep working — they just stop growing the route).
func (d *Daemon) recordRouteStep(ptz pixy.PTZValues) {
	d.mu.Lock()
	defer d.mu.Unlock()

	rec := d.routeRecording
	if rec == nil || len(rec.steps) >= pixy.MaxRouteSteps {
		return
	}

	now := time.Now()
	waitMs := now.Sub(rec.lastAt).Milliseconds()
	rec.lastAt = now

	rec.steps = append(rec.steps, pixy.RouteStep{
		Pan:    ptz.Pan,
		Tilt:   ptz.Tilt,
		Zoom:   ptz.Zoom,
		Speeds: d.state.Speeds,
		WaitMs: waitMs,
	})
}

// routeRecordingStepCount and routePendingStepCount are nil-safe accessors
// for statusSnapshot (snapshot() takes d.mu itself; these just avoid a
// nil-pointer read on the two optional d.mu-guarded pointers).
func routeRecordingStepCount(rec *routeRecordingState) int {
	if rec == nil {
		return 0
	}

	return len(rec.steps)
}

func routePendingStepCount(pending *pixy.Route) int {
	if pending == nil {
		return 0
	}

	return len(pending.Steps)
}

func (d *Daemon) handleRouteStop() CommandResult {
	d.hidMu.Lock()
	d.mu.Lock()
	cancel := d.routePlaybackCancel
	if cancel == nil {
		stopping := d.routePlaybackStopping
		d.mu.Unlock()
		d.hidMu.Unlock()
		if stopping {
			d.routePlaybackWG.Wait()

			return okResult("route stopped")
		}

		return errResultMsg(respRouteNotPlaying)
	}
	d.routePlaybackStopping = true
	d.mu.Unlock()
	d.hidMu.Unlock()

	cancel()
	d.routePlaybackWG.Wait()

	d.hidMu.Lock()
	d.mu.Lock()
	d.routePlaybackStopping = false
	restoreErr := d.routePlaybackRestoreError
	d.routePlaybackRestoreError = nil
	d.mu.Unlock()
	d.hidMu.Unlock()
	d.broadcastStateChanged()
	if restoreErr != nil {
		return errResult("route stop restore", restoreErr)
	}

	return okResult("route stopped")
}
