//go:build linux

// Package main implements the emeet-pixyd daemon for the EMEET PIXY webcam.
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/LarsArtmann/emeet-pixyd/internal/pixy"
	"github.com/coreos/go-systemd/v22/daemon"
	errorfamily "github.com/larsartmann/go-error-family"
)

// Build info, overridden via -ldflags.
//
//nolint:gochecknoglobals
var buildVersion = "dev"

type Daemon struct {
	mu        sync.RWMutex
	hidMu     sync.Mutex
	v4l2Mu    sync.Mutex
	state     pixy.State
	config    pixy.Config
	videoDev  string
	hidrawDev string
	model     pixy.Model
	// captureModeID controls the active FFmpeg/V4L2 stream profile. Guarded by d.mu.
	captureModeID string
	// unsupportedHint explains an EMEET device the probe recognized but
	// cannot control (e.g. a fixed C960); empty when a PIXY-family device
	// was found. Set only under d.mu via applyProbeResultLocked.
	unsupportedHint string
	hidDev          HIDDevice

	// Debounce counters: number of consecutive polls observing a stable
	// in-use or idle state. Both clamp to config.DebounceCount so the
	// >= check below is the trigger boundary, not unbounded growth.
	debounceInUse int
	debounceIdle  int
	hidFailCount  int
	autoError     error
	lastSyncedAt  time.Time

	// trapArmedUntil timestamps the firmware privacy-trap race window after
	// a tilt arrival in the bottom zone (see privacy_trap.go). Guarded by
	// d.mu.
	trapArmedUntil time.Time

	// trapReasserts tracks in-flight privacy-trap re-assert goroutines so
	// shutdown (and tests) can wait for their HID writes to finish.
	trapReasserts sync.WaitGroup

	// persistedIntent is true once the daemon has established authoritative
	// state on disk — either a valid file was loaded at startup or state was
	// saved during this session. It distinguishes "the user has expressed
	// intent" (persisted camera mode wins over hardware on device re-appear)
	// from "fresh install" (hardware is the source of truth). Deriving it
	// from any successful save (not a one-shot startup snapshot) is what
	// keeps a mid-session choice from being forgotten on the next replug.
	persistedIntent atomic.Bool

	lastFrame lastFrameCache

	ptzCache ptzCache

	// powerCache holds the last battery/charge reading (TODO #139) with a
	// TTL, so status/waybar surfaces cost at most one HID query per window.
	powerCache powerCache

	streamSema chan struct{}
	// activeVideoStream is the current HTTP-owned V4L2 capture (if any).
	// RTSP start cancels this session before claiming streamSema for its own
	// shared capture so the camera is never opened twice.
	activeVideoStream *activeVideoStream

	// broadcaster distributes state-change events to all connected SSE
	// clients via a thread-safe fan-out hub.
	broadcaster *Broadcaster

	// routeRecording holds in-progress route capture state ("Modo
	// recorrido"); nil when not recording. Guarded by d.mu.
	routeRecording *routeRecordingState

	// routePlaybackStopping stays true between cancellation and completion of
	// playback cleanup, preventing a second route from racing restoration.
	routePlaybackStopping bool
	routeShuttingDown     bool

	// routePlaybackTrackingRestore is the camera mode captured before playback
	// temporarily disables AI tracking. Guarded by d.mu.
	routePlaybackTrackingRestore routeTrackingRestore
	routePlaybackRestoreError    error

	// routePending holds a just-finished recording awaiting a name (the
	// same two-phase capture-then-name flow as preset save), or nil. Only
	// one of routeRecording/routePending is non-nil at a time. Guarded by
	// d.mu.
	routePending *pixy.Route

	// routePlaybackCancel cancels the in-progress route playback goroutine,
	// or nil when no route is playing. routePlayingName names it for the
	// web/CLI status surfaces. Guarded by d.mu.
	routePlaybackCancel context.CancelFunc
	routePlayingName    string

	// routePlaybackWG tracks the in-flight playback goroutine so shutdown
	// (and tests) can wait for its HID/V4L2 writes to finish.
	routePlaybackWG sync.WaitGroup

	// rtspMu serializes publication start/stop/exit transitions. The current
	// publication state is surfaced under d.mu; the URL credentials are never
	// stored in Daemon state or exposed through status surfaces.
	rtspMu          sync.Mutex
	rtspPublisher   *rtspPublisherProcess
	rtspState       rtspPublishState
	rtspDestination string
	rtspSavedURL    string
	rtspError       string

	// lifecycleCtx is the daemon's long-lived run context, canceled only on
	// shutdown. Route playback derives from it instead of from the
	// triggering HTTP request's context, so a route keeps running after
	// the request that started it completes, while still stopping cleanly
	// at shutdown. Set once in Run(); nil for a Daemon built directly in
	// tests, where routePlaybackContext falls back to context.Background().
	lifecycleCtx context.Context

	deps Dependencies
}

