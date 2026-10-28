//go:build linux

package main

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LarsArtmann/emeet-pixyd/internal/pixy"
)

// warmPTZCache seeds the daemon's PTZ cache the way a web page load does
// (getWebStatusWithPTZ -> parsePTZ) before any route-recording test issues
// a move: handlePTZCommand only records a route step when the cache is
// already valid (it needs the OTHER two axes' last-known values to build a
// full position), matching the existing cache-validity contract documented
// on the "accurate readback" comment it shares code with.
func warmPTZCache(d *Daemon, ptz pixy.PTZValues) {
	d.ptzCache.Set(ptz, ptzCacheTTL)
}

// withRouteTrackingFake models the camera mode write made at route start and
// its restoration at route completion without reaching a host HID device.
func withRouteTrackingFake(calls *[]pixy.CameraState) testDaemonOption {
	return func(d *Daemon) {
		d.deps.setTracking = func(_ context.Context, mode pixy.CameraState) error {
			if calls != nil {
				*calls = append(*calls, mode)
			}

			d.mu.Lock()
			d.state.Camera = mode
			d.mu.Unlock()

			return nil
		}
	}
}

func TestRouteRecord_CaptureStopSaveListDelete(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t, pixy.StateTracking, testVideoDev, testHIDDev, withNoopV4L2(), withNoopParsePTZ(), withRouteTrackingFake(nil))
	warmPTZCache(d, pixy.PTZValues{Pan: 0, Tilt: 0, Zoom: pixy.ZoomDefault})

	if result := d.handleCommand(t.Context(), "route record start"); result.IsError() {
		t.Fatalf("record start failed: %s", result.String())
	}

	if result := d.handleCommand(t.Context(), "pan 10"); result.IsError() {
		t.Fatalf("pan move failed: %s", result.String())
	}

	if result := d.handleCommand(t.Context(), "tilt 5"); result.IsError() {
		t.Fatalf("tilt move failed: %s", result.String())
	}

	stopResult := d.handleCommand(t.Context(), "route record stop")
	if stopResult.IsError() {
		t.Fatalf("record stop failed: %s", stopResult.String())
	}

	if !strings.Contains(stopResult.String(), "2 steps") {
		t.Errorf("record stop response = %q, want mention of 2 steps", stopResult.String())
	}

	saveResult := d.handleCommand(t.Context(), "route save myroute")
	if saveResult.IsError() {
		t.Fatalf("route save failed: %s", saveResult.String())
	}

	d.mu.RLock()
	route, ok := d.state.Routes["myroute"]
	d.mu.RUnlock()

	if !ok {
		t.Fatal("route \"myroute\" not present in state after save")
	}

	if len(route.Steps) != 2 {
		t.Fatalf("saved route has %d steps, want 2", len(route.Steps))
	}

	if route.Steps[0].Pan != 10 || route.Steps[0].Tilt != 0 {
		t.Errorf("step 0 = %+v, want pan=10 tilt=0", route.Steps[0])
	}

	if route.Steps[1].Pan != 10 || route.Steps[1].Tilt != 5 {
		t.Errorf("step 1 = %+v, want pan=10 tilt=5", route.Steps[1])
	}

	listResult := d.handleCommand(t.Context(), "route list")
	if !strings.Contains(listResult.String(), "myroute: 2 steps") {
		t.Errorf("route list = %q, want to mention myroute: 2 steps", listResult.String())
	}

	deleteResult := d.handleCommand(t.Context(), "route delete myroute")
	if deleteResult.IsError() {
		t.Fatalf("route delete failed: %s", deleteResult.String())
	}

	listAfterDelete := d.handleCommand(t.Context(), "route list")
	if listAfterDelete.String() != "no routes saved" {
		t.Errorf("route list after delete = %q, want %q", listAfterDelete.String(), "no routes saved")
	}
}

func TestRouteRecord_AlreadyRecordingRejected(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t, pixy.StateTracking, testVideoDev, testHIDDev, withNoopV4L2(), withNoopParsePTZ(), withRouteTrackingFake(nil))

	if result := d.handleCommand(t.Context(), "route record start"); result.IsError() {
		t.Fatalf("first start failed: %s", result.String())
	}

	second := d.handleCommand(t.Context(), "route record start")
	if !second.IsError() || second.String() != errorPrefix+respRouteAlreadyRecording {
		t.Errorf("second start = %q, want %q", second.String(), respRouteAlreadyRecording)
	}
}

