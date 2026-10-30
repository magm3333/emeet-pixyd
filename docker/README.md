# Docker / Docker para EMEET PIXY

**Language / Idioma:** [English](#english) · [Español](#español)

## Español

Esta carpeta contiene el Dockerfile, Compose y la prueba de build/runtime para
`linux/amd64`, `linux/arm64` y `linux/arm/v7` (Raspberry Pi 3 de 32 bits).

**Contributor:** Mariano García Mattío (`magm`) — [magm3333@gmail.com](mailto:magm3333@gmail.com).

Docker Hub account: <https://hub.docker.com/r/magm3333>.

Published multi-platform image:
[`magm3333/emeet-pixy-dual-cam:latest`](https://hub.docker.com/r/magm3333/emeet-pixy-dual-cam)
(`linux/amd64`, `linux/arm64`, `linux/arm/v7`).

## Requisitos

- Docker Engine con Docker Compose v2.
- Linux `amd64`, `arm64` o Raspberry Pi OS de 32 bits (`arm/v7`).
- Una EMEET PIXY o PIXY 2K conectada al host. Se requieren nodos V4L2 y
  `hidraw`; para monitorizar el micrófono también se pasan los nodos ALSA de
  captura (`controlC*` y `pcmC*D0c`). La imagen **no** incluye ni simula el
  firmware/IA de la cámara.
- Permiso del usuario de Docker para acceder al daemon Docker.

Identifica los nodos y grupos del host:

```bash
v4l2-ctl --list-devices
arecord -l
ls -l /dev/video* /dev/hidraw* /dev/snd/*
stat -c '%g %n' /dev/video0 /dev/hidraw0 /dev/snd/controlC2
```

Edita `docker/.env` con los nodos reales y sus GID. El ejemplo usa los nodos
detectados en los hosts de prueba (`video0`, `hidraw4`, grupos 44/46), pero
**no son universales**. Si tu PIXY aparece en otro nodo o usas otra cuenta,
ajusta `.env` (`PIXY_UID`, `PIXY_GID`, `PIXY_VIDEO_GID`, `PIXY_HIDRAW_GID`).
Después de una reconexión USB con nodos distintos, recrea el contenedor.

Para el Raspberry Pi 4 probado en esta instalación, usa
`docker/.env.raspi4.example`: PIXY en `/dev/video0`, `/dev/video1` y
`/dev/hidraw0`; `video` es GID 44 y `plugdev` es GID 46. La regla
`docker/99-emeet-pixy.rules` asigna V4L2 a `video` y `hidraw` a `plugdev`, con
modo `0660`.

Para Raspberry Pi 3 de 32 bits, usa `docker/.env.raspi3.example`: PIXY en
`/dev/video0`, `/dev/hidraw0` y ALSA `card 2`; el perfil usa `linux/arm/v7`.
Instala `docker/99-emeet-pixy.rules` para que `hidraw0` pertenezca a `plugdev`
con permisos `0660` antes de iniciar Compose.

## Ejecutar desde el código fuente

Desde la raíz del repositorio:

```bash
cp docker/.env.example docker/.env
# Edita docker/.env: device nodes, UID/GID y grupo de cada nodo.
mkdir -p docker/data
chown "$(id -u):$(id -g)" docker/data
# Opcional: conserva rutas/presets del daemon nativo.
python3 docker/scripts/import-state.py \
  "$HOME/.local/state/emeet-pixyd/state.json" docker/data/state.json --auto off
docker compose --env-file docker/.env -f docker/compose.yaml build
docker compose --env-file docker/.env -f docker/compose.yaml up -d
docker compose --env-file docker/.env -f docker/compose.yaml ps
docker compose --env-file docker/.env -f docker/compose.yaml logs -f emeet-pixyd
```

En `raspi4`, usa el env específico y el `pull` de la imagen publicada:

```bash
cp docker/.env.raspi4.example docker/.env
mkdir -p docker/data
chown "$(id -u):$(id -g)" docker/data
docker compose --env-file docker/.env -f docker/compose.yaml pull
docker compose --env-file docker/.env -f docker/compose.yaml up -d --no-build
python3 docker/scripts/check-live-route.py --url http://127.0.0.1:18090
```

En `raspi3carcasa` (Raspberry Pi 3 de 32 bits), instala primero la regla udev
para que el contenedor pueda abrir HID, y usa el perfil `arm/v7`:

```bash
sudo install -m 0644 docker/99-emeet-pixy.rules /etc/udev/rules.d/99-emeet-pixy.rules
sudo udevadm control --reload-rules && sudo udevadm trigger
cp docker/.env.raspi3.example docker/.env
mkdir -p docker/data
chown "$(id -u):$(id -g)" docker/data
docker compose --env-file docker/.env -f docker/compose.yaml pull
docker compose --env-file docker/.env -f docker/compose.yaml up -d --no-build
```

El puerto está ligado a loopback en la Pi. Para abrir la UI desde otra máquina,
crea un túnel SSH local y navega por el mismo puerto:

```bash
ssh -L 18090:127.0.0.1:18090 pi@raspi4
```

La UI queda en `http://<IP-del-host>:18090/` (en `raspi4`:
`http://raspi4:18090/`). El puerto se publica en todas las interfaces IPv4 para
acceso LAN. La API no tiene autenticación: úsala solo en una red confiable y
filtra el puerto con el firewall si corresponde.
Compose guarda `state.json` y el socket en `docker/data/`, que está excluido de
Git. Si importas un estado anterior, el script conserva presets/recorridos y
otros valores; `--auto off` modifica solo la copia para adaptarla al namespace
de procesos aislado del contenedor. El `state.json` del daemon nativo no se
modifica.

Estado y comandos:

```bash
curl -fsS http://127.0.0.1:18090/api/health
curl -fsS http://127.0.0.1:18090/api/status
docker compose --env-file docker/.env -f docker/compose.yaml exec emeet-pixyd emeet-pixyd status
```

## Arquitectura y limitaciones de Docker

- El Dockerfile es multi-stage: Go y `templ` solo están en el builder; la
  imagen final contiene el binario estático, `ffmpeg` y `v4l2-ctl`.
- `INSTALL_DESKTOP_TOOLS=1` añade `wpctl` y `notify-send` al build; esos binarios
  no montan por sí mismos el PipeWire ni el namespace de procesos del host.
- El contenedor corre como UID 65532, sin capacidades Linux añadidas, con
  filesystem de solo lectura y solo los nodos de cámara configurados.
- El host publica `127.0.0.1:18090`; el daemon escucha en `0.0.0.0:8090` dentro
  de la red aislada del contenedor.
- `EMEET_PIXYD_AUTO=off` es el valor inicial intencional: el namespace `/proc`
  privado del contenedor no ve las aplicaciones de videollamada del host, y
  `wpctl` no puede cambiar el PipeWire del host sin compartir su socket de
  usuario. El control manual de cámara, PTZ, recorridos, audio por HID y la UI
  sí están disponibles. Para automatización de llamadas se requiere una
  integración específica con el namespace de procesos y la sesión PipeWire del
   host; habilitar solo `auto=full` dentro del Compose predeterminado no basta.
  - El switch **Escuchar micrófono** recibe un flujo Ogg/Opus desde la PIXY por
  HTTP en la LAN; no lo guarda en disco. El Compose pasa los nodos ALSA de la
  PIXY al contenedor. Usa una red confiable y apaga el switch al terminar.
- Docker debe ejecutarse en un host Linux. Docker Desktop/macOS no ofrece el
  acceso USB/V4L2/hidraw que necesita la cámara.

Para incluir los comandos opcionales de escritorio en la imagen:

```bash
INSTALL_DESKTOP_TOOLS=1 docker compose --env-file docker/.env -f docker/compose.yaml build
```

El modo `full` además necesita integrar el namespace de procesos y el socket
PipeWire del host; instalar esos comandos no concede ese acceso automáticamente.

## Emitir por RTSP a MediaMTX

La vista normal de escritorio incluye un panel **Publicación RTSP**. Pega allí
la URL del publisher, pulsa **Iniciar emisión** y usa **Pausar emisión** para
cortar el proceso FFmpeg sin cerrar el preview. Una sola captura V4L2 alimenta
el publisher H.264 por TCP y el preview JPEG simultáneamente. La URL configurada
se conserva en `docker/data/state.json.rtsp-url` con permisos `0600`; el campo
la restaura ocultando la contraseña. El estado y los errores muestran el destino
sin userinfo, y los logs no incluyen la URL completa.
El perfil de resolución/FPS seleccionado en la interfaz es la fuente del stream
RTSP. Para cambiarlo, pausa la publicación; el selector se habilita al terminar
la pausa.

Para MediaMTX en la misma máquina, el alias Compose `nv` se resuelve al gateway
del host (`RTSP_HOST_IP=host-gateway`). En `raspi4`, `.env.raspi4.example` apunta
`nv` al host neuralVISION `192.168.195.33`; cambia ese valor si MediaMTX se mueve.
El destino de prueba de neuralVISION es de la forma
`rtsp://publisher:<password>@nv:8554/org1/testpixy`.

## Builds y prueba de todas las plataformas

El script construye los tres targets. Para `amd64` carga y ejecuta el contenedor;
para `arm64` y `arm/v7` hace que BuildKit ejecute el smoke stage con QEMU. El
builder necesita binfmt/QEMU registrado para ambos targets ARM:

```bash
docker buildx inspect npu-builder --bootstrap
# Solo si el builder no anuncia linux/arm/v7:
docker run --privileged --rm tonistiigi/binfmt --install arm
bash docker/scripts/check-platforms.sh
```

Se puede seleccionar otro builder con `BUILDER=nombre`. La prueba verifica que
cada target ejecuta el daemon y las utilidades esenciales; también comprueba
las herramientas de escritorio si `INSTALL_DESKTOP_TOOLS=1`. QEMU verifica el
formato y ABI de las imágenes ARM, **no equivale a ejecutar en una Raspberry Pi
física ni valida allí el controlador USB**.
Antes de considerar validada una instalación Raspberry Pi, corre Compose en la
Pi con la PIXY conectada y ejecuta el checklist visual del README raíz.

## Publicar en Docker Hub

El push se hace como `magm3333`; no se guardan credenciales en el repositorio.
Después de ejecutar `docker login` en el host:

```bash
VERSION="$(git rev-parse --short HEAD)"
docker buildx build \
  --builder npu-builder \
  --platform linux/amd64,linux/arm64,linux/arm/v7 \
  --build-arg VERSION="$VERSION" \
  --tag magm3333/emeet-pixy-dual-cam:latest \
  --tag "magm3333/emeet-pixy-dual-cam:$VERSION" \
  --file docker/Dockerfile \
  --push .
docker buildx imagetools inspect magm3333/emeet-pixy-dual-cam:latest
```

El último comando debe listar manifiestos para `linux/amd64`, `linux/arm64` y
`linux/arm/v7`.
Para instalar esa publicación en una máquina, usa `docker compose pull` y luego
`docker compose up -d --no-build` con el mismo archivo de Compose y `.env`.

## Tests fuente y contenido de imagen

Los tests Go permanecen junto al paquete `main`, porque verifican símbolos
internos no exportados. `.dockerignore` los excluye junto con docs, website,
Nix y configuración local de agentes; la imagen runtime contiene únicamente
los binarios y dependencias necesarios para operar.

## Checklist de prueba visual

En `http://127.0.0.1:18090/`, comprueba lo siguiente con la cámara conectada:

1. Cambia la cámara a **Track**. Pulsa **Grabar recorrido**: la cámara debe
   pasar a **Idle**, las tarjetas de modo deben quedar bloqueadas y el control
   debe convertirse en **Detener grabación**.
2. Mueve pan/tilt/zoom con sliders o mouse y comprueba que el contador de pasos
   cambia. Pulsa **Detener grabación**: aparece el campo de nombre y se restaura
   **Track**; al guardar, el chip de la ruta aparece en la lista.
3. Reproduce esa ruta: mientras esté activa, la cámara permanece en Idle y se
   muestra **Detener**. Pulsa **Detener** y verifica que desaparece, vuelven los
   controles iniciales y Track se restaura. Repite con finalización natural,
   loop e ida y vuelta.
4. Repite iniciar/detener desde **Idle** y **Privacy**. Al terminar debe quedar
   exactamente el modo previo, no Track por defecto.
5. Inicia y descarta una grabación vacía: el estado debe regresar al control
   inicial sin dejar la cámara en otro modo.
6. Comprueba que la previsualización y los controles PTZ siguen actualizándose
   durante la ruta, y que no aparecen errores JavaScript en la consola.
7. Abre la UI desde el navegador del teléfono en la misma LAN: debe mostrar
   solo el video con el pad direccional y zoom. Arrastra sobre el video para
   pan/tilt, usa las flechas/zoom para ajustes pequeños y cambia entre
   Seguir/Pausa/Privacidad desde los botones superpuestos.
8. Activa **Esfera**: el switch debe ocultarse y aparecer el ojo arriba a la
   derecha del globo. Arrastra la esfera para mover la cámara; al grabar un
   recorrido, esos movimientos deben quedar registrados. El marcador y el rastro
   que se desvanece deben seguir la posición, y el ojo debe ocultar el globo.
9. Inicia RTSP y comprueba que el preview continúa. El selector de resolución
   debe bloquearse durante la emisión y volver a habilitarse al pausarla.
10. Activa **Escuchar micrófono** y permite el acceso del navegador: la entrada
    EMEET PIXY debe escucharse por la salida seleccionada. Apaga el switch y
    confirma que la monitorización se detiene.

El caso de llamadas automáticas requiere configurar además el acceso del
contenedor a los procesos y al PipeWire del host; el Compose predeterminado usa
modo manual por diseño.

---

## English

This directory contains the Dockerfile, Compose configuration, and build/runtime
checks for `linux/amd64`, `linux/arm64`, and `linux/arm/v7` (32-bit Raspberry Pi 3).

**Contributor:** Mariano García Mattío (`magm`) — [magm3333@gmail.com](mailto:magm3333@gmail.com).

Published multi-platform image:
[`magm3333/emeet-pixy-dual-cam:latest`](https://hub.docker.com/r/magm3333/emeet-pixy-dual-cam)
(`linux/amd64`, `linux/arm64`, `linux/arm/v7`).

### Requirements

- Docker Engine with Docker Compose v2.
- Linux `amd64`, `arm64`, or 32-bit Raspberry Pi OS (`arm/v7`).
- An EMEET PIXY or PIXY 2K connected to the host. The container needs V4L2
  video and `hidraw` nodes; the image does not include or emulate the camera's
  firmware/AI.
- Permission to access the Docker daemon.

Identify the host device nodes and groups:

```bash
v4l2-ctl --list-devices
arecord -l
ls -l /dev/video* /dev/hidraw* /dev/snd/*
stat -c '%g %n' /dev/video0 /dev/hidraw0 /dev/snd/controlC2
```

Copy `docker/.env.example` to `docker/.env` and set the actual device nodes,
UID, and GIDs. Its `video0`/`hidraw4` and groups 44/46 are examples from the
test hosts, not universal values. After USB reconnection changes a node, recreate
the container.

For the Raspberry Pi 4 used in this setup, start from
`docker/.env.raspi4.example`: the PIXY is `/dev/video0`, `/dev/video1`, and
`/dev/hidraw0`; `video` has GID 44 and `plugdev` has GID 46. The
`docker/99-emeet-pixy.rules` udev rule assigns V4L2 to `video` and `hidraw` to
`plugdev`, mode `0660`.

For a 32-bit Raspberry Pi 3, start from `docker/.env.raspi3.example`: the PIXY
uses `/dev/video0`, `/dev/hidraw0`, and ALSA `card 2`. This profile targets
`linux/arm/v7`. Install `docker/99-emeet-pixy.rules` so `hidraw0` belongs to
`plugdev` with mode `0660` before starting Compose.

### Run from source

From the repository root:

```bash
cp docker/.env.example docker/.env
# Edit docker/.env: device nodes, UID/GID, and node groups.
mkdir -p docker/data
chown "$(id -u):$(id -g)" docker/data
# Optional: preserve presets/routes from the native daemon.
python3 docker/scripts/import-state.py \
  "$HOME/.local/state/emeet-pixyd/state.json" docker/data/state.json --auto off
docker compose --env-file docker/.env -f docker/compose.yaml build
docker compose --env-file docker/.env -f docker/compose.yaml up -d
docker compose --env-file docker/.env -f docker/compose.yaml ps
docker compose --env-file docker/.env -f docker/compose.yaml logs -f emeet-pixyd
```

On `raspi4`, use the Pi-specific environment and pull the published image:

```bash
cp docker/.env.raspi4.example docker/.env
mkdir -p docker/data
chown "$(id -u):$(id -g)" docker/data
docker compose --env-file docker/.env -f docker/compose.yaml pull
docker compose --env-file docker/.env -f docker/compose.yaml up -d --no-build
python3 docker/scripts/check-live-route.py --url http://127.0.0.1:18090
```

On `raspi3carcasa` (32-bit Raspberry Pi 3), install the udev rule and use the
arm/v7 profile:

```bash
sudo install -m 0644 docker/99-emeet-pixy.rules /etc/udev/rules.d/99-emeet-pixy.rules
sudo udevadm control --reload-rules && sudo udevadm trigger
cp docker/.env.raspi3.example docker/.env
mkdir -p docker/data
chown "$(id -u):$(id -g)" docker/data
docker compose --env-file docker/.env -f docker/compose.yaml pull
docker compose --env-file docker/.env -f docker/compose.yaml up -d --no-build
```

The port is bound to loopback on the Pi. To open the UI from another machine,
create an SSH tunnel and browse to the forwarded port:

```bash
ssh -L 18090:127.0.0.1:18090 pi@raspi4
```

The example Compose file publishes the UI on all IPv4 host interfaces at
`http://<host-IP>:18090/` (on the Pi: `http://raspi4:18090/`). The API has no
authentication; use it only on a trusted network and restrict it with the host
firewall. Do not forward it to the public internet.

Compose stores `state.json`, the RTSP destination, and the Unix socket under
`docker/data/`, which is excluded from Git. The RTSP URL is stored separately in
`state.json.rtsp-url` with permissions `0600`; the UI restores it in the masked
field. If importing old state, the script preserves presets/routes and other
values; `--auto off` changes only the container copy because the container's
process namespace cannot observe host calls. The native daemon's `state.json` is
not modified.

Check status and send commands:

```bash
curl -fsS http://127.0.0.1:18090/api/health
curl -fsS http://127.0.0.1:18090/api/status
docker compose --env-file docker/.env -f docker/compose.yaml exec emeet-pixyd emeet-pixyd status
```

### Docker architecture and limitations

- The multi-stage Dockerfile uses Go and `templ` only in the builder. The final
  image contains the static daemon binary, FFmpeg, and `v4l2-ctl`.
- `INSTALL_DESKTOP_TOOLS=1` adds `wpctl` and `notify-send`; installing them does
  not provide access to the host's PipeWire session or process namespace.
- The container runs as UID 65532 without added Linux capabilities, with a
  read-only filesystem and only the configured camera nodes passed through.
- The host publishes `127.0.0.1:18090` in the default config; the daemon listens
  on `0.0.0.0:8090` inside the isolated container network.
- `EMEET_PIXYD_AUTO=off` is the intentional default. The container's private
  `/proc` namespace cannot see conferencing apps on the host, and `wpctl` cannot
  change host PipeWire without sharing its user socket. Manual camera/PTZ/routes,
  HID audio controls, and the web UI are available. Enabling `auto=full` alone
   does not provide host integration.
- **Monitor microphone** streams PIXY audio to the browser as Ogg/Opus over
  local HTTP while enabled; it is not stored on disk. Compose passes the PIXY's
  ALSA capture/control nodes into the container. Use a trusted LAN and switch
  monitoring off when finished.
- Docker must run on a Linux host. Docker Desktop/macOS does not expose the USB,
  V4L2, and `hidraw` access required by the camera.

To include the optional desktop commands in the image:

```bash
INSTALL_DESKTOP_TOOLS=1 docker compose --env-file docker/.env -f docker/compose.yaml build
```

Full automation also requires integrating the host process namespace and
PipeWire socket; installing the optional commands does not grant this access.

### RTSP publishing to MediaMTX

The desktop web UI includes a **RTSP Publishing** panel. Paste the publish URL,
select **Start publishing**, and use **Pause publishing** to stop FFmpeg without
closing the preview. One V4L2 capture feeds both the RTSP H.264/TCP publisher and
the JPEG web preview, so they can run simultaneously. The selected capture
resolution/FPS is the source profile used by RTSP. Resolution changes are
available after pausing the publisher.

The destination URL is saved under `docker/data/state.json.rtsp-url` with
permissions `0600` and restored in the masked field after restart. Status and
errors show only the sanitized destination, never the URL userinfo; logs do not
include the complete URL.

For MediaMTX on the same host, Compose maps `nv` to the host gateway
(`RTSP_HOST_IP=host-gateway`). On `raspi4`, `.env.raspi4.example` maps `nv` to the
neuralVISION host `192.168.195.33`; change it if MediaMTX moves. A test target has
this form: `rtsp://publisher:<password>@nv:8554/org1/testpixy`.

### Build and test all platforms

The script builds all three platforms. It loads and executes the `amd64` image;
BuildKit/QEMU runs the `arm64` and `arm/v7` smoke stages. The builder needs
binfmt/QEMU registered for both ARM targets:

```bash
docker buildx inspect npu-builder --bootstrap
# Only if the builder does not list linux/arm/v7:
docker run --privileged --rm tonistiigi/binfmt --install arm
bash docker/scripts/check-platforms.sh
```

Select a different builder with `BUILDER=name`. The smoke test executes the
daemon and required utilities for each target and checks optional desktop tools
when `INSTALL_DESKTOP_TOOLS=1`. QEMU checks image format and ABI; it is not a
substitute for testing USB access on a physical Raspberry Pi. Before validating
a Pi installation, run Compose on the Pi with a connected PIXY and follow the
visual checklist in the root README.

### Publish to Docker Hub

The image is published as `magm3333`; repository credentials are never stored
in source control. Log in to Docker Hub on the build host, then run this from the
repository root after committing the release source:

```bash
VERSION="$(git rev-parse --short HEAD)"
docker buildx build \
  --builder npu-builder \
  --platform linux/amd64,linux/arm64,linux/arm/v7 \
  --build-arg VERSION="$VERSION" \
  --tag magm3333/emeet-pixy-dual-cam:latest \
  --tag "magm3333/emeet-pixy-dual-cam:$VERSION" \
  --file docker/Dockerfile \
  --push .
docker buildx imagetools inspect magm3333/emeet-pixy-dual-cam:latest
```

The inspection should list manifests for `linux/amd64`, `linux/arm64`, and
`linux/arm/v7`. On an installation host, run `docker compose pull` followed by
`docker compose up -d --no-build` using the same Compose file and `.env`.

### Source tests and image contents

Go tests remain alongside the package because they verify unexported internals.
`.dockerignore` excludes tests, documentation, the website, Nix files, and local
agent configuration; the runtime image contains only the binaries and
dependencies required to run the daemon.

### Visual test checklist

At `http://127.0.0.1:18090/`, with the camera connected, verify:

1. Switch the camera to **Track** and start route recording. The camera should
   move to **Idle**, mode cards should lock, and the control should become
   **Stop recording**.
2. Move pan/tilt/zoom using sliders or the mouse; confirm the step count changes.
   Stop recording, confirm **Track** is restored, save the route, and check that
   its chip appears.
3. Play the route; confirm Idle remains active and **Stop** is available. Stop
   and verify controls return and Track is restored. Repeat with natural
   completion, loop, and ping-pong.
4. Repeat recording/playback from **Idle** and **Privacy**; the exact prior mode
   should be restored.
5. Start and discard an empty recording; camera mode should return to its
   original state.
6. Confirm preview and PTZ remain responsive during route playback and check the
   browser console for JavaScript errors.
7. Open the UI from a phone on the same LAN. Confirm the video-only layout,
   directional/zoom controls, drag-to-pan/tilt, and overlaid Track/Idle/Privacy
   buttons.
8. Enable **Sphere**. The switch should hide and an eye button should appear at
   the globe's upper-right. Drag the sphere to move the camera; route recording
   should capture those moves. Verify the marker and fading trail follow camera
   position, and that the eye hides the sphere.
9. Start RTSP publishing. Confirm the preview continues, the resolution selector
   is locked while publishing, and it becomes available after pausing.
10. Enable **Monitor microphone**, allow browser mic access, and verify the PIXY
    input is audible locally; switch it off and confirm monitoring stops.

Automatic call handling additionally requires host process and PipeWire access;
the default Compose setup uses manual mode by design.
