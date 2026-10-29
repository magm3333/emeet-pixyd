(function () {
  "use strict";

  var TOAST_DISPLAY_MS = 2500;
  var TOAST_FADE_MS = 300;
  var STREAM_RETRY_INITIAL_MS = 3000;
  var STREAM_RETRY_MAX_MS = 30000;

  /* -------------------------------------------------------------------- */
  /*  Toast — invoked by server ExecuteScript and local callers           */
  /* -------------------------------------------------------------------- */

  window.__showToast = showToast;

  function showToast(msg, type) {
    type = type || "success";
    var container = document.getElementById("toast-container");
    if (!container) return;
    var toast = document.createElement("div");
    toast.className = "toast toast-" + type + " show";
    toast.textContent = msg;
    container.appendChild(toast);
    setTimeout(function () {
      toast.classList.remove("show");
      setTimeout(function () {
        toast.remove();
      }, TOAST_FADE_MS);
    }, TOAST_DISPLAY_MS);
  }

  /* -------------------------------------------------------------------- */
  /*  SSE connection state indicator + offline banner                     */
  /*  DataStar dispatches 'datastar-fetch' custom events on document.     */
  /*  We listen to all events (no element filter) because:                */
  /*  - datastar-patch-elements = data flowing (green)                    */
  /*  - started = a fetch is in flight (yellow)                           */
  /*  - retries-failed = server unreachable (red + banner)               */
  /*  Button clicks also trigger these events, which is fine — they       */
  /*  prove the server is reachable.                                      */
  /* -------------------------------------------------------------------- */

  (function () {
    var indicator = document.getElementById("sse-indicator");
    var banner = document.getElementById("offline-banner");
    if (!indicator && !banner) return;

    function setSSEState(state) {
      if (indicator) {
        indicator.className = "sse-indicator " + state;
        indicator.setAttribute(
          "aria-label",
          state === "connected"
            ? "Live updates connected"
            : state === "disconnected"
              ? "Live updates disconnected"
              : "Live updates connecting",
        );
      }
      if (banner) banner.style.display = state === "disconnected" ? "flex" : "none";
    }

    window.setSSEState = setSSEState;

    document.addEventListener("datastar-fetch", function (e) {
      var type = e.detail && e.detail.type;
      if (!type) return;

      if (type === "datastar-patch-elements" || type === "datastar-patch-signals") {
        setSSEState("connected");
      } else if (type === "retrying" || type === "retries-failed") {
        setSSEState("disconnected");
      } else if (type === "started") {
        setSSEState("");
      }
    });
  })();

  /* -------------------------------------------------------------------- */
  /*  Mouse PTZ controls on the live preview                               */
  /* -------------------------------------------------------------------- */

  (function () {
    var img = document.getElementById("preview-img");
    if (!img) return;

    var PIXELS_PER_DEGREE = 4;
    var ZOOM_STEP = 10;
    var drag = null;

    function sliderValue(axis, fallback) {
      var slider = document.getElementById("slider-" + axis);
      return slider ? Number(slider.value) : fallback;
    }

    function setSlider(axis, value) {
      var slider = document.getElementById("slider-" + axis);
      if (!slider) return;
      var min = Number(slider.min);
      var max = Number(slider.max);
      slider.value = String(Math.max(min, Math.min(max, value)));
      slider.dispatchEvent(new Event("input", { bubbles: true }));
    }

    // The mobile overlay shares the same DataStar-bound sliders as desktop.
    // A semantic button click works for touch, mouse, Enter, and Space.
    document.querySelectorAll("[data-mobile-ptz-axis]").forEach(function (button) {
      button.addEventListener("click", function (event) {
        event.preventDefault();
        var axis = button.getAttribute("data-mobile-ptz-axis");
        var step = Number(button.getAttribute("data-mobile-ptz-step"));
        var fallback = axis === "zoom" ? 100 : 0;
        setSlider(axis, sliderValue(axis, fallback) + step);
      });
    });

    img.addEventListener("pointerdown", function (event) {
      if (event.target.closest(".mobile-ptz-controls")) return;
      if (event.button !== 0) return;
      event.preventDefault();
      drag = {
        pointerId: event.pointerId,
        x: event.clientX,
        y: event.clientY,
        pan: sliderValue("pan", 0),
        tilt: sliderValue("tilt", 0),
      };
      img.setPointerCapture(event.pointerId);
    });

    img.addEventListener("pointermove", function (event) {
      if (!drag || event.pointerId !== drag.pointerId) return;
      var dx = Math.round((event.clientX - drag.x) / PIXELS_PER_DEGREE);
      var dy = Math.round((event.clientY - drag.y) / PIXELS_PER_DEGREE);
      setSlider("pan", drag.pan + dx);
      setSlider("tilt", drag.tilt - dy);
    });

    function endDrag(event) {
      if (!drag || event.pointerId !== drag.pointerId) return;
      if (img.hasPointerCapture(event.pointerId)) img.releasePointerCapture(event.pointerId);
      drag = null;
    }

    img.addEventListener("pointerup", endDrag);
    img.addEventListener("pointercancel", endDrag);
    img.addEventListener("lostpointercapture", function () {
      drag = null;
    });

    img.addEventListener("dblclick", function (event) {
      event.preventDefault();
      setSlider("zoom", sliderValue("zoom", 100) + ZOOM_STEP);
    });

    img.addEventListener("contextmenu", function (event) {
      event.preventDefault();
      setSlider("zoom", sliderValue("zoom", 100) - ZOOM_STEP);
    });
  })();

  /* -------------------------------------------------------------------- */
  /*  Optional PTZ coverage globe overlay                                  */
  /* -------------------------------------------------------------------- */

  (function () {
    var toggle = document.getElementById("coverage-toggle");
    var eye = document.getElementById("coverage-eye");
    var panel = document.getElementById("coverage-visual");
    var canvas = document.getElementById("coverage-canvas");
    var readout = document.getElementById("coverage-readout");
    if (!toggle || !eye || !panel || !canvas || !readout) return;

    var context = canvas.getContext("2d");
    if (!context) return;
    var trail = [];
    var lastTrailPosition = null;
    var syncTimer = null;
    var sphereDrag = null;
    var trailLifetime = 2500;
    var maxTrailPoints = 64;

    function sliderValue(axis, fallback) {
      var slider = document.getElementById("slider-" + axis);
      return slider ? Number(slider.value) : fallback;
    }

    function localized(key, fallback) {
      return window.__i18n ? window.__i18n(key) : fallback;
    }

    function pointOnSphere(pan, tilt, cx, cy, radius) {
      var panRad = pan * Math.PI / 180;
      var tiltRad = tilt * Math.PI / 180;
      return {
        x: cx + radius * Math.sin(panRad) * Math.cos(tiltRad),
        y: cy - radius * Math.sin(tiltRad),
      };
    }

    function setSlider(axis, value) {
      var slider = document.getElementById("slider-" + axis);
      if (!slider) return;
      var min = Number(slider.min);
      var max = Number(slider.max);
      slider.value = String(Math.max(min, Math.min(max, value)));
      slider.dispatchEvent(new Event("input", { bubbles: true }));
    }

    function drawGlobe() {
      var cssSize = 180;
      var dpr = Math.min(window.devicePixelRatio || 1, 3);
      var bitmapSize = Math.round(cssSize * dpr);
      if (canvas.width !== bitmapSize || canvas.height !== bitmapSize) {
        canvas.width = bitmapSize;
        canvas.height = bitmapSize;
      }
      canvas.style.width = "100%";
      canvas.style.height = "auto";
      context.setTransform(dpr, 0, 0, dpr, 0, 0);
      context.clearRect(0, 0, cssSize, cssSize);

      var ctx = context;
      var cx = 90;
      var cy = 87;
      var radius = 68;
      var pan = Math.max(-150, Math.min(150, sliderValue("pan", 0)));
      var tilt = Math.max(-90, Math.min(90, sliderValue("tilt", 0)));
      var panRad = pan * Math.PI / 180;
      var tiltRad = tilt * Math.PI / 180;
      var depth = Math.cos(panRad) * Math.cos(tiltRad);
      var now = performance.now();
      if (!lastTrailPosition || pan !== lastTrailPosition.pan || tilt !== lastTrailPosition.tilt) {
        if (trail.length === 0 && lastTrailPosition) {
          trail.push({ pan: lastTrailPosition.pan, tilt: lastTrailPosition.tilt, time: now - 1 });
        }
        trail.push({ pan: pan, tilt: tilt, time: now });
        lastTrailPosition = { pan: pan, tilt: tilt };
      }
      while (trail.length && now - trail[0].time > trailLifetime) trail.shift();
      if (trail.length > maxTrailPoints) trail.splice(0, trail.length - maxTrailPoints);

      // Soft atmospheric halo and the sphere's polished glass surface.
      ctx.save();
      ctx.shadowColor = "rgba(69, 219, 221, 0.28)";
      ctx.shadowBlur = 18;
      var sphere = ctx.createRadialGradient(cx - 22, cy - 29, 5, cx, cy, radius + 2);
      sphere.addColorStop(0, "rgba(34, 99, 119, 0.9)");
      sphere.addColorStop(0.48, "rgba(14, 48, 68, 0.92)");
      sphere.addColorStop(0.82, "rgba(8, 25, 42, 0.96)");
      sphere.addColorStop(1, "rgba(5, 14, 27, 0.98)");
      ctx.beginPath();
      ctx.arc(cx, cy, radius, 0, Math.PI * 2);
      ctx.fillStyle = sphere;
      ctx.fill();
      ctx.restore();

      ctx.save();
      ctx.beginPath();
      ctx.arc(cx, cy, radius - 0.5, 0, Math.PI * 2);
      ctx.clip();

      // A cool translucent wash represents the reachable direction envelope.
      var coverage = ctx.createLinearGradient(cx - radius, cy, cx + radius, cy);
      coverage.addColorStop(0, "rgba(43, 219, 199, 0.15)");
      coverage.addColorStop(0.5, "rgba(75, 225, 218, 0.07)");
      coverage.addColorStop(1, "rgba(43, 219, 199, 0.16)");
      ctx.fillStyle = coverage;
      ctx.fillRect(cx - radius, cy - radius, radius * 2, radius * 2);

      // Meridian and latitude curves give the overlay a true globe reading.
      ctx.lineWidth = 0.72;
      ctx.strokeStyle = "rgba(165, 231, 238, 0.28)";
      [-60, -30, 0, 30, 60].forEach(function (latitude) {
        var y = cy - radius * Math.sin(latitude * Math.PI / 180);
        var rx = radius * Math.cos(latitude * Math.PI / 180);
        var ry = Math.max(3, radius * 0.16 * Math.cos(latitude * Math.PI / 180));
        ctx.beginPath();
        ctx.ellipse(cx, y, rx, ry, 0, 0, Math.PI * 2);
        ctx.stroke();
      });
      [-75, -45, 0, 45, 75].forEach(function (longitude) {
        var radians = longitude * Math.PI / 180;
        ctx.beginPath();
        ctx.ellipse(cx + radius * Math.sin(radians), cy, radius * Math.abs(Math.cos(radians)), radius, 0, 0, Math.PI * 2);
        ctx.stroke();
      });

      // The 60° amber wedge marks the rear azimuth sector the pan motor cannot reach.
      var gapStart = -Math.PI / 3;
      var gapEnd = 0;
      ctx.beginPath();
      ctx.moveTo(cx, cy);
      ctx.arc(cx, cy, radius, gapStart, gapEnd);
      ctx.closePath();
      ctx.fillStyle = "rgba(250, 176, 91, 0.23)";
      ctx.fill();
      ctx.save();
      ctx.font = "600 7px system-ui, sans-serif";
      ctx.textAlign = "center";
      ctx.textBaseline = "middle";
      ctx.fillStyle = "rgba(255, 220, 172, 0.94)";
      ctx.shadowColor = "rgba(14, 12, 8, 0.75)";
      ctx.shadowBlur = 4;
      ctx.fillText("60°", cx + radius * 0.55, cy - radius * 0.32);
      ctx.restore();
      ctx.beginPath();
      ctx.arc(cx, cy, radius, gapStart, gapEnd);
      ctx.strokeStyle = "rgba(255, 199, 123, 0.72)";
      ctx.lineWidth = 1.15;
      ctx.stroke();

      // Rim lighting and a narrow shadow create a dimensional, glass-like edge.
      var shade = ctx.createLinearGradient(cx - radius, cy - radius, cx + radius, cy + radius);
      shade.addColorStop(0, "rgba(222, 255, 255, 0.15)");
      shade.addColorStop(0.35, "rgba(121, 222, 232, 0.015)");
      shade.addColorStop(1, "rgba(1, 7, 17, 0.47)");
      ctx.fillStyle = shade;
      ctx.fillRect(cx - radius, cy - radius, radius * 2, radius * 2);

      // Recent PTZ samples form a short, time-fading trail along the globe.
      if (trail.length > 1) {
        ctx.save();
        ctx.lineCap = "round";
        ctx.lineJoin = "round";
        for (var trailIndex = 1; trailIndex < trail.length; trailIndex++) {
          var start = pointOnSphere(trail[trailIndex - 1].pan, trail[trailIndex - 1].tilt, cx, cy, radius);
          var end = pointOnSphere(trail[trailIndex].pan, trail[trailIndex].tilt, cx, cy, radius);
          var age = (now - trail[trailIndex].time) / trailLifetime;
          var opacity = Math.max(0, (1 - age) * 0.82);
          if (opacity <= 0) continue;
          ctx.beginPath();
          ctx.moveTo(start.x, start.y);
          ctx.lineTo(end.x, end.y);
          ctx.strokeStyle = "rgba(133, 255, 236, " + opacity.toFixed(3) + ")";
          ctx.lineWidth = 2.8 - age * 1.2;
          ctx.shadowColor = "rgba(82, 255, 227, " + (opacity * 0.72).toFixed(3) + ")";
          ctx.shadowBlur = 6;
          ctx.stroke();
        }
        ctx.restore();
      }
      ctx.restore();

      // Current camera aim; a dashed halo indicates it is on the far hemisphere.
      var marker = pointOnSphere(pan, tilt, cx, cy, radius);
      var markerX = marker.x;
      var markerY = marker.y;
      ctx.save();
      ctx.beginPath();
      ctx.arc(markerX, markerY, 6.3, 0, Math.PI * 2);
      ctx.strokeStyle = depth >= 0 ? "rgba(241, 255, 255, 0.96)" : "rgba(202, 222, 229, 0.66)";
      ctx.lineWidth = 1.4;
      ctx.setLineDash(depth >= 0 ? [] : [2, 2]);
      ctx.stroke();
      ctx.setLineDash([]);
      ctx.beginPath();
      ctx.arc(markerX, markerY, 2.45, 0, Math.PI * 2);
      ctx.fillStyle = depth >= 0 ? "#f6ffff" : "#a9c2ce";
      ctx.shadowColor = "rgba(132, 255, 241, 0.95)";
      ctx.shadowBlur = 9;
      ctx.fill();
      ctx.restore();

      // Subtle rim and specular highlight finish the illustration.
      ctx.beginPath();
      ctx.arc(cx, cy, radius, -2.22, 0.5);
      ctx.strokeStyle = "rgba(193, 248, 255, 0.53)";
      ctx.lineWidth = 1.15;
      ctx.stroke();
      ctx.beginPath();
      ctx.ellipse(cx - 15, cy - radius + 5, 25, 5, -0.48, Math.PI, Math.PI * 1.72);
      ctx.strokeStyle = "rgba(231, 255, 255, 0.42)";
      ctx.lineWidth = 1.25;
      ctx.stroke();

      var panLabel = localized("preview.coveragePan", "Pan");
      var tiltLabel = localized("preview.coverageTilt", "Tilt");
      var formatAngle = function (value) { return (value > 0 ? "+" : "") + Math.round(value) + "°"; };
      readout.textContent = panLabel + " " + formatAngle(pan) + " · " + tiltLabel + " " + formatAngle(tilt);
    }

    function setVisible(visible, persist, restoreFocus) {
      panel.hidden = !visible;
      toggle.hidden = visible;
      toggle.setAttribute("aria-checked", String(visible));
      if (visible) {
        drawGlobe();
        if (syncTimer === null) syncTimer = window.setInterval(drawGlobe, 35);
      } else if (syncTimer !== null) {
        window.clearInterval(syncTimer);
        syncTimer = null;
      }
      if (persist) {
        try { window.localStorage.setItem("emeet-pixyd-coverage-globe", visible ? "1" : "0"); }
        catch (_error) { /* Private browsing can disable local storage. */ }
      }
      if (restoreFocus) (visible ? eye : toggle).focus();
    }

    var initiallyVisible = false;
    try { initiallyVisible = window.localStorage.getItem("emeet-pixyd-coverage-globe") === "1"; }
    catch (_error) { /* Private browsing can disable local storage. */ }
    setVisible(initiallyVisible, false, false);

    toggle.addEventListener("click", function () {
      setVisible(true, true, true);
    });
    eye.addEventListener("click", function () {
      setVisible(false, true, true);
    });

    canvas.addEventListener("pointerdown", function (event) {
      if (event.button !== 0) return;
      event.preventDefault();
      var rect = canvas.getBoundingClientRect();
      sphereDrag = {
        pointerId: event.pointerId,
        startX: event.clientX,
        startY: event.clientY,
        startPan: sliderValue("pan", 0),
        startTilt: sliderValue("tilt", 0),
        width: rect.width,
        height: rect.height,
      };
      canvas.classList.add("is-dragging");
      canvas.setPointerCapture(event.pointerId);
    });
    canvas.addEventListener("pointermove", function (event) {
      if (!sphereDrag || event.pointerId !== sphereDrag.pointerId) return;
      var dx = event.clientX - sphereDrag.startX;
      var dy = event.clientY - sphereDrag.startY;
      setSlider("pan", sphereDrag.startPan + dx * 300 / sphereDrag.width);
      setSlider("tilt", sphereDrag.startTilt - dy * 180 / sphereDrag.height);
    });
    function endSphereDrag(event) {
      if (!sphereDrag || event.pointerId !== sphereDrag.pointerId) return;
      if (canvas.hasPointerCapture(event.pointerId)) canvas.releasePointerCapture(event.pointerId);
      canvas.classList.remove("is-dragging");
      sphereDrag = null;
    }
    canvas.addEventListener("pointerup", endSphereDrag);
    canvas.addEventListener("pointercancel", endSphereDrag);
    canvas.addEventListener("lostpointercapture", function () {
      canvas.classList.remove("is-dragging");
      sphereDrag = null;
    });
    canvas.addEventListener("keydown", function (event) {
      var step = event.shiftKey ? 15 : 5;
      switch (event.key) {
        case "ArrowLeft": setSlider("pan", sliderValue("pan", 0) - step); break;
        case "ArrowRight": setSlider("pan", sliderValue("pan", 0) + step); break;
        case "ArrowUp": setSlider("tilt", sliderValue("tilt", 0) + step); break;
        case "ArrowDown": setSlider("tilt", sliderValue("tilt", 0) - step); break;
        default: return;
      }
      event.preventDefault();
    });

    document.addEventListener("input", function (event) {
      if (panel.hidden || !event.target || !/^slider-(pan|tilt)$/.test(event.target.id || "")) return;
      drawGlobe();
    });
    var statusPanel = document.getElementById("status-panel");
    if (statusPanel && window.MutationObserver) {
      var ptzObserver = new MutationObserver(function () {
        if (!panel.hidden) drawGlobe();
      });
      ptzObserver.observe(statusPanel, {
        attributes: true,
        attributeFilter: ["value"],
        childList: true,
        subtree: true,
      });
    }
    document.addEventListener("locale:changed", function () {
      if (!panel.hidden) drawGlobe();
    });
  })();

  /* -------------------------------------------------------------------- */
  /*  Capture mode selector                                                */
  /* -------------------------------------------------------------------- */

  (function () {
    var select = document.getElementById("capture-mode-select");
    if (!select) return;

    select.addEventListener("change", function () {
      select.disabled = true;
      fetch("/api/capture-mode", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ mode: select.value }),
      })
        .then(function (response) {
          return response.json().catch(function () { return {}; }).then(function (result) {
            if (!response.ok) {
              var error = new Error("HTTP " + response.status);
              error.status = response.status;
              error.result = result;
              throw error;
            }
            return result;
          });
        })
        .then(function (result) {
          select.dataset.current = result.mode;
          var zoomHelp = document.getElementById("capture-mode-help");
          if (zoomHelp) {
            var key = result.zoomAvailable ? "preview.zoomAvailable" : "preview.zoomUnavailable";
            zoomHelp.textContent = window.__i18n ? window.__i18n(key) : result.zoomAvailable
              ? "Zoom digital disponible hasta 1,5×. Cambiar el modo reinicia el stream."
              : "Zoom digital no disponible en este modo. Cambiar el modo reinicia el stream.";
          }
          var img = document.getElementById("preview-img");
          if (img) img.src = "/api/stream?mode=" + encodeURIComponent(result.mode) + "&ts=" + Date.now();
        })
        .catch(function (error) {
          var currentSelect = document.getElementById("capture-mode-select") || select;
          if (error.status === 409 && error.result && error.result.rtspActive) {
            currentSelect.value = error.result.mode || currentSelect.dataset.current;
            currentSelect.dataset.current = error.result.mode || currentSelect.dataset.current;
            currentSelect.disabled = true;
            var blockedMessage = window.__i18n
              ? window.__i18n("preview.pauseRTSPBeforeMode")
              : "Pausá la emisión RTSP antes de cambiar la resolución.";
            showToast(blockedMessage, "error");
            return;
          }
          currentSelect.value = currentSelect.dataset.current;
          showToast("No se pudo cambiar el modo de captura", "error");
        })
        .finally(function () {
          var currentSelect = document.getElementById("capture-mode-select") || select;
          currentSelect.disabled = Boolean(document.getElementById("rtsp-pause-btn"));
        });
    });
  })();

  /* -------------------------------------------------------------------- */
  /*  Local microphone monitor (browser-only, no recording/network upload) */
  /* -------------------------------------------------------------------- */

  (function () {
    var button = document.getElementById("audio-monitor-toggle");
    if (!button) return;

    var stream = null;
    var audioContext = null;
    var source = null;
    var gainNode = null;
    var monitoring = false;
    var busy = false;

    function translate(key, fallback) {
      return window.__i18n ? window.__i18n(key) : fallback;
    }

    function syncMonitorUI(statusKey) {
      var currentButton = document.getElementById("audio-monitor-toggle");
      var status = document.getElementById("audio-monitor-status");
      if (!currentButton) return;
      currentButton.classList.toggle("on", monitoring);
      currentButton.setAttribute("aria-checked", String(monitoring));
      currentButton.setAttribute("aria-busy", String(busy));
      currentButton.disabled = busy;
      if (status) {
        var statusText = statusKey ? translate(statusKey, "") : "";
        if (status.textContent !== statusText) status.textContent = statusText;
      }
    }

    function showMonitorError(key, fallback) {
      var message = translate(key, fallback);
      if (window.__showToast) window.__showToast(message, "error");
      syncMonitorUI("");
    }

    async function stopMonitoring() {
      monitoring = false;
      if (source) {
        try { source.disconnect(); } catch (_error) { /* Already disconnected. */ }
      }
      if (gainNode) {
        try { gainNode.disconnect(); } catch (_error) { /* Already disconnected. */ }
      }
      if (stream) stream.getTracks().forEach(function (track) { track.stop(); });
      source = null;
      gainNode = null;
      stream = null;
      var context = audioContext;
      audioContext = null;
      if (context && context.state !== "closed") {
        try { await context.close(); } catch (_error) { /* Release best-effort. */ }
      }
      syncMonitorUI("");
    }

    async function startMonitoring() {
      if (!window.isSecureContext || !navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) {
        showMonitorError(
          "audio.monitorSecureContext",
          "Abre la interfaz en localhost o HTTPS para escuchar el micrófono.",
        );
        return;
      }

      var Context = window.AudioContext || window.webkitAudioContext;
      if (!Context) {
        showMonitorError("audio.monitorFailed", "Este navegador no permite monitorizar el micrófono.");
        return;
      }

      busy = true;
      syncMonitorUI("audio.monitorStarting");
      var permissionStream = null;
      var pendingStream = null;
      var pendingContext = null;
      var pendingSource = null;
      var pendingGain = null;

      try {
        // Create/resume synchronously from the user's click before awaiting permission.
        pendingContext = new Context();
        var contextReady = pendingContext.resume();
        permissionStream = await navigator.mediaDevices.getUserMedia({ audio: true });
        permissionStream.getTracks().forEach(function (track) { track.stop(); });
        permissionStream = null;

        var devices = await navigator.mediaDevices.enumerateDevices();
        var pixyInput = devices.find(function (device) {
          return device.kind === "audioinput" && /emeet|pixy/i.test(device.label);
        });
        if (!pixyInput || !pixyInput.deviceId) {
          throw new Error("audio.monitorDeviceMissing");
        }

        pendingStream = await navigator.mediaDevices.getUserMedia({
          audio: {
            deviceId: { exact: pixyInput.deviceId },
            echoCancellation: false,
            noiseSuppression: false,
            autoGainControl: false,
          },
        });
        await contextReady;
        pendingSource = pendingContext.createMediaStreamSource(pendingStream);
        pendingGain = pendingContext.createGain();
        pendingGain.gain.value = 0.7;
        pendingSource.connect(pendingGain);
        pendingGain.connect(pendingContext.destination);

        stream = pendingStream;
        audioContext = pendingContext;
        source = pendingSource;
        gainNode = pendingGain;
        pendingStream = null;
        pendingContext = null;
        pendingSource = null;
        pendingGain = null;
        monitoring = true;
        if (window.__showToast) window.__showToast(translate("audio.monitorOn", "Microphone monitoring on."), "success");
      } catch (error) {
        if (permissionStream) permissionStream.getTracks().forEach(function (track) { track.stop(); });
        if (pendingSource) {
          try { pendingSource.disconnect(); } catch (_disconnectError) { /* Best-effort cleanup. */ }
        }
        if (pendingGain) {
          try { pendingGain.disconnect(); } catch (_disconnectError) { /* Best-effort cleanup. */ }
        }
        if (pendingStream) pendingStream.getTracks().forEach(function (track) { track.stop(); });
        if (pendingContext && pendingContext.state !== "closed") {
          try { await pendingContext.close(); } catch (_closeError) { /* Best-effort cleanup. */ }
        }

        if (error && error.message === "audio.monitorDeviceMissing") {
          showMonitorError("audio.monitorDeviceMissing", "EMEET PIXY is not listed as a microphone input.");
        } else if (error && (error.name === "NotAllowedError" || error.name === "SecurityError")) {
          showMonitorError("audio.monitorPermissionDenied", "Allow microphone access in the browser and try again.");
        } else {
          showMonitorError("audio.monitorFailed", "Could not start microphone monitoring.");
        }
      } finally {
        busy = false;
        syncMonitorUI(monitoring ? "audio.monitorOn" : "");
      }
    }

    document.addEventListener("click", function (event) {
      var target = event.target && event.target.closest ? event.target.closest("#audio-monitor-toggle") : null;
      if (!target || busy) return;
      if (monitoring) {
        busy = true;
        syncMonitorUI("audio.monitorStarting");
        stopMonitoring().finally(function () {
          busy = false;
          syncMonitorUI("");
        });
      } else {
        startMonitoring();
      }
    });

    var statusPanel = document.getElementById("status-panel");
    if (statusPanel && window.MutationObserver) {
      var audioPanelObserver = new MutationObserver(function () { syncMonitorUI(monitoring ? "audio.monitorOn" : ""); });
      audioPanelObserver.observe(statusPanel, { childList: true, subtree: true });
    }

    window.addEventListener("beforeunload", function () {
      if (stream) stream.getTracks().forEach(function (track) { track.stop(); });
      if (audioContext && audioContext.state !== "closed") audioContext.close();
    });
    syncMonitorUI("");
  })();

  /* -------------------------------------------------------------------- */
  /*  RTSP destination visibility                                          */
  /* -------------------------------------------------------------------- */

  (function () {
    var input = document.getElementById("rtsp-url-input");
    var toggle = document.getElementById("rtsp-url-toggle");
    if (!input || !toggle) return;

    function refreshLabel() {
      var key = input.type === "password" ? "rtsp.showURL" : "rtsp.hideURL";
      var label = window.__i18n ? window.__i18n(key) : input.type === "password" ? "Mostrar" : "Ocultar";
      toggle.textContent = label;
      toggle.setAttribute("data-i18n", key);
      toggle.setAttribute("aria-pressed", String(input.type === "text"));
    }

    toggle.addEventListener("click", function () {
      input.type = input.type === "password" ? "text" : "password";
      refreshLabel();
    });
    document.addEventListener("locale:changed", refreshLabel);
  })();

  /* -------------------------------------------------------------------- */
  /*  Keyboard shortcuts                                                  */
  /* -------------------------------------------------------------------- */

  function clickDataStarButton(selector) {
    var btn = document.querySelector(selector);
    if (btn && !btn.disabled) btn.click();
  }

  document.addEventListener("keydown", function (e) {
    if (
      e.target.tagName === "INPUT" ||
      e.target.tagName === "TEXTAREA" ||
      e.target.tagName === "SELECT"
    )
      return;

    if (e.key === "?") {
      e.preventDefault();
      toggleLegend();
      return;
    }

    if (e.key === "Escape") {
      toggleLegend(false);
      return;
    }

    var modeMap = {
      t: ".mode-track",
      i: ".mode-idle",
      p: ".mode-privacy",
    };
    var selector = modeMap[e.key.toLowerCase()];
    if (selector) {
      e.preventDefault();
      clickDataStarButton(selector);
      return;
    }

    if (e.key.toLowerCase() === "c") {
      e.preventDefault();
      var centerBtn = document.querySelector('[aria-label="Center camera"]');
      if (centerBtn && !centerBtn.disabled) centerBtn.click();
      return;
    }

    var ptzStep = { pan: 5, tilt: 5, zoom: 10 };
    var ptzAction = null;
    switch (e.key) {
      case "ArrowLeft":
        ptzAction = { axis: "pan", delta: -ptzStep.pan };
        break;
      case "ArrowRight":
        ptzAction = { axis: "pan", delta: ptzStep.pan };
        break;
      case "ArrowUp":
        ptzAction = { axis: "tilt", delta: ptzStep.tilt };
        break;
      case "ArrowDown":
        ptzAction = { axis: "tilt", delta: -ptzStep.tilt };
        break;
      case "+":
      case "=":
        ptzAction = { axis: "zoom", delta: ptzStep.zoom };
        break;
      case "-":
      case "_":
        ptzAction = { axis: "zoom", delta: -ptzStep.zoom };
        break;
    }
    if (!ptzAction) return;
    e.preventDefault();
    var slider = document.getElementById("slider-" + ptzAction.axis);
    if (!slider) return;
    var current = parseInt(slider.value, 10) || 0;
    var next = current + ptzAction.delta;
    var min = parseInt(slider.min, 10);
    var max = parseInt(slider.max, 10);
    next = Math.max(min, Math.min(max, next));
    slider.value = String(next);
    slider.dispatchEvent(new Event("input", { bubbles: true }));
  });

  /* -------------------------------------------------------------------- */
  /*  Shortcut legend toggle                                              */
  /* -------------------------------------------------------------------- */

  function toggleLegend(force) {
    var legend = document.getElementById("shortcut-legend");
    var fab = document.getElementById("shortcut-fab");
    if (!legend) return;
    var shouldShow = force !== undefined ? force : legend.hidden;
    legend.hidden = !shouldShow;
    if (fab) fab.setAttribute("aria-expanded", String(shouldShow));
  }

  (function () {
    var fab = document.getElementById("shortcut-fab");
    var closeBtn = document.getElementById("legend-close");
    if (fab) {
      fab.setAttribute("aria-expanded", "false");
      fab.addEventListener("click", function () {
        toggleLegend();
      });
    }
    if (closeBtn) {
      closeBtn.addEventListener("click", function () {
        toggleLegend(false);
      });
    }
  })();

  /* -------------------------------------------------------------------- */
  /*  Snapshot button                                                     */
  /* -------------------------------------------------------------------- */

  (function () {
    var snapshotBtn = document.getElementById("snapshot-btn");
    if (!snapshotBtn) return;

    snapshotBtn.addEventListener("click", function () {
      snapshotBtn.classList.add("flash");
      setTimeout(function () {
        snapshotBtn.classList.remove("flash");
      }, 400);

      fetch("/api/snapshot?ts=" + Date.now())
        .then(function (resp) {
          if (!resp.ok) throw new Error("HTTP " + resp.status);
          return resp.blob();
        })
        .then(function (blob) {
          if (blob.size === 0) throw new Error("empty");
          var url = URL.createObjectURL(blob);
          var a = document.createElement("a");
          a.href = url;
          a.download =
            "pixy-" + new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-") + ".jpg";
          document.body.appendChild(a);
          a.click();
          a.remove();
          URL.revokeObjectURL(url);
          showToast("Snapshot saved", "success");
        })
        .catch(function () {
          showToast("Snapshot unavailable", "error");
        });
    });
  })();

  /* -------------------------------------------------------------------- */
  /*  Preview stream error recovery                                       */
  /* -------------------------------------------------------------------- */

  (function () {
    var img = document.getElementById("preview-img");
    if (!img) return;
    var STREAM_MAX_RETRIES = 10;
    var retryTimer = null;
    var streamRetryCount = 0;
    var streamRetryDelay = STREAM_RETRY_INITIAL_MS;

    img.addEventListener("load", function () {
      streamRetryDelay = STREAM_RETRY_INITIAL_MS;
      streamRetryCount = 0;
    });

    img.addEventListener("error", function () {
      if (retryTimer) return;
      streamRetryCount++;

      if (streamRetryCount > STREAM_MAX_RETRIES) {
        this.style.display = "none";
        var fallback = document.getElementById("preview-fallback");
        if (fallback) {
          fallback.style.display = "flex";
          var label = fallback.querySelector("div:last-child");
          if (label) label.textContent = "Stream unavailable \u2014 reload page to retry";
        }
        streamRetryDelay = STREAM_RETRY_INITIAL_MS;
        streamRetryCount = 0;
        return;
      }

      this.style.display = "none";
      var fallback = document.getElementById("preview-fallback");
      if (fallback) {
        fallback.style.display = "flex";
        var label = fallback.querySelector("div:last-child");
        if (label)
          label.textContent =
            "Reconnecting\u2026 (" + streamRetryCount + "/" + STREAM_MAX_RETRIES + ")";
      }
      var delay = streamRetryDelay;
      retryTimer = setTimeout(function () {
        retryTimer = null;
        img.src = "/api/stream?" + Date.now();
        img.style.display = "";
        if (fallback) fallback.style.display = "none";
      }, delay);
      streamRetryDelay = Math.min(streamRetryDelay * 2, STREAM_RETRY_MAX_MS);
    });
  })();
})();