func TestRouteRecord_StopWithoutMovementRejectedButClearsState(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t, pixy.StateTracking, testVideoDev, testHIDDev, withNoopV4L2(), withNoopParsePTZ(), withRouteTrackingFake(nil))

	if result := d.handleCommand(t.Context(), "route record start"); result.IsError() {
		t.Fatalf("start failed: %s", result.String())
	}

	stopResult := d.handleCommand(t.Context(), "route record stop")
	if !stopResult.IsError() || stopResult.String() != errorPrefix+respRouteEmptyRecording {
		t.Errorf("empty stop = %q, want %q", stopResult.String(), respRouteEmptyRecording)
	}
	if camera := readCameraState(d); camera != pixy.StateTracking {
		t.Errorf("camera state after empty stop = %s, want restored tracking", camera)
	}

	// The rejected stop must still have cleared d.routeRecording — a new
	// recording can start right away.
	if result := d.handleCommand(t.Context(), "route record start"); result.IsError() {
		t.Errorf("start after empty stop failed: %s", result.String())
	}
}

func TestRouteRecord_RestoresEveryPreviousCameraMode(t *testing.T) {
	t.Parallel()

	for _, previous := range []pixy.CameraState{pixy.StateTracking, pixy.StateIdle, pixy.StatePrivacy} {
		t.Run(string(previous), func(t *testing.T) {
			t.Parallel()

			var trackingCalls []pixy.CameraState
			d := newTestDaemon(
				t, previous, testVideoDev, testHIDDev,
				withRouteTrackingFake(&trackingCalls),
			)

			if result := d.handleCommand(t.Context(), "route record start"); result.IsError() {
				t.Fatalf("route start failed from %s: %s", previous, result.String())
			}

			wantDuring := previous
			if previous == pixy.StateTracking {
				wantDuring = pixy.StateIdle
			}
			if got := readCameraState(d); got != wantDuring {
				t.Fatalf("camera while recording = %s, want %s", got, wantDuring)
			}

			stop := d.handleCommand(t.Context(), "route record stop")
			if !stop.IsError() || stop.String() != errorPrefix+respRouteEmptyRecording {
				t.Fatalf("empty stop = %q, want empty-recording rejection", stop.String())
			}
			if got := readCameraState(d); got != previous {
				t.Errorf("camera after recording = %s, want exact prior mode %s", got, previous)
			}

			wantCalls := []pixy.CameraState(nil)
			if previous == pixy.StateTracking {
				wantCalls = []pixy.CameraState{pixy.StateIdle, pixy.StateTracking}
			}
			if len(trackingCalls) != len(wantCalls) {
				t.Errorf("tracking writes = %v, want %v", trackingCalls, wantCalls)
			} else {
				for i := range wantCalls {
					if trackingCalls[i] != wantCalls[i] {
						t.Errorf("tracking write %d = %s, want %s", i, trackingCalls[i], wantCalls[i])
					}
				}
			}
		})
	}
}

func TestRouteRecord_DisablesTrackingAndRestoresItOnStop(t *testing.T) {
	t.Parallel()

	var trackingCalls []pixy.CameraState
	d := newTestDaemon(
		t, pixy.StateTracking, testVideoDev, testHIDDev,
		withNoopV4L2(), withNoopParsePTZ(), withRouteTrackingFake(&trackingCalls),
	)
	warmPTZCache(d, pixy.PTZValues{Pan: 0, Tilt: 0, Zoom: pixy.ZoomDefault})

	if result := d.handleCommand(t.Context(), "route record start"); result.IsError() {
		t.Fatalf("record start failed: %s", result.String())
	}
	if camera := readCameraState(d); camera != pixy.StateIdle {
		t.Fatalf("camera state while recording = %s, want idle", camera)
	}
	if len(trackingCalls) != 1 || trackingCalls[0] != pixy.StateIdle {
		t.Fatalf("tracking writes after record start = %v, want [idle]", trackingCalls)
	}

	if result := d.handleCommand(t.Context(), "pan 15"); result.IsError() {
		t.Fatalf("manual pan during recording failed: %s", result.String())
	}
	if result := d.handleCommand(t.Context(), "route record stop"); result.IsError() {
		t.Fatalf("record stop failed: %s", result.String())
	}

	if camera := readCameraState(d); camera != pixy.StateTracking {
		t.Errorf("camera state after recording = %s, want restored tracking", camera)
	}
	if len(trackingCalls) != 2 || trackingCalls[1] != pixy.StateTracking {
		t.Errorf("tracking writes after record stop = %v, want [idle tracking]", trackingCalls)
	}
}

