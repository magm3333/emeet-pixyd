//go:build linux

package main

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/LarsArtmann/emeet-pixyd/internal/pixy"
)

// routePlaybackMaxStepWait bounds a single recorded wait so an accidental
// multi-minute pause during recording doesn't hang playback forever; the
// real gap is preserved up to this cap.
const routePlaybackMaxStepWait = 5 * time.Minute

// routePlayOptions controls how a route repeats after its last step.
type routePlayOptions struct {
	// Loop replays the route after it finishes instead of stopping.
	Loop bool
	// PingPong reverses direction at the end of each lap instead of
	// restarting from step 0 (only meaningful when Loop is true).
	PingPong bool
}

// startRoutePlayback launches route playback in a background goroutine and
// returns immediately. The caller holds hidMu, has disabled tracking if it
// was active, and verified that no recording/playback is in progress.
func (d *Daemon) startRoutePlayback(
	name string,
	route pixy.Route,
	opts routePlayOptions,
	trackingRestore routeTrackingRestore,
) {
	playCtx, cancel := context.WithCancel(d.routePlaybackContext())

	d.mu.Lock()
	d.routePlaybackWG.Add(1)
	d.routePlaybackCancel = cancel
	d.routePlayingName = name
	d.routePlaybackTrackingRestore = trackingRestore
	d.routePlaybackRestoreError = nil
	d.mu.Unlock()
	d.broadcastStateChanged()

	go func() {
		defer d.routePlaybackWG.Done()

		d.runRoutePlayback(playCtx, route, opts)

		// Restore the camera's previous follow mode before clearing the active
		// route state. Manual and automatic camera-mode transitions serialize
		// on hidMu and are blocked while playback is active.
		d.hidMu.Lock()
		d.mu.RLock()
		restore := d.routePlaybackTrackingRestore
		d.mu.RUnlock()
		restoreErr := d.restoreCameraTrackingAfterRoute(context.Background(), restore)
		if restoreErr != nil {
			slog.Error("route playback: failed to restore camera mode", "mode", restore.camera, "error", restoreErr)
		}

		d.mu.Lock()
		d.routePlaybackCancel = nil
		d.routePlayingName = ""
		d.routePlaybackTrackingRestore = routeTrackingRestore{}
		d.routePlaybackRestoreError = restoreErr
		d.mu.Unlock()
		d.hidMu.Unlock()
		d.broadcastStateChanged()

		// Best-effort: restore the user's persisted speed preference on
		// the motor MCU — playback drove it at each step's recorded
		// speed, which may differ from the configured preference.
		// Independent of the request/playback context's lifetime.
		d.reassertSpeeds(context.Background(), pixy.AxisPan, pixy.AxisTilt, pixy.AxisZoom)
	}()
}

// runRoutePlayback drives the loop/ping-pong policy around repeated laps of
// playRouteLap. Forward stays true across loop restarts (a non-ping-pong
// loop always restarts from step 0, per spec); ping-pong flips it so the
// next lap walks the recorded steps in reverse.
func (d *Daemon) runRoutePlayback(ctx context.Context, route pixy.Route, opts routePlayOptions) {
	if len(route.Steps) == 0 {
		return
	}

	forward := true

	for {
		if !d.playRouteLap(ctx, route.Steps, forward) {
			return // canceled, or a step failed
		}

		if !opts.Loop {
			return
		}

		if opts.PingPong {
			forward = !forward
		}
	}
}

// playRouteLap applies every step of steps once, in the given direction,
// each after its recorded wait. The lap's first step is applied
// immediately — playback always starts (or, in ping-pong, resumes) by
// moving straight to that lap's starting position, never waiting on an
// artificial gap. Returns false if canceled or a step's hardware write
// failed (playback stops rather than skipping ahead on a real error).
func (d *Daemon) playRouteLap(ctx context.Context, steps []pixy.RouteStep, forward bool) bool {
	for n := range steps {
		idx := n
		if !forward {
			idx = len(steps) - 1 - n
		}

		step := steps[idx]

		wait := time.Duration(step.WaitMs) * time.Millisecond
		switch {
		case n == 0:
			wait = 0
		case wait > routePlaybackMaxStepWait:
			wait = routePlaybackMaxStepWait
		}

		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()

				return false
			case <-timer.C:
			}
		}

		select {
		case <-ctx.Done():
			return false
		default:
		}

		if err := d.applyRouteStep(ctx, step); err != nil {
			slog.Warn("route playback: step failed, stopping", "error", err)

			return false
		}
	}

	return true
}

// applyRouteStep moves every axis to the step's recorded position at the
// step's recorded speed. Unlike handlePTZCommand, it never feeds a route
// recording (playback must not record itself) and always treats the
// position as absolute — routes store absolute positions, never deltas.
//
// LOCK CONTRACT: takes v4l2Mu for the whole step (position + the speed
// re-assert nested under hidMu), matching the documented v4l2Mu -> hidMu
// order used by every other PTZ move path.
func (d *Daemon) applyRouteStep(ctx context.Context, step pixy.RouteStep) error {
	d.mu.RLock()
	videoDev := d.videoDev
	d.mu.RUnlock()

	if videoDev == "" {
		return errDeviceNotFound
	}

	d.v4l2Mu.Lock()
	defer d.v4l2Mu.Unlock()

	d.hidMu.Lock()

	for _, axis := range ptzAxisOrder {
		speed, ok := step.Speeds.Get(axis)
		if !ok || speed <= 0 {
			continue
		}

		motor, motorOK := pixy.MotorTypeFromAxis(axis)
		if !motorOK {
			continue
		}

		if speedErr := d.setMotorSpeed(ctx, motor, speed); speedErr != nil {
			slog.Warn(
				"route playback: speed re-assert failed, moving at firmware default",
				"axis", axis, "error", speedErr,
			)
		}
	}

	d.hidMu.Unlock()

	target := pixy.PTZValues{Pan: step.Pan, Tilt: step.Tilt, Zoom: step.Zoom}.Clamp()

	// Mirror handlePTZCommand's privacy-trap race protection: a recorded
	// route that dips into the bottom tilt zone needs the same armed
	// window as a manual move there.
	if tilt, _ := target.Get(pixy.AxisTilt); tilt <= privacyTrapTilt {
		d.armPrivacyTrap()
	}

	for _, axis := range ptzAxisOrder {
		info := ptzAxes[axis]
		val, _ := target.Get(axis)

		if setErr := d.deps.v4l2Set(ctx, videoDev, info.V4L2Ctrl, strconv.Itoa(val*info.Multiplier)); setErr != nil {
			return setErr
		}
	}

	d.ptzCache.Set(target, ptzCacheTTL)
	d.broadcastStateChanged()

	return nil
}