// routePlaybackContext returns the base context route playback derives its
// cancelable context from: the daemon's run-lifetime context when running
// under Run(), or context.Background() in tests that construct a Daemon
// directly.
func (d *Daemon) routePlaybackContext() context.Context {
	if d.lifecycleCtx != nil {
		return d.lifecycleCtx
	}

	return context.Background()
}

// routeRecordingState is the in-progress capture buffer for route recording.
// startedAt lines up with the first step's zero wait; lastAt advances after
// every captured step so each step's WaitMs is the gap since the previous
// one.
type routeRecordingState struct {
	steps           []pixy.RouteStep
	startedAt       time.Time
	lastAt          time.Time
	trackingRestore routeTrackingRestore
}

type activeVideoStream struct {
	cancel context.CancelFunc
	done   chan struct{}
}

const hidCircuitBreakerThreshold = 3

const (
	httpReadHeaderTimeout = 5 * time.Second
	httpReadTimeout       = 10 * time.Second
	httpWriteTimeout      = 30 * time.Second
	httpIdleTimeout       = 60 * time.Second
	httpMaxHeaderBytes    = 1 << 20
	ueventChBufSize       = 8
	shutdownTimeout       = 5 * time.Second
)

func NewDaemon(cfg pixy.Config) (*Daemon, error) {
	err := cfg.Validate()
	if err != nil {
		return nil, errorfamily.Wrap(err, errorfamily.Classify(err), "daemon.validate_config", "validate config")
	}

	//nolint:exhaustruct // remaining fields set below or zero-valued
	d := &Daemon{
		config:        cfg,
		state:         pixy.DefaultState(),
		captureModeID: defaultCaptureModeID,
		rtspState:     rtspStateStopped,
		streamSema:    make(chan struct{}, 1),
		broadcaster:   NewBroadcaster(),
	}
	//nolint:exhaustruct // remaining deps set below (circular ref on d.setTracking etc)
	d.deps = Dependencies{
		commander:      realCommandRunner{},
		ueventListener: netlinkUeventListener{},
		probeDevices:   probeDevices,
		isCameraInUse:  isCameraInUse,
		findSource:     d.findPixySource,
		setSource:      d.setDefaultSource,
		notify:         d.notifyCmd,
	}
	d.deps.setTracking = d.setTracking
	d.deps.setAudio = d.setAudio
	d.deps.setGesture = d.setGesture
	d.deps.centerCamera = d.centerCamera
	d.deps.v4l2Set = d.v4l2Set
	d.deps.parsePTZ = d.parsePTZValues
	// Persisted state wins on subsequent restarts; env-configured defaults apply
	// only on first run (no valid state file present). This way EMEET_PIXYD_AUTO
	// and EMEET_PIXYD_DEFAULT_AUDIO seed initial state, then the daemon takes over.
	d.persistedIntent.Store(d.loadState())
	d.rtspSavedURL = d.loadRTSPURL()

	if !d.persistedIntent.Load() {
		d.state.AutoMode = cfg.AutoMode
		d.state.Audio = cfg.DefaultAudio
	}

	// InCall is a runtime observation, not intent: a freshly started daemon
	// cannot be in a call before it begins watching /proc. Dropping any
	// persisted value prevents a stale "in call: yes" from surviving a
	// restart forever when auto-management is off (the monitor never runs).
	d.state.InCall = false

	registerMetrics()
	registerErrorFamilies()
	// NewDaemon runs before any goroutines exist, so we can call the
	// _Locked variant directly without taking d.mu.
	probe := d.deps.probeDevices(cfg.ExtraProductIDs)
	d.applyProbeResultLocked(probe)
	warnInaccessibleDevices(probe)
	checkExternalDeps(d.deps.commander, d.state.AutoMode)

	return d, nil
}

func checkExternalDeps(commander CommandRunner, autoMode pixy.AutoMode) {
	deps := []struct {
		binary string
		impact string
	}{
		{ffmpegBin, "MJPEG streaming unavailable"},
		{v4l2ctl, "PTZ control unavailable"},
	}
	if autoMode.SwitchesSource() {
		deps = append(deps, struct {
			binary string
			impact string
		}{wpctl, "PipeWire source switching unavailable"})
	}
	if !autoMode.IsOff() {
		deps = append(deps, struct {
			binary string
			impact string
		}{notifySend, "desktop notifications unavailable"})
	}

	for _, dep := range deps {
		_, err := commander.LookPath(dep.binary)
		if err != nil {
			slog.Warn("optional dependency not found", "binary", dep.binary, "impact", dep.impact)
		}
	}
}