func TestRouteRecord_RestoresTrackingOnDiscardAndBlocksManualTrackingDuringRoute(t *testing.T) {
	t.Parallel()

	var trackingCalls []pixy.CameraState
	d := newTestDaemon(
		t, pixy.StateTracking, testVideoDev, testHIDDev,
		withNoopV4L2(), withRouteTrackingFake(&trackingCalls),
	)

	if result := d.handleCommand(t.Context(), "route record start"); result.IsError() {
		t.Fatalf("record start failed: %s", result.String())
	}
	blocked := d.handleCommand(t.Context(), cmdTrack)
	if !blocked.IsError() || !strings.Contains(blocked.String(), respRouteCameraBusy) {
		t.Fatalf("manual tracking during route = %q, want route-busy error", blocked.String())
	}
	if camera := readCameraState(d); camera != pixy.StateIdle {
		t.Errorf("camera state after blocked manual track = %s, want idle", camera)
	}

	if result := d.handleCommand(t.Context(), "route record discard"); result.IsError() {
		t.Fatalf("record discard failed: %s", result.String())
	}
	if camera := readCameraState(d); camera != pixy.StateTracking {
		t.Errorf("camera state after discard = %s, want restored tracking", camera)
	}
	if len(trackingCalls) != 2 || trackingCalls[0] != pixy.StateIdle || trackingCalls[1] != pixy.StateTracking {
		t.Errorf("tracking writes during discard flow = %v, want [idle tracking]", trackingCalls)
	}
}

func TestAutoCallLifecycleCannotReenableTrackingDuringRoute(t *testing.T) {
	t.Parallel()

	var trackingCalls []pixy.CameraState
	d := newTestDaemon(
		t, pixy.StateIdle, testVideoDev, testHIDDev,
		withNoopV4L2(), withNoopAudio(), withRouteTrackingFake(&trackingCalls),
	)
	if result := d.handleCommand(t.Context(), "route record start"); result.IsError() {
		t.Fatalf("record start failed: %s", result.String())
	}

	d.handleCallStart(t.Context(), pixy.StateIdle, pixy.AutoFull)
	if got := readCameraState(d); got != pixy.StateIdle {
		t.Errorf("call start changed camera to %s during route; want idle", got)
	}
	d.handleCallEnd(t.Context(), pixy.AutoFull)
	if got := readCameraState(d); got != pixy.StateIdle {
		t.Errorf("call end changed camera to %s during route; want idle", got)
	}
	if len(trackingCalls) != 0 {
		t.Errorf("auto call lifecycle wrote tracking modes during route: %v", trackingCalls)
	}

	if result := d.handleCommand(t.Context(), "route record discard"); result.IsError() {
		t.Fatalf("discard failed: %s", result.String())
	}
}

func TestRouteRecord_ShutdownRestoresTracking(t *testing.T) {
	t.Parallel()

	var trackingCalls []pixy.CameraState
	d := newTestDaemon(
		t, pixy.StateTracking, testVideoDev, testHIDDev,
		withRouteTrackingFake(&trackingCalls),
	)
	if result := d.handleCommand(t.Context(), "route record start"); result.IsError() {
		t.Fatalf("record start failed: %s", result.String())
	}

	d.stopRouteRecordingOnShutdown()

	if got := readCameraState(d); got != pixy.StateTracking {
		t.Errorf("camera mode after shutdown cleanup = %s, want restored tracking", got)
	}
	d.mu.RLock()
	stillRecording := d.routeRecording != nil
	d.mu.RUnlock()
	if stillRecording {
		t.Error("shutdown cleanup left a route recording active")
	}
}

