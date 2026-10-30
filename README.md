<h1 align="center">
  emeet-pixyd
  <br>
  <sub>Auto-activation daemon for the EMEET PIXY dual-camera AI webcam</sub>
</h1>

<p align="center">
  <strong>A great AI webcam, dumb on Linux — until now. Works with the PIXY and PIXY 2K:</strong>
  <br>
  face tracking on call start · privacy mode on call end · audio switching · DataStar web UI
</p>

<p align="center">
  <a href="https://emeet-pixyd.lars.software">
    <img alt="Documentation" src="https://img.shields.io/badge/docs-emeet--pixyd.lars.software-8b5cf6?logo=astro">
  </a>
  <a href="https://emeet-pixyd.lars.software/#demo">
    <img alt="Watch the 25s demo" src="https://img.shields.io/badge/watch_the_25s_demo-emeet--pixyd.lars.software-red?logo=youtube">
  </a>
  <br>
  <a href="https://github.com/magm3333/emeet-pixyd/actions/workflows/go-test.yml">
    <img alt="Go tests" src="https://github.com/magm3333/emeet-pixyd/actions/workflows/go-test.yml/badge.svg">
  </a>
  <a href="https://github.com/magm3333/emeet-pixyd/actions/workflows/nix.yml">
    <img alt="Nix build" src="https://github.com/magm3333/emeet-pixyd/actions/workflows/nix.yml/badge.svg">
  </a>
  <img alt="Go 1.27+" src="https://img.shields.io/badge/Go-1.27+-00ADD8?logo=go">
  <img alt="Linux only" src="https://img.shields.io/badge/platform-Linux-FCC624?logo=linux">
  <a href="https://www.emeet.com">
    <img alt="EMEET PIXY" src="https://img.shields.io/badge/device-EMEET_PIXY-328f00?logo=usb">
  </a>
  <a href="LICENSE">
    <img alt="License: MIT" src="https://img.shields.io/badge/license-MIT-blue.svg">
  </a>
</p>

---