func sdNotify(state string) {
	sent, err := daemon.SdNotify(false, state)
	if err != nil {
		slog.Warn("sd_notify failed", "error", err)
	} else if !sent {
		slog.Debug("sd_notify not sent (no NOTIFY_SOCKET)")
	}
}

func (d *Daemon) newHTTPServer() *http.Server {
	webSrv := &webServer{daemon: d}
	mux := newWebMux(webSrv)
	//nolint:exhaustruct
	return &http.Server{
		Addr: d.config.WebAddr,
		Handler: chain(
			securityHeaderMiddleware, loggingMiddleware, requestIDMW,
		)(mux),
		ReadHeaderTimeout: httpReadHeaderTimeout,
		ReadTimeout:       httpReadTimeout,
		WriteTimeout:      httpWriteTimeout,
		IdleTimeout:       httpIdleTimeout,
		MaxHeaderBytes:    httpMaxHeaderBytes,
	}
}

func (d *Daemon) Run() {
	createErr := os.MkdirAll(d.config.StateDir, pixy.PermissionStateDir)
	if createErr != nil {
		slog.Error("failed to create state dir", "error", createErr)
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	ctx, cancel := context.WithCancel(context.Background())
	d.lifecycleCtx = ctx
	go func() {
		listenErr := d.listenUnix(ctx)
		if listenErr != nil {
			slog.Error("unix socket error", "error", listenErr)
		}
	}()

	httpSrv := d.startHTTPServer()

	slog.Info("EMEET PIXY daemon started")
	sdNotify("READY=1")
	d.mu.Lock()
	slog.Info(
		"initial state",
		"camera",
		d.state.Camera,
		"audio",
		d.state.Audio,
		"auto",
		d.state.AutoMode,
	)
	d.mu.Unlock()

	// The camera may have been power-cycled while the daemon was down (or
	// boots with the daemon): reconcile belief and hardware without blocking
	// startup. hidMu serializes this against commands and autoManage.
	if d.videoDevice() != "" {
		go func() {
			d.hidMu.Lock()
			defer d.hidMu.Unlock()

			slog.Info("device present at startup, reconciling state with hardware")
			d.reconcileOnDeviceAppear(ctx)
		}()
	}

	d.eventLoop(ctx, cancel, sigs, httpSrv)
}

// broadcastStateChanged notifies all active SSE clients that they should
// refresh the UI. Uses a thread-safe fan-out hub for race-safe, non-blocking
// fan-out — slow clients drop events without stalling the daemon.
func (d *Daemon) broadcastStateChanged() {
	d.broadcaster.Broadcast()
}

func (d *Daemon) startHTTPServer() *http.Server {
	if d.config.WebAddr == "" {
		return nil
	}

	httpSrv := d.newHTTPServer()

	go func() {
		slog.Info("web UI starting", "addr", d.config.WebAddr)

		listenErr := httpSrv.ListenAndServe()
		if listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
			slog.Error("web server error", "error", listenErr)
		}
	}()

	return httpSrv
}

func (d *Daemon) handleShutdown(cancel context.CancelFunc, httpSrv *http.Server) {
	sdNotify("STOPPING=1")
	slog.Info("shutting down")
	d.hidMu.Lock()
	d.mu.Lock()
	d.routeShuttingDown = true
	d.saveStateOrLog("failed to save state on shutdown")
	if d.routePlaybackCancel != nil {
		d.routePlaybackCancel()
	}
	d.mu.Unlock()
	d.hidMu.Unlock()
	cancel()
	d.routePlaybackWG.Wait()
	d.stopRTSPOnShutdown()
	d.stopRouteRecordingOnShutdown()

	if removeErr := os.Remove(d.config.SocketPath()); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
		slog.Debug("stale socket removal on shutdown failed", "err", removeErr)
	}

	if httpSrv != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
		if shutdownErr := httpSrv.Shutdown(shutdownCtx); shutdownErr != nil {
			slog.Warn("web server shutdown failed", "err", shutdownErr)
		}

		shutdownCancel()
	}
}