func TestRouteSave_WithoutPendingRejected(t *testing.T) {
	t.Parallel()

	d := testDaemonNoDevice(t)

	result := d.handleCommand(t.Context(), "route save somename")
	if !result.IsError() || result.String() != errorPrefix+respRouteNothingPending {
		t.Errorf("save without pending = %q, want %q", result.String(), respRouteNothingPending)
	}
}

func TestRouteRecord_Discard(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t, pixy.StateTracking, testVideoDev, testHIDDev, withNoopV4L2(), withNoopParsePTZ(), withRouteTrackingFake(nil))
	warmPTZCache(d, pixy.PTZValues{Pan: 0, Tilt: 0, Zoom: pixy.ZoomDefault})

	if result := d.handleCommand(t.Context(), "route record start"); result.IsError() {
		t.Fatalf("start failed: %s", result.String())
	}

	if result := d.handleCommand(t.Context(), "pan 10"); result.IsError() {
		t.Fatalf("move failed: %s", result.String())
	}

	if result := d.handleCommand(t.Context(), "route record discard"); result.IsError() {
		t.Fatalf("discard failed: %s", result.String())
	}

	// Discard must clear the in-progress recording, not just the pending
	// slot: a subsequent "stop" (with nothing to stop) must say so.
	stopResult := d.handleCommand(t.Context(), "route record stop")
	if !stopResult.IsError() || !strings.Contains(stopResult.String(), "not recording") {
		t.Errorf("stop after discard = %q, want a not-recording error", stopResult.String())
	}

	saveResult := d.handleCommand(t.Context(), "route save shouldfail")
	if !saveResult.IsError() || saveResult.String() != errorPrefix+respRouteNothingPending {
		t.Errorf("save after discard = %q, want %q", saveResult.String(), respRouteNothingPending)
	}
}

// --- Playback ---

// seedRoute installs a two-step route directly into daemon state, bypassing
// the (timing-sensitive) recorder — playback tests only need steps with
// known positions, speeds, and wait times.
func seedRoute(t *testing.T, d *Daemon, name string, steps []pixy.RouteStep) {
	t.Helper()

	d.mu.Lock()
	if d.state.Routes == nil {
		d.state.Routes = pixy.NewRouteMap()
	}

	d.state.Routes[name] = pixy.Route{Steps: steps}
	d.mu.Unlock()
}

func TestRoutePlay_ForwardAppliesEveryStepInOrder(t *testing.T) {
	t.Parallel()

	var moves []v4l2Call

	d := newTestDaemon(t, pixy.StateTracking, testVideoDev, testHIDDev, withCaptureV4L2(&moves), withRouteTrackingFake(nil))
	seedRoute(t, d, "r", []pixy.RouteStep{
		{Pan: 10, Tilt: 0, Zoom: 100, WaitMs: 0},
		{Pan: 20, Tilt: 5, Zoom: 110, WaitMs: 20},
	})

	playResult := d.handleCommand(t.Context(), "route play r")
	if playResult.IsError() {
		t.Fatalf("route play failed: %s", playResult.String())
	}

	d.routePlaybackWG.Wait()

	want := []v4l2Call{
		{dev: testVideoDev, ctrl: "pan_absolute", val: "36000"},
		{dev: testVideoDev, ctrl: "tilt_absolute", val: "0"},
		{dev: testVideoDev, ctrl: "zoom_absolute", val: "100"},
		{dev: testVideoDev, ctrl: "pan_absolute", val: "72000"},
		{dev: testVideoDev, ctrl: "tilt_absolute", val: "18000"},
		{dev: testVideoDev, ctrl: "zoom_absolute", val: "110"},
	}

	if len(moves) != len(want) {
		t.Fatalf("captured %d v4l2 calls, want %d: %+v", len(moves), len(want), moves)
	}

	for i, m := range moves {
		if m != want[i] {
			t.Errorf("call %d = %+v, want %+v", i, m, want[i])
		}
	}

	d.mu.RLock()
	playing := d.routePlayingName
	d.mu.RUnlock()

	if playing != "" {
		t.Errorf("routePlayingName = %q after playback finished, want empty", playing)
	}
}