**Language / Idioma:** [English](#english) · [Español](#español)

## English

> **Fork:** this repository is based on the original
> [LarsArtmann/emeet-pixyd](https://github.com/LarsArtmann/emeet-pixyd) project
> (MIT). This fork adds PTZ route recording/playback, simultaneous RTSP publishing
> and web preview, a persistent Spanish/English UI, the optional coverage globe,
> capture-mode controls, and Docker images for `linux/amd64`, `linux/arm64`, and
> `linux/arm/v7` (32-bit Raspberry Pi 3).

**Contributor:** Mariano García Mattío (`magm`) — [magm3333@gmail.com](mailto:magm3333@gmail.com).

## Why

The EMEET PIXY is a great dual-camera AI webcam — but on Linux, you're stuck manually enabling face tracking before calls, switching audio sources, and remembering to enable privacy mode when you're done. **emeet-pixyd automates all of that.**

It watches `/proc` to detect when a video call starts (Zoom, Teams, Google Meet, anything that opens the camera), then:

1. **Activates** face tracking + noise cancellation
2. **Switches** your PipeWire audio source to the PIXY microphone
3. When the call ends, enters **privacy mode** (lens physically blocked)

No setup per app. No browser extension. Works with anything that opens `/dev/video*`.

## Who is this for?

- **Linux users with an EMEET PIXY** who want face tracking and privacy to "just work" on every call.
- **Remote workers** tired of manually enabling tracking before meetings and remembering privacy mode after.
- **NixOS users** who want a declarative, reproducible setup via a single module option.
- **Waybar / Quickshell users** who want live camera status (mode, PTZ, in-call) in their bar.
- **Privacy-conscious users** who want a guaranteed physical lens block whenever no call is active.

## When NOT to use this

Skip this daemon if:

- You don't own a PIXY-family device — the automation targets the EMEET PIXY (`328f:00c0`) and PIXY 2K (`328f:0118`). Other EMEET webcams (C960, C950, S600, …) are standard UVC cameras that the Linux kernel already drives completely; the daemon recognizes them and says so, but has nothing to automate. See [Supported Devices](#supported-devices). For general webcam tooling, reach for [webcamoid](https://webcamoid.github.io/) instead.
- You're on **macOS or Windows** — this is Linux-only by design (HID hidraw, V4L2, `/proc`, netlink uevents).
- The camera's own tracking toggle is enough for you — if you never forget to enable it, you don't need a daemon.
- You want cloud or host-side AI inference — person tracking and gesture recognition run in the camera firmware; the daemon only controls those features over HID. It makes no cloud calls and sends no telemetry.
- You need **multi-vendor webcam management** — this targets one device family. See [Related Tools](https://emeet-pixyd.lars.software/related-tools/) for general-purpose alternatives.

## Supported Devices

| Device                | USB ID                  | Support                                                                                             |
| --------------------- | ----------------------- | --------------------------------------------------------------------------------------------------- |
| EMEET PIXY            | `328f:00c0`             | **Full** — tracking, privacy, audio, PTZ, presets, battery                                          |
| EMEET PIXY 2K         | `328f:0118`             | **Full** — same protocol as the PIXY                                                                |
| EMEET C960 / C960 2K  | `328f:003f` / `328f:2013` / `328f:007c` | Recognized — fixed UVC camera, fully driven by the standard kernel driver; nothing to automate |
| EMEET C950 / C970     | `328f:0073` / `328f:002d` | Recognized — fixed UVC camera                                                                       |
| EMEET S600L           | `328f:00ef`             | Recognized — fixed UVC camera                                                                       |
| Other EMEET devices   | `328f:*`                | Detected as "unknown EMEET device" — the daemon logs the product ID and how to report or opt in     |

The daemon never sends vendor commands to devices outside the PIXY family: fixed EMEET webcams have no vendor control surface, so there is nothing to automate — and no reason to grant extra device permissions. When a recognized-but-unsupported device is plugged in without a PIXY, the daemon logs one rate-limited hint explaining exactly that, and `emeet-pixy device` repeats it.

**Own an unlisted PIXY variant?** Point the daemon at it without waiting for a release:

```bash
EMEET_PIXYD_EXTRA_PRODUCT_IDS=0x0119 emeet-pixyd   # comma-separated hex IDs
```

Extra IDs are treated as PIXY-family devices and show up as `PIXY (PID 0x…)` in logs and the UI.

## Comparison

| Approach                   | Auto call detection | Privacy on end | Audio switching | Hotplug | Linux-native |
| -------------------------- | :-----------------: | :------------: | :-------------: | :-----: | :----------: |
| **emeet-pixyd**            |          ✓          |       ✓        |        ✓        |    ✓    |      ✓       |
| Manual `v4l2-ctl` per call |                     |                |                 |         |      ✓       |
| EMEET STUDIO (official app) |          ✓          |                |        ✓        |         |              |
| webcamoid                  |                     |                |                 |         |      ✓       |

The differentiator is the first column: emeet-pixyd is the only option that detects calls by watching `/proc`, so it works with **any** app that opens the camera — no per-app setup, no browser extension.

### Not an OBS fork — and not trying to be

EMEET STUDIO, the official Windows/macOS app, is a **~350 MB OBS Studio fork**:
scenes, sources, transitions, a virtual camera, and ByteDance beauty filters ride
an embedded `libobs` (verified by disassembling the installer — see
[`docs/emeet-studio-official-app-comparison.md`](docs/emeet-studio-official-app-comparison.md)).
It also has **zero call awareness** — it never detects that you are in a call.

**If you want OBS, use OBS.** It is free, open source, and better at being OBS.

**If you want the PIXY's hardware-specific features to just work on Linux** —
tracking, the privacy shutter, audio modes, PTZ, presets — **use emeet-pixyd.**
It speaks the same wire protocol as the official app, headless, in a single
small binary, and automates what the official app leaves manual even on its
own platforms.

The two can coexist: point OBS at `/dev/video0` and let emeet-pixyd handle the
camera's intelligence underneath it.

## Features

| Feature                | Description                                                                                      |
| ---------------------- | ------------------------------------------------------------------------------------------------ |
| **Call detection**     | Scans `/proc/*/fd` for processes holding the video device — works with any app                   |
| **Auto-activate**      | Enables face tracking + noise cancellation when a call starts                                    |
| **Auto-privacy**       | Physically blocks the lens when the call ends                                                    |
| **Audio switching**    | Auto-switches PipeWire default source to PIXY on call start                                      |
| **Web UI**             | Dark-themed DataStar control panel with live MJPEG preview, PTZ sliders, and toast notifications |
| **Modo recorrido**     | Record manual PTZ routes, save named routes, and replay once, looped, or ping-pong             |
| **Route safety**       | Suspends camera AI tracking during recording/playback and restores the exact previous mode     |
| **Bilingual UI**       | Persistent Spanish/English selector                                                          |
| **Coverage globe**     | Optional interactive semi-transparent PTZ sphere; drag to move, see the live aim and fading movement trail |
| **Phone UI**           | Full-screen preview with touch pan/tilt dragging, directional nudges, zoom, and on-video Track/Idle/Privacy controls |
| **RTSP publishing**    | Configure a destination, publish H.264 over RTSP/TCP, pause, and inspect sanitized FFmpeg errors |
| **RTSP + preview**     | One V4L2 capture feeds both the RTSP publisher and the web preview; the configured URL is saved locally with mode `0600` |
| **Docker**             | Multi-platform image for amd64, arm64 and 32-bit Raspberry Pi 3 (`arm/v7`)                    |
| **Waybar integration** | JSON output for a custom Waybar module                                                           |
| **Status API**         | `GET /api/status` JSON for Quickshell / DankMaterialShell bar widgets                            |
| **Hotplug**            | Netlink uevent listener detects USB plug/unplug, auto-re-probes                                  |
| **Prometheus metrics** | OTel-based metrics at `/metrics` for monitoring                                                  |
| **NixOS module**       | Systemd user service, udev rules, tmpfiles.d — one option to enable                              |

## Quick Start

### NixOS (recommended)

Add the flake input and enable the module:

```nix
# flake.nix
inputs.emeet-pixyd.url = "github:magm3333/emeet-pixyd";

# In your nixosConfiguration:
inputs.emeet-pixyd.nixosModules.default
```

Then in your configuration:

```nix
hardware.emeet-pixy = {
  enable = true;
  auto = "full";           # off | full | tracking-only | privacy-only
  defaultAudio = "nc";     # nc | live | org
  debug = false;
};
```

That's it. The module installs the daemon, udev rules, systemd user service, and all runtime dependencies.

### Build from Source

```bash
git clone https://github.com/magm3333/emeet-pixyd.git
cd emeet-pixyd
nix build    # or, with a Go ≥ 1.27.1 toolchain: go build -o emeet-pixyd .
```

### Docker (amd64, arm64 and Raspberry Pi 3 arm/v7)

The multi-platform image is published as
[`magm3333/emeet-pixy-dual-cam:latest`](https://hub.docker.com/r/magm3333/emeet-pixy-dual-cam)
for `linux/amd64`, `linux/arm64`, and `linux/arm/v7` (Raspberry Pi 3 32-bit).
See [`docker/README.md`](docker/README.md)
for camera device permissions, Compose setup, reproducible builds, and test
checklists.

## Usage

Run without arguments to start the daemon, or pass a command to communicate via Unix socket:

```
emeet-pixy status               # Full status (camera, audio, gesture, PTZ, in-call, auto)
emeet-pixy track                # Enable face tracking
emeet-pixy idle                 # Set camera to idle
emeet-pixy privacy              # Enable privacy mode
emeet-pixy toggle-privacy       # Toggle privacy mode
emeet-pixy center               # Center camera (pan=0, tilt=0, zoom=100)
emeet-pixy audio [mode]         # Cycle or set audio mode (nc, live, org)
emeet-pixy gesture-on           # Enable gesture control
emeet-pixy gesture-off          # Disable gesture control
emeet-pixy toggle-gesture       # Toggle gesture control
emeet-pixy auto [mode]          # Set auto mode (off, full, tracking-only, privacy-only)
emeet-pixy auto-on              # Enable full auto mode
emeet-pixy auto-off             # Disable auto mode
emeet-pixy toggle-auto          # Toggle auto mode
emeet-pixy pan <value>          # Set pan (−150 to 150; or rel+/-N for relative)
emeet-pixy tilt <value>         # Set tilt (−90 to 90; or rel+/-N for relative)
emeet-pixy zoom <value>         # Set zoom (100 to 150; or rel+/-N for relative)
emeet-pixy speed <axis> <value> # Set motor speed for PTZ moves (axis: pan, tilt, zoom)
emeet-pixy tracking <variant>   # Set tracking variant (none, face, halfbody, fullbody)
emeet-pixy battery              # Show battery/charge (omitted if the device doesn't answer)
emeet-pixy preset save <name>   # Save current PTZ position as a named preset
emeet-pixy preset load <name>   # Recall a saved preset
emeet-pixy preset push <name>   # Mirror a saved preset into a hardware motor slot
emeet-pixy preset pull          # Sweep hardware slots into hw-N presets (read-only)
emeet-pixy preset pull --dry-run # Preview the sweep without storing anything
emeet-pixy preset list          # List all saved presets
emeet-pixy route record start   # Start recording manual PTZ moves
emeet-pixy route record stop    # Stop a recording and keep its captured steps pending
emeet-pixy route record discard # Discard an in-progress/pending recording
emeet-pixy route save <name>    # Save the pending recording under a name
emeet-pixy route delete <name>  # Delete a saved route
emeet-pixy route play <name> [loop] [pingpong]
emeet-pixy route stop           # Stop playback and restore camera mode
emeet-pixy route list           # List saved routes
emeet-pixy sync                 # Sync daemon state from camera hardware
emeet-pixy probe                # Re-detect device (video + hidraw)
emeet-pixy device               # Show current video device path + model
emeet-pixy waybar               # Output Waybar JSON
```

## Auto Modes

The daemon supports four auto-management strategies (full reference:
[emeet-pixyd.lars.software/guides/auto-modes](https://emeet-pixyd.lars.software/guides/auto-modes/)):

| Mode             | On call start                                               | On call end  |
| ---------------- | ----------------------------------------------------------- | ------------ |
| `full` (default) | Face tracking + noise cancellation + PipeWire source switch | Privacy mode |
| `tracking-only`  | Face tracking                                               | Privacy mode |
| `privacy-only`   | Nothing                                                     | Privacy mode |
| `off`            | — (manual mode, see [Manual Control](#manual-control))      | —            |

### Manual Control

`auto = off` disables the `/proc` call-detection monitor entirely: the daemon
never scans for processes using the camera, so whatever mode you set —
tracking, idle, or privacy — persists until you change it. No app needs to
hold the camera open for tracking to stay active.

Your mode even survives reboots, replugs, and camera power cycles: the camera
resets itself to its boot default when it loses power, and the daemon detects
this on startup or hotplug and re-applies your persisted mode (so privacy
mode physically re-blocks the lens after a power cut). Audio and gesture
settings are adopted from the hardware on replug instead.

This is the right mode for headless or on-demand setups, e.g. a
[Home Assistant](https://www.home-assistant.io/) voice/vision assistant that
switches tracking/privacy on demand and grabs focused snapshots via FFmpeg:

```bash
emeet-pixy auto-off         # or: EMEET_PIXYD_AUTO=off / NixOS: auto = "off"
emeet-pixy track            # tracking stays on, no capture required
emeet-pixy privacy          # physically block the lens when the assistant is idle
```

## Web UI

The daemon serves a dark-themed control panel at `http://127.0.0.1:8090` with:

- Live MJPEG camera preview (full-width hero)
- Camera mode cards (Track / Idle / Privacy) with SVG icons and keyboard shortcuts (T / I / P / C)
- Audio mode selector (Noise Cancel / Live / Original) and optional PIXY microphone monitor streamed to the browser over the LAN
- PTZ sliders (pan ±150°, tilt ±90°, zoom 100–150×) with a spatial position radar
- Motor-speed sliders and a tracking-variant picker (face / half-body / full-body)
- Snapshot button to capture still frames
- Preset save/load/delete chips plus hardware-slot mirroring (`preset push` to slots, `preset pull` to sweep slots back)
- Route record/save/discard/play/delete controls and a list of saved routes
- Recording/playback automatically changes Track to Idle while the route runs and restores the exact prior Track/Idle/Privacy mode when it ends or is stopped
- Loop and ping-pong playback options; camera mode controls are locked while a route is active
- Persistent ES/EN language selector and selectable capture resolution/FPS
- Optional semi-transparent PTZ coverage globe with the unreachable 60° rear sector; drag it to move, watch the live marker and fading route trail, and hide it with the eye button
- On narrow/coarse-pointer screens, a video-only layout with touch pan/tilt/zoom and camera-mode controls
- In desktop view, configure an RTSP target, start/pause H.264 publishing, and see connection errors without exposing URL credentials in the status panel
- A single V4L2 capture supplies the RTSP stream and web preview simultaneously; the destination URL is saved under the state directory with permissions `0600`
- Camera model badge and battery row (shown when the device reports it)
- Gesture control and auto-mode toggles
- Toast notifications for state changes
- Live updates via SSE (Server-Sent Events)

## Configuration

### Environment Variables

All config is via environment variables (no CLI flags — `os.Args` is reserved for socket commands):

| Variable                     | Default            | Description                                                                                               |
| ---------------------------- | ------------------ | --------------------------------------------------------------------------------------------------------- |
| `EMEET_PIXYD_STATE_DIR`      | `/run/emeet-pixyd` | Runtime state directory (socket, state.json, and saved RTSP destination)                                   |
| `EMEET_PIXYD_WEB_ADDR`       | `127.0.0.1:8090`   | Web UI listen address (localhost only)                                                                    |
| `EMEET_PIXYD_POLL_INTERVAL`  | `2s`               | Call detection polling interval (Go duration)                                                             |
| `EMEET_PIXYD_DEBOUNCE_COUNT` | `3`                | Consecutive polls before state change                                                                     |
| `EMEET_PIXYD_DEBUG`          | `false`            | Enable pprof endpoints at `/debug/pprof/`                                                                 |
| `EMEET_PIXYD_AUTO`           | `full`             | Auto mode: off (manual, no /proc monitoring), full, tracking-only, privacy-only (legacy: true/1, false/0) |
| `EMEET_PIXYD_DEFAULT_AUDIO`  | `nc`               | Default audio mode: nc, live, org                                                                         |
| `EMEET_PIXYD_EXTRA_PRODUCT_IDS` | *(empty)*       | Comma-separated hex USB product IDs treated as PIXY-family variants (e.g. `0x0119,01ab`)                  |

### NixOS Module Options

| Option                             | Type   | Default  | Description                             |
| ---------------------------------- | ------ | -------- | --------------------------------------- |
| `hardware.emeet-pixy.enable`       | bool   | `false`  | Enable the daemon                       |
| `hardware.emeet-pixy.user`         | string | `"lars"` | User owning the runtime state directory |
| `hardware.emeet-pixy.auto`         | enum   | `"full"` | Auto-management strategy                |
| `hardware.emeet-pixy.defaultAudio` | enum   | `"nc"`   | Default audio mode                      |
| `hardware.emeet-pixy.debug`        | bool   | `false`  | Enable debug mode                       |

## Runtime Dependencies

The daemon integrates with these Linux tools (all provided by the NixOS module):

| Tool          | Purpose                           | Required            |
| ------------- | --------------------------------- | ------------------- |
| `v4l2-ctl`    | Pan/tilt/zoom control via V4L2    | Yes                 |
| `ffmpeg`      | MJPEG preview, H.264 RTSP, and ALSA-to-Ogg/Opus mic monitoring | For preview/RTSP/audio monitor |
| `wpctl`       | PipeWire default source switching | For audio switching |
| `notify-send` | Desktop notifications             | Optional            |

## Architecture

```
main() → NewDaemon() → Run()
  ├── Unix socket listener    Command routing (status, track, privacy, PTZ, …)
  ├── HTTP server              Web UI + API + Prometheus /metrics
  ├── Polling ticker (2s)      /proc scanning → call detection + debounce
  ├── Netlink uevent listener  USB hotplug detection
  └── systemd sd_notify        READY=1 + WATCHDOG=1
```

### Source Layout

```
main.go             Entry point, daemon lifecycle, signal handling, status/waybar output
commands.go         Command routing (socket + CLI), PTZ/audio/auto/gesture handlers
handlers.go         HTTP routing, web handlers, toast propagation, PTZ axis lookup table
metrics.go          OTel metrics registration and updates
stream.go           MJPEG streaming, snapshot, JPEG frame extraction, checkDevice guard
rtsp.go             RTSP FFmpeg publisher, shared preview-frame fan-out, saved destination
capture_mode.go     Camera capture profiles and model-specific resolution choices
middleware.go       Middleware variable assignments (implementations live in http.go)
sse.go              SSE-only: Broadcaster (thread-safe fan-out), per-client SSE, event helpers
http.go             HTTP helpers: writeJSON, middleware chain, security/request/logging middleware
ptz.go              PTZ logic: parsePTZValue, axis dispatch, V4L2 control via v4l2-ctl
hid.go              HID bidirectional communication (config/query over hidraw)
device.go           HID device state management: setDeviceState, setTracking/Audio/Gesture, centerCamera
process.go          /proc scanning for call detection, PipeWire, notifications
uevent.go           Netlink uevent listener for hotplug (context-cancellable)
uevent_linux.go     Low-level unix.Socket for netlink
auto.go             Auto-manage loop, call start/end, debounce logic
state.go            State persistence (JSON, atomic write via tmpfile + rename)
probe.go            Device probing (sysfs walks for video4linux + hidraw)
commander.go        CommandRunner interface (subprocess logging), realCommandRunner, noopCommandRunner
deps.go             Dependencies struct (CommandRunner + DI function pointers)
waybar.go           Waybar integration: waybarJSON struct, tooltip builder
web_types.go        webStatus struct shared between handlers and templates
errors.go           CommandError type, CommandResult, exported sentinel errors
cache.go            Named cache types: lastFrameCache, ptzCache (encapsulated mutex access)
templates.templ     HTML templates (compiled via templ generate)
internal/pixy/      Shared types: Config, State, CameraState, AudioMode, PID, SourceID, PTZ constants
static/             Frontend assets (DataStar, app.js, style.css) — go:embed
```

### Key Design Decisions

- **HID protocol**: Commands are 9-byte config reports followed by a commit report with a 200ms inter-report delay. Responses are 64-byte reads parsed by byte position. Newer V2 command families (`speed`, tracking variants, `battery`, `preset push`, identity queries) use single head-framed reports (`[0x09, iface, category, cmd]`) from the official protocol instead.
- **State persistence**: JSON file at `{StateDir}/state.json`, atomic write via `.tmp` + rename. Loaded state always wins over defaults.
- **Call detection**: Scans `/proc/*/fd` for processes holding the video device open, excluding self and descendants. Debounced (default 3 cycles × 2s = 6s).
- **Dependency injection**: All external interactions (HID, v4l2, PipeWire, notifications) are function fields on `Daemon`, enabling full test injectability without interfaces.
- **Branded types**: `PID` and `SourceID` use phantom-type branding to prevent mixing process IDs and PipeWire source IDs at compile time.

## Troubleshooting

| Problem                           | Solution                                                                                                                |
| --------------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| `PIXY not connected`              | Check USB connection, run `lsusb \| grep 328f`. Ensure udev rules are loaded.                                           |
| `Permission denied` on hidraw     | Verify udev rules match vendor `328f`, product `00c0` (PIXY) or `0118` (PIXY 2K). Run `udevadm control --reload-rules`. |
| `v4l2-ctl: command not found`     | Install `v4l-utils` (provided by NixOS module).                                                                         |
| No audio switching                | Check `wpctl status` shows a PIXY source. `wpctl` must be in PATH.                                                      |
| Camera not detected after plug-in | Daemon auto-detects via netlink uevents. Check `emeet-pixy probe` output.                                               |
| "recognized but not controllable: EMEET C960 …" | You have a fixed EMEET webcam, not a PIXY — the standard UVC driver already handles it; emeet-pixyd automates only the PIXY family. Nothing to fix. |
| "unknown EMEET device (USB product ID 0x…)" | Unregistered EMEET hardware. If it is a PIXY-family variant, set `EMEET_PIXYD_EXTRA_PRODUCT_IDS=0x<pid>`; otherwise open an issue with the ID. |
| Web UI shows "Camera in use"      | Another process (e.g., OBS, another browser tab) is holding `/dev/video*` open.                                         |
| Debounce too slow                 | Reduce `EMEET_PIXYD_DEBOUNCE_COUNT` (default 3) or `EMEET_PIXYD_POLL_INTERVAL` (default 2s).                            |

## Development

```bash
nix develop             # Enter dev shell (go, golangci-lint, templ)
nix build               # Build the daemon
templ generate           # Regenerate HTML templates after editing .templ

# Test (use GOWORK=off — parent go.work doesn't include this project)
GOWORK=off go test -race -count=1 ./...

# Lint
GOWORK=off golangci-lint run --timeout 2m ./...
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full workflow (pre-commit lint gate, template regeneration, issue reporting).

## License

MIT — see [LICENSE](LICENSE).

---

<p align="center">
  <a href="https://emeet-pixyd.lars.software">Full documentation at emeet-pixyd.lars.software</a>
</p>

---

## Español

**emeet-pixyd** es un daemon para Linux que automatiza las funciones de las cámaras EMEET PIXY y PIXY 2K. Detecta videollamadas observando qué procesos abren la cámara: activa seguimiento facial y cancelación de ruido al comenzar una llamada, cambia la entrada de audio a la PIXY y activa el modo privacidad al terminarla.

No requiere configuración por aplicación ni una extensión del navegador. La IA de seguimiento y gestos se ejecuta en el firmware de la cámara; el daemon la controla por HID y no envía telemetría ni realiza llamadas a la nube.

**Autor de este fork:** Mariano García Mattío (`magm`) — [magm3333@gmail.com](mailto:magm3333@gmail.com). Basado en el proyecto MIT [LarsArtmann/emeet-pixyd](https://github.com/LarsArtmann/emeet-pixyd).

### Para quién es

- Usuarios de Linux con una PIXY que quieren que el seguimiento y la privacidad se activen automáticamente.
- Usuarios de NixOS que prefieren una configuración declarativa.
- Usuarios de Waybar o Quickshell que quieren mostrar el estado de la cámara.
- Personas que buscan que el obturador físico bloquee la lente cuando no hay una llamada.

No está pensado para macOS/Windows, para cámaras EMEET que no sean PIXY ni para administrar cámaras de varios fabricantes. Las C960, C950, C970 y S600L son cámaras UVC fijas, controladas por el kernel; el daemon las reconoce, pero no les envía comandos del firmware PIXY.

### Dispositivos compatibles

| Dispositivo | ID USB | Compatibilidad |
| --- | --- | --- |
| EMEET PIXY | `328f:00c0` | Completa: seguimiento, privacidad, audio, PTZ, presets y batería |
| EMEET PIXY 2K | `328f:0118` | Completa: mismo protocolo que PIXY |
| C960 / C960 2K | `328f:003f` / `328f:2013` / `328f:007c` | Reconocida; cámara UVC fija |
| C950 / C970 | `328f:0073` / `328f:002d` | Reconocida; cámara UVC fija |
| S600L | `328f:00ef` | Reconocida; cámara UVC fija |
| Otros dispositivos EMEET | `328f:*` | Identificados como desconocidos; se informa el ID del producto |

Para probar una variante PIXY no incluida, se puede indicar su PID, separado por comas:

```bash
EMEET_PIXYD_EXTRA_PRODUCT_IDS=0x0119 emeet-pixyd
```

### Funciones

- Detección de llamadas mediante `/proc`, compatible con cualquier aplicación que abra la cámara.
- Seguimiento facial, modo privacidad y selección de audio automática.
- Interfaz web DataStar con vista MJPEG, controles PTZ, presets, recorridos, batería y notificaciones.
- Selector de idioma español/inglés, resolución y FPS.
- Esfera PTZ opcional semitransparente sobre el video: muestra el sector trasero no alcanzable, permite mover la cámara arrastrándola, sincroniza el marcador y dibuja un rastro que se desvanece; el botón del ojo la oculta.
- Envío RTSP H.264/TCP desde el panel de escritorio. Una única captura V4L2 alimenta tanto RTSP como el preview web. La URL se guarda en el directorio de estado con permisos `0600`; credenciales no aparecen en el estado ni en los errores.
- Interfaz móvil de pantalla completa con gestos táctiles y controles Track/Pausa/Privacidad.
- Integraciones con Waybar, Quickshell y métricas Prometheus.
- Modo de seguimiento automático, hotplug USB y módulo NixOS.

### Inicio rápido

#### NixOS (recomendado)

Agrega el input al `flake.nix`:

```nix
inputs.emeet-pixyd.url = "github:magm3333/emeet-pixyd";

# En nixosConfiguration:
inputs.emeet-pixyd.nixosModules.default
```

Luego configura el módulo:

```nix
hardware.emeet-pixy = {
  enable = true;
  auto = "full";           # off | full | tracking-only | privacy-only
  defaultAudio = "nc";     # nc | live | org
  debug = false;
};
```

El módulo instala el daemon, las reglas udev, el servicio systemd de usuario y sus dependencias.

#### Compilar desde el código fuente

```bash
git clone https://github.com/magm3333/emeet-pixyd.git
cd emeet-pixyd
nix build    # o go build -o emeet-pixyd . con Go >= 1.27.1
```

#### Docker (amd64, arm64 y Raspberry Pi 3 arm/v7)

La imagen multi-plataforma está publicada como [`magm3333/emeet-pixy-dual-cam:latest`](https://hub.docker.com/r/magm3333/emeet-pixy-dual-cam) para `linux/amd64`, `linux/arm64` y `linux/arm/v7` (Raspberry Pi 3 de 32 bits). Consulta [`docker/README.md`](docker/README.md) para permisos USB, Compose y compilación.

### Comandos

Sin argumentos, `emeet-pixyd` inicia el daemon. Los comandos siguientes se comunican con la instancia activa por socket Unix:

```text
emeet-pixy status                 # Estado completo
emeet-pixy track                  # Activar seguimiento facial
emeet-pixy idle                   # Dejar la cámara en espera
emeet-pixy privacy                # Bloquear físicamente la lente
emeet-pixy toggle-privacy         # Alternar privacidad
emeet-pixy center                 # Centrar pan/tilt y zoom
emeet-pixy audio [nc|live|org]    # Consultar o cambiar audio
emeet-pixy auto [mode]            # off, full, tracking-only, privacy-only
emeet-pixy pan <value>            # Pan −150° a 150°; rel+/-N para relativo
emeet-pixy tilt <value>           # Tilt −90° a 90°; rel+/-N para relativo
emeet-pixy zoom <value>           # Zoom 100 a 150; rel+/-N para relativo
emeet-pixy speed <axis> <value>  # Velocidad de motor (pan, tilt, zoom)
emeet-pixy tracking <variant>     # none, face, halfbody, fullbody
emeet-pixy battery                # Batería/carga, si el dispositivo responde
emeet-pixy preset save <name>
emeet-pixy preset load <name>
emeet-pixy preset push <name>     # Guardar en un slot físico
emeet-pixy preset pull            # Importar slots físicos como hw-N
emeet-pixy route record start
emeet-pixy route record stop
emeet-pixy route save <name>
emeet-pixy route play <name> [loop] [pingpong]
emeet-pixy route stop
emeet-pixy route list
emeet-pixy sync                   # Sincronizar estado con la cámara
emeet-pixy probe                  # Volver a detectar video e HID
emeet-pixy device                 # Mostrar dispositivo y modelo
emeet-pixy waybar                 # Emitir JSON para Waybar
```

### Modos automáticos

| Modo | Al comenzar la llamada | Al terminar |
| --- | --- | --- |
| `full` (predeterminado nativo) | Seguimiento, cancelación de ruido y cambio de fuente PipeWire | Privacidad |
| `tracking-only` | Seguimiento facial | Privacidad |
| `privacy-only` | Sin cambios | Privacidad |
| `off` | Control manual; no observa `/proc` | Sin cambios |

`auto=off` es apropiado para uso manual o headless: el modo elegido persiste hasta que se cambie. El daemon reaplica la intención de cámara después de reinicios, reconexiones y ciclos de energía. En Docker, `auto=off` es el valor predeterminado porque el namespace privado no ve las aplicaciones de videollamada del host.

### Interfaz web

El daemon sirve el panel en `http://127.0.0.1:8090` (Docker publica `18090`): preview en vivo, controles Track/Idle/Privacy, audio, PTZ, velocidad de motores, presets y recorridos. Los recorridos pueden repetirse en bucle o ping-pong; mientras se ejecutan, la cámara pasa a Idle y al finalizar restaura el modo previo.

La tarjeta Audio cambia Noise Cancel/Live/Original. El switch **Escuchar micrófono** activa un flujo Ogg/Opus desde la PIXY al navegador por la LAN; solo se abre mientras está encendido y no se guarda en disco. Usa una red confiable. Para una videollamada, selecciona `EMEET PIXY USB Audio` como micrófono en esa aplicación.

La resolución y los FPS se eligen desde la interfaz. Mientras RTSP transmite, el selector queda bloqueado; pausa la emisión para cambiar el modo. Una esfera de cobertura opcional aparece sobre el preview abajo a la derecha y sigue el pan/tilt actual. El panel RTSP comparte una única captura V4L2 entre la emisión y el video web. La URL de destino se restaura al reiniciar y queda protegida en el directorio de estado.

### Configuración

Toda la configuración usa variables de entorno; no hay flags CLI porque `os.Args` transporta comandos al socket:

| Variable | Predeterminado | Descripción |
| --- | --- | --- |
| `EMEET_PIXYD_STATE_DIR` | `/run/emeet-pixyd` | Socket y estado persistente |
| `EMEET_PIXYD_WEB_ADDR` | `127.0.0.1:8090` | Dirección del panel web |
| `EMEET_PIXYD_POLL_INTERVAL` | `2s` | Intervalo de detección de llamadas |
| `EMEET_PIXYD_DEBOUNCE_COUNT` | `3` | Lecturas consecutivas antes de cambiar estado |
| `EMEET_PIXYD_DEBUG` | `false` | Activar pprof en `/debug/pprof/` |
| `EMEET_PIXYD_AUTO` | `full` (nativo), `off` (Docker) | `off`, `full`, `tracking-only`, `privacy-only` |
| `EMEET_PIXYD_DEFAULT_AUDIO` | `nc` | `nc`, `live`, `org` |
| `EMEET_PIXYD_EXTRA_PRODUCT_IDS` | vacío | PID hex separados por comas para variantes PIXY |

Opciones NixOS: `hardware.emeet-pixy.enable`, `user`, `auto`, `defaultAudio` y `debug`.

### Dependencias y arquitectura

`v4l2-ctl` controla PTZ; FFmpeg ofrece preview MJPEG, publicación RTSP y monitor de micrófono ALSA→Ogg/Opus; `wpctl` permite cambiar la fuente PipeWire; `notify-send` es opcional. La arquitectura combina un socket Unix, servidor HTTP/API y `/metrics`, detección de procesos mediante `/proc`, eventos de hotplug netlink y notificaciones systemd `sd_notify`.

La cámara PIXY debe ser la única propietaria de sus controles V4L2. Para RTSP, un solo FFmpeg captura y deriva cuadros al preview web y al encoder H.264. El estado se guarda atómicamente en `{StateDir}/state.json`; la URL RTSP se guarda aparte en `{StateDir}/state.json.rtsp-url` con permisos `0600`.

### Solución de problemas

| Problema | Solución |
| --- | --- |
| PIXY no aparece | Revisa USB con `lsusb` y ejecuta `emeet-pixy probe`; comprueba que udev esté cargado. |
| Permiso denegado en `hidraw` | Recarga reglas udev para `328f:00c0` o `328f:0118`. |
| Falta `v4l2-ctl` | Instala `v4l-utils`. |
| No cambia el audio | Comprueba con `wpctl status` que PipeWire muestre la fuente PIXY. |
| Cámara ocupada | Cierra OBS u otra pestaña/proceso que tenga abierto `/dev/video*`. |
| EMEET reconocido pero sin controles | Es una cámara UVC fija, no una PIXY; no hay nada que corregir. |
| Dispositivo EMEET desconocido | Para una variante PIXY, configura `EMEET_PIXYD_EXTRA_PRODUCT_IDS`; si no, informa el PID en un issue. |
| Detección de llamadas lenta | Reduce `EMEET_PIXYD_DEBOUNCE_COUNT` o `EMEET_PIXYD_POLL_INTERVAL`. |

### Desarrollo

```bash
nix develop
nix build
templ generate
GOWORK=off go test -race -count=1 ./...
GOWORK=off golangci-lint run --timeout 2m ./...
```

`GOWORK=off` es necesario porque existe un `go.work` en el directorio padre. Consulta [CONTRIBUTING.md](CONTRIBUTING.md) para el flujo completo y [la documentación del proyecto](https://emeet-pixyd.lars.software).

### Licencia

MIT — consulta [LICENSE](LICENSE).