func (d *Daemon) eventLoop(
	ctx context.Context,
	cancel context.CancelFunc,
	sigs <-chan os.Signal,
	httpSrv *http.Server,
) {
	ticker := time.NewTicker(d.config.PollInterval)
	defer ticker.Stop()

	ueventCh := make(chan struct{}, ueventChBufSize)
	go d.deps.ueventListener.Listen(ctx, ueventCh)

	for {
		select {
		case sig := <-sigs:
			if sig == syscall.SIGHUP {
				slog.Info("received SIGHUP, saving state")
				d.mu.Lock()
				d.saveStateOrLog("failed to save state on SIGHUP")
				d.mu.Unlock()

				continue
			}

			d.handleShutdown(cancel, httpSrv) //nolint:contextcheck // shutdown cancels the parent context

			return
		case <-ueventCh:
			slog.Info("device event detected, re-probing")
			// Lock order is v4l2Mu -> hidMu everywhere the two nest: PTZ move
			// paths hold v4l2Mu and briefly take hidMu to re-assert configured
			// motor speeds before the move. Keep this the only nested site in
			// the same order to make a lock-order inversion impossible.
			d.v4l2Mu.Lock()
			d.hidMu.Lock()
			d.mu.Lock()
			oldVideo := d.videoDev
			probe := d.deps.probeDevices(d.config.ExtraProductIDs)
			d.applyProbeResultLocked(probe)
			newVideo := d.videoDev
			d.mu.Unlock()
			d.broadcastStateChanged()

			if oldVideo == "" && newVideo != "" {
				slog.Info("device appeared, reconciling state")

				// The node may exist but be inaccessible (broken udev):
				// surface the fix hint when the device appears, not only at
				// daemon startup. Rate-limited against replug storms.
				warnInaccessibleDevicesLimited(probe, deviceAccessWarnLimiter)

				d.reconcileOnDeviceAppear(ctx)
			}
			d.hidMu.Unlock()
			d.v4l2Mu.Unlock()
		case <-ticker.C:
			d.autoManage(ctx)
			sdNotify("WATCHDOG=1")
		}
	}
}

func exitWithDaemonError(err error) {
	if err != nil {
		_, dieErr := fmt.Fprintf(os.Stderr, "Error: %v\nIs emeet-pixyd running?\n", err)
		_ = dieErr

		os.Exit(errorfamily.ExitCode(err))
	}
}

// helpText is printed for --help. "preset" repeats as a command prefix.
//
//nolint:dupword // command help text naturally repeats the command prefix
const helpText = `Usage: emeet-pixyd [command]

Run without arguments to start the daemon.
Run with a command to send it to a running daemon via Unix socket.

Commands:
  status            Show current camera status
  waybar            Output waybar-compatible JSON
  version           Print version
  sync              Sync state from hardware
  probe             Re-probe for PIXY
  device            Show device paths
  track             Enable tracking mode
  idle              Set idle mode
  privacy           Enable privacy mode
  toggle-privacy    Toggle privacy on/off
  center            Center camera (pan/tilt/zoom reset)
  audio [mode]      Cycle or set audio mode (nc, live, org/original)
  gesture-on        Enable gesture control
  gesture-off       Disable gesture control
  toggle-gesture    Toggle gesture control
  auto              Show current auto mode
  auto-on           Enable auto mode (full)
  auto-off          Disable auto mode
  toggle-auto       Toggle auto mode
  pan <degrees>     Set pan position (absolute; rel+/- for relative)
  tilt <degrees>    Set tilt position (absolute; rel+/- for relative)
  zoom <value>      Set zoom level (absolute; rel+/- for relative)
  speed <axis> <v>  Set motor speed over HID (axis: pan, tilt, zoom)
  battery           Show battery/charge if the device supports it
  tracking <mode>   Set tracking variant (face, halfbody, fullbody)
  preset push <name>    Mirror a saved preset into a hardware motor slot
  preset pull           Sweep hardware slots into hw-N presets (--dry-run reports only)
  preset save <name>    Save current PTZ position as named preset
  preset load <name>    Recall a saved preset
  preset delete <name>  Delete a saved preset
  preset list           List all saved presets`

func handleFlag() bool {
	if len(os.Args) < 2 {
		return false
	}

	switch os.Args[1] {
	case "--version", "-v":
		_, printErr := fmt.Fprintln(os.Stdout, "emeet-pixyd", buildVersion)
		if printErr != nil {
			slog.Debug("failed to print version", "error", printErr)
		}

		return true
	case "--help", "-h":
		if _, printErr := fmt.Fprintln(os.Stdout, helpText); printErr != nil {
			slog.Debug("help print failed", "err", printErr)
		}

		return true
	default:
		return false
	}
}

func main() {
	cfg := pixy.ConfigFromEnv()

	if cfg.Debug {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}

	if handleFlag() {
		return
	}

	if len(os.Args) > 1 {
		cmd := strings.Join(os.Args[1:], " ")

		resp, err := sendCommand(cfg, cmd)
		exitWithDaemonError(err)

		_, printErr := fmt.Fprintln(os.Stdout, resp)
		if printErr != nil {
			slog.Debug("failed to print response", "error", printErr)
		}

		return
	}

	d, err := NewDaemon(cfg)
	if err != nil {
		errorfamily.LogError(err, slog.Default())
		os.Exit(errorfamily.ExitCode(err))
	}

	d.Run()
}