func TestRoutePlay_DisablesAndRestoresTrackingOnCompletion(t *testing.T) {
	t.Parallel()

	var (
		moves         []v4l2Call
		trackingCalls []pixy.CameraState
	)
	d := newTestDaemon(
		t, pixy.StateTracking, testVideoDev, testHIDDev,
		withCaptureV4L2(&moves), withRouteTrackingFake(&trackingCalls),
	)
	seedRoute(t, d, "r", []pixy.RouteStep{{Pan: 7}, {Pan: 8, WaitMs: 250}})
	if result := d.handleCommand(t.Context(), "route play r"); result.IsError() {
		t.Fatalf("route play failed: %s", result.String())
	}
	if camera := readCameraState(d); camera != pixy.StateIdle {
		t.Fatalf("camera state during playback = %s, want idle", camera)
	}
	d.routePlaybackWG.Wait()

	if camera := readCameraState(d); camera != pixy.StateTracking {
		t.Errorf("camera state after playback = %s, want restored tracking", camera)
	}
	if len(trackingCalls) != 2 || trackingCalls[0] != pixy.StateIdle || trackingCalls[1] != pixy.StateTracking {
		t.Errorf("tracking writes around playback = %v, want [idle tracking]", trackingCalls)
	}
}

func TestRoutePlay_RestoresEveryPreviousCameraMode(t *testing.T) {
	t.Parallel()

	for _, previous := range []pixy.CameraState{pixy.StateTracking, pixy.StateIdle, pixy.StatePrivacy} {
		t.Run(string(previous), func(t *testing.T) {
			t.Parallel()

			var trackingCalls []pixy.CameraState
			d := newTestDaemon(
				t, previous, testVideoDev, testHIDDev,
				withNoopV4L2(), withRouteTrackingFake(&trackingCalls),
			)
			seedRoute(t, d, "r", []pixy.RouteStep{{Pan: 8}, {Pan: 9, WaitMs: 1000}})

			if result := d.handleCommand(t.Context(), "route play r"); result.IsError() {
				t.Fatalf("route play failed from %s: %s", previous, result.String())
			}
			wantDuring := previous
			if previous == pixy.StateTracking {
				wantDuring = pixy.StateIdle
			}
			if got := readCameraState(d); got != wantDuring {
				t.Fatalf("camera during playback = %s, want %s", got, wantDuring)
			}

			d.routePlaybackWG.Wait()
			if got := readCameraState(d); got != previous {
				t.Errorf("camera after playback = %s, want exact prior mode %s", got, previous)
			}
			wantCalls := []pixy.CameraState(nil)
			if previous == pixy.StateTracking {
				wantCalls = []pixy.CameraState{pixy.StateIdle, pixy.StateTracking}
			}
			if len(trackingCalls) != len(wantCalls) {
				t.Errorf("tracking writes = %v, want %v", trackingCalls, wantCalls)
			} else {
				for i := range wantCalls {
					if trackingCalls[i] != wantCalls[i] {
						t.Errorf("tracking write %d = %s, want %s", i, trackingCalls[i], wantCalls[i])
					}
				}
			}
		})
	}
}

func TestRoutePlay_LoopWithoutPingPongRestartsFromStepZero(t *testing.T) {
	t.Parallel()

	var moves []v4l2Call

	d := newTestDaemon(t, pixy.StateTracking, testVideoDev, testHIDDev, withCaptureV4L2(&moves), withRouteTrackingFake(nil))
	seedRoute(t, d, "r", []pixy.RouteStep{
		{Pan: 1, WaitMs: 0},
		{Pan: 2, WaitMs: 2},
	})

	if result := d.handleCommand(t.Context(), "route play r loop"); result.IsError() {
		t.Fatalf("route play failed: %s", result.String())
	}

	// Let a few laps elapse, then stop and inspect the pan sequence.
	time.Sleep(60 * time.Millisecond)

	if result := d.handleCommand(t.Context(), "route stop"); result.IsError() {
		t.Fatalf("route stop failed: %s", result.String())
	}

	d.routePlaybackWG.Wait()

	panSeq := panSequence(moves)
	if len(panSeq) < 4 {
		t.Fatalf("only %d pan writes captured, want at least 2 full laps (4): %v", len(panSeq), panSeq)
	}

	// Every lap must read 1, 2 in that order (never 2, 1 — that would be
	// ping-pong behavior, which was NOT requested here).
	for i := 0; i+1 < len(panSeq); i += 2 {
		if panSeq[i] != 1 || panSeq[i+1] != 2 {
			t.Errorf("pan pair at %d = (%d,%d), want (1,2) every lap: %v", i, panSeq[i], panSeq[i+1], panSeq)

			break
		}
	}
}

