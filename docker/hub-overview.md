# EMEET PIXY Dual Cam

Multi-platform Docker image for the EMEET PIXY and PIXY 2K Linux daemon.

Published image: [`magm3333/emeet-pixy-dual-cam:latest`](https://hub.docker.com/r/magm3333/emeet-pixy-dual-cam).

**Source code for this fork:** [github.com/magm3333/emeet-pixyd](https://github.com/magm3333/emeet-pixyd)

## Platforms

- `linux/amd64` — Intel/AMD 64-bit Linux
- `linux/arm64` — 64-bit ARM, including Raspberry Pi OS 64-bit

## Features

- Camera PTZ, audio-mode, gesture, and privacy controls through the camera's HID interface.
- Web UI with live preview, pan/tilt/zoom controls, and persistent Spanish/English language selection.
- Phone-first full-screen preview with touch pan/tilt dragging, directional controls, and zoom buttons.
- Record, save, play, loop, ping-pong, and delete PTZ routes.
- During route recording/playback, camera AI tracking is paused if active and the previous Track/Idle/Privacy state is restored at the end.
- State and saved routes persist in the mounted data directory/volume.

The camera itself performs person/gesture AI. The daemon sends camera mode
commands; it does not ship an AI model or perform vision inference on the host.

## Quick start

Run on the Linux host with the PIXY's `/dev/video*` and `/dev/hidraw*` nodes
passed through. Compose configuration, permissions, and Raspberry Pi details
are documented in the source repository's [`docker/README.md`](https://github.com/magm3333/emeet-pixyd/blob/master/docker/README.md).

The example Compose publishes the unauthenticated control UI on all IPv4 host
interfaces at port `18090` for LAN access. Restrict that port to a trusted LAN
with the host firewall; do not forward it to the public internet.

Container auto-call detection defaults to `off` because a private PID namespace
cannot see conferencing processes on the host. Host process and PipeWire access
must be configured separately to enable full desktop automation.
