# Docker para EMEET PIXY

Esta carpeta contiene el Dockerfile, Compose y la prueba de build/runtime para
`linux/amd64` y `linux/arm64` (Raspberry Pi con SO de 64 bits).

**Contributor:** Mariano García Mattío (`magm`) — [magm3333@gmail.com](mailto:magm3333@gmail.com).

Docker Hub account: <https://hub.docker.com/r/magm3333>.

Published multi-platform image:
[`magm3333/emeet-pixy-dual-cam:latest`](https://hub.docker.com/r/magm3333/emeet-pixy-dual-cam)
(`linux/amd64`, `linux/arm64`).

## Requisitos

- Docker Engine con Docker Compose v2.
- Linux `amd64` o `arm64` de 64 bits.
- Una EMEET PIXY o PIXY 2K conectada al host. Se requieren un nodo V4L2 y uno
  `hidraw`; la imagen **no** incluye ni simula el firmware/IA de la cámara.
- Permiso del usuario de Docker para acceder al daemon Docker.

Identifica los nodos y grupos del host:

```bash
v4l2-ctl --list-devices
ls -l /dev/video* /dev/hidraw*
stat -c '%g %n' /dev/video0 /dev/hidraw4
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
- Docker debe ejecutarse en un host Linux. Docker Desktop/macOS no ofrece el
  acceso USB/V4L2/hidraw que necesita la cámara.

Para incluir los comandos opcionales de escritorio en la imagen:

```bash
INSTALL_DESKTOP_TOOLS=1 docker compose --env-file docker/.env -f docker/compose.yaml build
```

El modo `full` además necesita integrar el namespace de procesos y el socket
PipeWire del host; instalar esos comandos no concede ese acceso automáticamente.

## Builds y prueba de ambas arquitecturas

El script construye ambos targets. Para `amd64` carga y ejecuta el contenedor;
para `arm64` hace que BuildKit ejecute el smoke stage con QEMU. En este entorno,
`npu-builder` tiene `linux/arm64` habilitado con emulación:

```bash
docker buildx inspect npu-builder --bootstrap
bash docker/scripts/check-platforms.sh
```

Se puede seleccionar otro builder con `BUILDER=nombre`. La prueba verifica que
cada target ejecuta el daemon y las utilidades esenciales; también comprueba
las herramientas de escritorio si `INSTALL_DESKTOP_TOOLS=1`. La ejecución
`arm64` bajo QEMU verifica el formato y ABI de la imagen, **no equivale
a ejecutar en una Raspberry Pi física ni valida allí el controlador USB**.
Antes de considerar validada una instalación Raspberry Pi, corre Compose en la
Pi con la PIXY conectada y ejecuta el checklist visual del README raíz.

## Publicar en Docker Hub

El push se hace como `magm3333`; no se guardan credenciales en el repositorio.
Después de ejecutar `docker login` en el host:

```bash
VERSION="$(git rev-parse --short HEAD)"
docker buildx build \
  --builder npu-builder \
  --platform linux/amd64,linux/arm64 \
  --build-arg VERSION="$VERSION" \
  --tag magm3333/emeet-pixy-dual-cam:latest \
  --tag "magm3333/emeet-pixy-dual-cam:$VERSION" \
  --file docker/Dockerfile \
  --push .
docker buildx imagetools inspect magm3333/emeet-pixy-dual-cam:latest
```

El último comando debe listar manifiestos para `linux/amd64` y `linux/arm64`.
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
   pan/tilt y usa las flechas/zoom para ajustes táctiles pequeños.

El caso de llamadas automáticas requiere configurar además el acceso del
contenedor a los procesos y al PipeWire del host; el Compose predeterminado usa
modo manual por diseño.