func TestRoutePlay_LoopWithPingPongReversesDirection(t *testing.T) {
	t.Parallel()

	var moves []v4l2Call

	d := newTestDaemon(t, pixy.StateTracking, testVideoDev, testHIDDev, withCaptureV4L2(&moves), withRouteTrackingFake(nil))
	seedRoute(t, d, "r", []pixy.RouteStep{
		{Pan: 1, WaitMs: 0},
		{Pan: 2, WaitMs: 2},
	})

	if result := d.handleCommand(t.Context(), "route play r loop pingpong"); result.IsError() {
		t.Fatalf("route play failed: %s", result.String())
	}

	time.Sleep(60 * time.Millisecond)

	if result := d.handleCommand(t.Context(), "route stop"); result.IsError() {
		t.Fatalf("route stop failed: %s", result.String())
	}

	d.routePlaybackWG.Wait()

	panSeq := panSequence(moves)
	if len(panSeq) < 4 {
		t.Fatalf("only %d pan writes captured, want at least 2 full laps (4): %v", len(panSeq), panSeq)
	}

	// Ping-pong must show BOTH a forward pair (1,2) and a reversed pair
	// (2,1) somewhere in the sequence.
	var sawForward, sawReverse bool

	for i := 0; i+1 < len(panSeq); i++ {
		switch {
		case panSeq[i] == 1 && panSeq[i+1] == 2:
			sawForward = true
		case panSeq[i] == 2 && panSeq[i+1] == 1:
			sawReverse = true
		}
	}

	if !sawForward || !sawReverse {
		t.Errorf("pan sequence %v does not show both forward and reversed laps (ping-pong)", panSeq)
	}
}

func TestRoutePlay_StopCancelsBeforeLaterSteps(t *testing.T) {
	t.Parallel()

	var moves []v4l2Call

	d := newTestDaemon(t, pixy.StateTracking, testVideoDev, testHIDDev, withCaptureV4L2(&moves), withRouteTrackingFake(nil))
	seedRoute(t, d, "r", []pixy.RouteStep{
		{Pan: 1, WaitMs: 0},
		{Pan: 2, WaitMs: 60_000}, // long enough that the test's Stop always wins the race
	})

	if result := d.handleCommand(t.Context(), "route play r"); result.IsError() {
		t.Fatalf("route play failed: %s", result.String())
	}

	time.Sleep(20 * time.Millisecond)

	if result := d.handleCommand(t.Context(), "route stop"); result.IsError() {
		t.Fatalf("route stop failed: %s", result.String())
	}

	d.routePlaybackWG.Wait()

	panSeq := panSequence(moves)
	if len(panSeq) != 1 || panSeq[0] != 1 {
		t.Errorf("pan sequence = %v, want exactly [1] (step 1 must never be reached)", panSeq)
	}
	if got := readCameraState(d); got != pixy.StateTracking {
		t.Errorf("camera mode after canceled playback = %s, want restored tracking", got)
	}

	d.mu.RLock()
	cancel := d.routePlaybackCancel
	d.mu.RUnlock()

	if cancel != nil {
		t.Error("routePlaybackCancel still set after playback stopped")
	}
}

