# EMEET PIXY Dual Cam

**English / Español:** [English](#english) · [Español](#español)

## English

Multi-platform Docker image for the EMEET PIXY and PIXY 2K Linux daemon.

Published image: [`magm3333/emeet-pixy-dual-cam:latest`](https://hub.docker.com/r/magm3333/emeet-pixy-dual-cam).

**Source code:** [github.com/magm3333/emeet-pixyd](https://github.com/magm3333/emeet-pixyd)

### Platforms

- `linux/amd64` — Intel/AMD 64-bit Linux
- `linux/arm64` — 64-bit ARM, including Raspberry Pi OS 64-bit

### Features

- PTZ, audio mode, gesture, tracking, and privacy controls over the camera's HID interface.
- Optional browser-local PIXY microphone monitoring, with an explicit on/off switch; audio is not recorded or uploaded.
- Web UI with live preview, pan/tilt/zoom, resolution/FPS selection, and persistent Spanish/English language choice.
- Optional semi-transparent interactive PTZ sphere over the lower-right of the video: drag to move, record its PTZ moves in routes, follow playback with a live marker and fading trail, and hide it with the eye button.
- Phone-first full-screen preview with touch PTZ controls and over-video Track/Idle/Privacy buttons.
- Desktop RTSP publishing panel with a masked destination, H.264/TCP output, start/pause controls, and sanitized errors.
- One V4L2 capture feeds RTSP and the web preview simultaneously. The destination URL is saved locally under the mounted state directory with permissions `0600`.
- Record, save, play, loop, ping-pong, and delete PTZ routes. During recording/playback, tracking is suspended when needed and the previous camera mode is restored.
- Persistent daemon state and saved routes in the mounted data directory.

The camera performs its own person/gesture AI. This daemon controls the camera; it does not ship an AI model or perform vision inference on the host.

### Quick start

Run on a Linux host and pass through the PIXY's `/dev/video*` and `/dev/hidraw*` nodes. Compose configuration, device permissions, and Raspberry Pi instructions are in the [Docker guide](https://github.com/magm3333/emeet-pixyd/blob/master/docker/README.md).

The example Compose file publishes an unauthenticated control UI at port `18090` on host IPv4 interfaces. Restrict the port to a trusted LAN with the host firewall; do not expose it to the public internet. Container auto-call detection defaults to `off` because its private PID namespace cannot see host conferencing processes. Host process and PipeWire access require separate integration.

## Español

Imagen Docker multi-plataforma para el daemon Linux de EMEET PIXY y PIXY 2K.

Imagen publicada: [`magm3333/emeet-pixy-dual-cam:latest`](https://hub.docker.com/r/magm3333/emeet-pixy-dual-cam).

**Código fuente:** [github.com/magm3333/emeet-pixyd](https://github.com/magm3333/emeet-pixyd)

### Plataformas

- `linux/amd64` — Linux de 64 bits para Intel/AMD
- `linux/arm64` — ARM de 64 bits, incluida Raspberry Pi OS de 64 bits

### Funciones

- Controles PTZ, audio, gestos, seguimiento y privacidad mediante la interfaz HID de la cámara.
- Monitorización local y opcional del micrófono desde el navegador, con switch para encender/apagar; el audio no se graba ni se sube.
- Interfaz web con preview en vivo, pan/tilt/zoom, selección de resolución/FPS e idioma español/inglés persistente.
- Esfera PTZ interactiva y opcional sobre la esquina inferior derecha: arrástrala para mover la cámara y grabar recorridos; sigue la reproducción con marcador y rastro que se desvanece, y se oculta con el botón del ojo.
- Vista móvil de pantalla completa con controles táctiles PTZ y botones Track/Pausa/Privacidad sobre el video.
- Panel RTSP de escritorio con destino enmascarado, salida H.264/TCP, iniciar/pausar y errores sanitizados.
- Una sola captura V4L2 alimenta simultáneamente RTSP y el preview web. La URL de destino se guarda en el directorio montado con permisos `0600`.
- Grabar, guardar, reproducir, repetir en bucle o ping-pong y borrar recorridos PTZ. Durante la grabación/reproducción se suspende el seguimiento si hace falta y se restaura el modo de cámara anterior.
- Estado del daemon y recorridos guardados persistentes en el directorio de datos montado.

La cámara ejecuta su propia IA de personas y gestos. Este daemon la controla; no incluye modelos de IA ni procesa video en el host.

### Inicio rápido

Ejecuta en un host Linux y pasa al contenedor los nodos `/dev/video*` y
`/dev/hidraw*` de la PIXY. La guía de Compose, permisos de dispositivos y
Raspberry Pi está en [Docker README](https://github.com/magm3333/emeet-pixyd/blob/master/docker/README.md).

El Compose de ejemplo publica una interfaz de control sin autenticación en el
puerto `18090` de las interfaces IPv4 del host. Limítalo a una LAN confiable con
el firewall; no lo expongas a internet. La detección automática de llamadas del
contenedor está desactivada (`off`) porque su namespace PID privado no ve las
aplicaciones del host. El acceso a procesos y PipeWire del host requiere una
integración adicional.