func TestRoutePlay_ErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("route not found", func(t *testing.T) {
		t.Parallel()

		d := newTestDaemon(t, pixy.StateTracking, testVideoDev, testHIDDev, withRouteTrackingFake(nil))

		result := d.handleCommand(t.Context(), "route play nope")
		if !result.IsError() || result.String() != errorPrefix+respRouteNotFound {
			t.Errorf("play missing route = %q, want %q", result.String(), respRouteNotFound)
		}
	})

	t.Run("no device", func(t *testing.T) {
		t.Parallel()

		d := testDaemonNoDevice(t)
		seedRoute(t, d, "r", []pixy.RouteStep{{Pan: 1}})

		result := d.handleCommand(t.Context(), "route play r")
		if !result.IsError() || result.String() != errorPrefix+respDeviceNotFound {
			t.Errorf("play without device = %q, want %q", result.String(), respDeviceNotFound)
		}
	})

	t.Run("already recording", func(t *testing.T) {
		t.Parallel()

		d := newTestDaemon(t, pixy.StateTracking, testVideoDev, testHIDDev, withRouteTrackingFake(nil))
		seedRoute(t, d, "r", []pixy.RouteStep{{Pan: 1}})

		if result := d.handleCommand(t.Context(), "route record start"); result.IsError() {
			t.Fatalf("record start failed: %s", result.String())
		}

		result := d.handleCommand(t.Context(), "route play r")
		if !result.IsError() || result.String() != errorPrefix+respRouteAlreadyRecording {
			t.Errorf("play while recording = %q, want %q", result.String(), respRouteAlreadyRecording)
		}
	})

	t.Run("already playing", func(t *testing.T) {
		t.Parallel()

		var moves []v4l2Call

		d := newTestDaemon(t, pixy.StateTracking, testVideoDev, testHIDDev, withCaptureV4L2(&moves), withRouteTrackingFake(nil))
		// WaitMs on step 0 is always ignored (the first step of a lap is
		// always applied immediately); the delay that keeps this playback
		// "already playing" long enough for the assertion below belongs
		// on step 1.
		seedRoute(t, d, "r", []pixy.RouteStep{{Pan: 1}, {Pan: 2, WaitMs: 60_000}})

		if result := d.handleCommand(t.Context(), "route play r"); result.IsError() {
			t.Fatalf("first play failed: %s", result.String())
		}

		result := d.handleCommand(t.Context(), "route play r")
		if !result.IsError() || result.String() != errorPrefix+respRouteAlreadyPlaying {
			t.Errorf("second play = %q, want %q", result.String(), respRouteAlreadyPlaying)
		}

		if stopResult := d.handleCommand(t.Context(), "route stop"); stopResult.IsError() {
			t.Fatalf("cleanup stop failed: %s", stopResult.String())
		}

		d.routePlaybackWG.Wait()
	})

	t.Run("empty route rejected", func(t *testing.T) {
		t.Parallel()

		d := testDaemonWithDevice(t, pixy.StateTracking)
		seedRoute(t, d, "r", nil)

		result := d.handleCommand(t.Context(), "route play r")
		if !result.IsError() || result.String() != errorPrefix+respRouteEmptyRecording {
			t.Errorf("play empty route = %q, want %q", result.String(), respRouteEmptyRecording)
		}
	})
}

// --- Web surface ---

func TestWeb_RouteRecordStopSaveDeleteFlow(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t, pixy.StateTracking, testVideoDev, testHIDDev, withNoopV4L2(), withNoopParsePTZ(), withRouteTrackingFake(nil))
	warmPTZCache(d, pixy.PTZValues{Pan: 0, Tilt: 0, Zoom: pixy.ZoomDefault})
	server := newTestWebServer(t, d)

	startResp := post(t, server.URL+"/api/route/record/start", "application/json", nil)
	defer startResp.Body.Close()
	assertStatusCode(t, startResp, http.StatusOK)
	startBody := getBody(t, startResp)
	assertContains(t, startBody, `id="route-status-recording"`, "route start swaps in recording controls")
	assertContains(t, startBody, "mode-idle active", "camera mode UI shows tracking suspended")
	assertContains(t, startBody, `aria-label="Track" disabled=""`, "tracking control is locked during recording")
	assertContains(t, startBody, `id="slider-pan"`, "PTZ sliders remain rendered during recording")

	panResp := post(t, server.URL+"/api/ptz/pan", "application/json", strings.NewReader(`{"pan":10}`))
	defer panResp.Body.Close()
	assertStatusCode(t, panResp, http.StatusOK)

	stopResp := post(t, server.URL+"/api/route/record/stop", "application/json", nil)
	defer stopResp.Body.Close()
	assertStatusCode(t, stopResp, http.StatusOK)
	stopBody := getBody(t, stopResp)
	assertContains(t, stopBody, "1 steps", "record stop toast")
	assertContains(t, stopBody, `id="route-name-input"`, "route stop offers route naming controls")
	assertContains(t, stopBody, "mode-track active", "camera mode UI shows the prior tracking mode restored")
	assertNotContains(t, stopBody, `aria-label="Track" disabled=""`, "tracking control unlocks after recording")

	saveResp := post(t, server.URL+"/api/route/save/my%20route", "application/json", nil)
	defer saveResp.Body.Close()
	assertStatusCode(t, saveResp, http.StatusOK)
	assertContains(t, getBody(t, saveResp), "my route", "save toast mentions the full name")

	d.mu.RLock()
	_, saved := d.state.Routes["my route"]
	d.mu.RUnlock()

	if !saved {
		t.Fatal("route \"my route\" not present in state after web save")
	}

	pageResp := get(t, server.URL+"/")
	defer pageResp.Body.Close()
	page := getBody(t, pageResp)
	assertContains(t, page, "my route", "main page lists the saved route")
	assertContains(t, page, `class="preset-chip route-chip"`, "saved route is displayed as a preset-style chip")

	deleteResp := post(t, server.URL+"/api/route/delete/my%20route", "application/json", nil)
	defer deleteResp.Body.Close()
	assertStatusCode(t, deleteResp, http.StatusOK)

	d.mu.RLock()
	_, stillSaved := d.state.Routes["my route"]
	d.mu.RUnlock()

	if stillSaved {
		t.Error("route \"my route\" still present in state after web delete")
	}
}

func TestWeb_RoutePlayWithLoopAndPingPongOptions(t *testing.T) {
	t.Parallel()

	var moves []v4l2Call

	d := newTestDaemon(t, pixy.StateTracking, testVideoDev, testHIDDev, withCaptureV4L2(&moves), withRouteTrackingFake(nil))
	seedRoute(t, d, "r", []pixy.RouteStep{{Pan: 1}, {Pan: 2, WaitMs: 2}})
	server := newTestWebServer(t, d)

	playResp := post(t, server.URL+"/api/route/play/r?loop=true&pingpong=true", "application/json", nil)
	defer playResp.Body.Close()
	assertStatusCode(t, playResp, http.StatusOK)
	playBody := getBody(t, playResp)
	assertContains(t, playBody, "loop, ping-pong", "play toast mentions loop+ping-pong")
	assertContains(t, playBody, "mode-idle active", "camera mode UI shows tracking suspended during playback")
	assertContains(t, playBody, `id="route-stop-btn"`, "playback swaps in the stop control")

	time.Sleep(30 * time.Millisecond)

	stopResp := post(t, server.URL+"/api/route/stop", "application/json", nil)
	defer stopResp.Body.Close()
	assertStatusCode(t, stopResp, http.StatusOK)
	stopBody := getBody(t, stopResp)
	assertContains(t, stopBody, `id="route-record-start-btn"`, "stop playback restores the start control")
	assertNotContains(t, stopBody, `id="route-stop-btn"`, "stop playback removes the stop control")
	assertContains(t, stopBody, "mode-track active", "stop playback restores tracking in the panel")
	assertNotContains(t, stopBody, `aria-label="Track" disabled=""`, "tracking control unlocks after playback")

	d.routePlaybackWG.Wait()

	if len(panSequence(moves)) < 2 {
		t.Errorf("only %d pan writes captured over the web play/stop flow", len(panSequence(moves)))
	}
}

func TestRouteStop_NotPlayingRejected(t *testing.T) {
	t.Parallel()

	d := testDaemonNoDevice(t)

	result := d.handleCommand(t.Context(), "route stop")
	if !result.IsError() || result.String() != errorPrefix+respRouteNotPlaying {
		t.Errorf("stop while idle = %q, want %q", result.String(), respRouteNotPlaying)
	}
}

// panSequence extracts the sequence of pan_absolute values (in degrees, the
// multiplier undone) from a captured v4l2Call log, in call order.
func panSequence(calls []v4l2Call) []int {
	out := make([]int, 0, len(calls))

	for _, c := range calls {
		if c.ctrl != "pan_absolute" {
			continue
		}

		n, err := strconv.Atoi(c.val)
		if err != nil {
			continue
		}

		out = append(out, n/v4l2UnitsPerDegree)
	}

	return out
}
