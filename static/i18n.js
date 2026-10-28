// i18n Spanish/English translations for EMEET PIXY UI
// Compatible with both regular scripts and ES modules
// All translations exposed via window.__i18n and window.__i18nInitialized

(function() {
  // Translation catalog
  var i18nCatalog = {
    // Status
    status: {
      online: { es: 'En línea', en: 'Online' },
      offline: { es: 'Desconectado', en: 'Offline' },
      reconnecting: { es: 'Conexión perdida — reconectando…', en: 'Connection lost — reconnecting…' }
    },

    // Preview
    preview: {
      unavailable: { es: 'Cámara en uso o no disponible', en: 'Camera in use or unavailable' },
      snapshot: { es: 'Captura', en: 'Snapshot' },
      offline: { es: 'Cámara desconectada', en: 'Camera offline' },
      resolutionLabel: { es: 'Resolución y FPS', en: 'Resolution and FPS' },
      zoomAvailable: { es: 'Zoom digital disponible hasta 1,5×. Cambiar el modo reinicia el stream.', en: 'Digital zoom available up to 1.5×. Changing mode restarts the stream.' },
      zoomUnavailable: { es: 'Zoom digital no disponible en este modo. Cambiar el modo reinicia el stream.', en: 'Digital zoom not available in this mode. Changing mode restarts the stream.' },
      mouseHelp: {
        es: 'Controles con mouse: arrastrá el video; derecha = pan positivo, izquierda = pan negativo, arriba = tilt positivo y abajo = tilt negativo (aprox. 1° cada 4 px). Doble clic: zoom +10; clic derecho: zoom −10. Los sliders de posición se actualizan con cada movimiento.',
        en: 'Mouse controls: drag the video; right = positive pan, left = negative pan, up = positive tilt and down = negative tilt (approx. 1° every 4 px). Double click: zoom +10; right click: zoom −10. Position sliders update with each movement.'
      }
    },

    // Touch controls shown on phone-sized/coarse-pointer layouts.
    mobile: {
      controlsLabel: { es: 'Controles táctiles PTZ', en: 'Touch PTZ controls' },
      dpadLabel: { es: 'Pan y tilt', en: 'Pan and tilt' },
      panLeft: { es: 'Mover a la izquierda', en: 'Pan left' },
      panRight: { es: 'Mover a la derecha', en: 'Pan right' },
      tiltUp: { es: 'Inclinar hacia arriba', en: 'Tilt up' },
      tiltDown: { es: 'Inclinar hacia abajo', en: 'Tilt down' },
      zoomLabel: { es: 'Zoom', en: 'Zoom' },
      zoomIn: { es: 'Acercar', en: 'Zoom in' },
      zoomOut: { es: 'Alejar', en: 'Zoom out' }
    },

    // Presets
    presets: {
      title: { es: 'Presets', en: 'Presets' },
      pull: { es: 'Pull', en: 'Pull' },
      placeholder: { es: 'Guardar posición actual…', en: 'Save current position…' },
      save: { es: 'Guardar', en: 'Save' },
      hint: { es: 'Ingresá un nombre para guardar la posición actual; podés usar espacios.', en: 'Enter a name to save the current position; you can use spaces.' },
      emptyNameError: { es: 'Escribí un nombre para el preset', en: 'Write a name for the preset' }
    },

    // Route / Modo recorrido
    route: {
      title: { es: 'Modo recorrido', en: 'Route mode' },
      start: { es: 'Grabar', en: 'Record' },
      stop: { es: 'Detener', en: 'Stop' },
      stopRecording: { es: 'Detener grabación', en: 'Stop recording' },
      recordingLabel: { es: 'Grabando', en: 'Recording' },
      playingLabel: { es: 'Reproduciendo', en: 'Playing' },
      pendingHint: { es: 'pasos capturados — asignale un nombre para guardarlo.', en: 'steps captured — give it a name to save it.' },
      empty: { es: 'Todavía no hay recorridos guardados. Aparecerán aquí.', en: 'No saved routes yet. They will appear here.' },
      namePlaceholder: { es: 'Nombre del recorrido…', en: 'Route name…' },
      nameRequired: { es: 'Escribí un nombre para el recorrido', en: 'Enter a name for the route' },
      save: { es: 'Guardar', en: 'Save' },
      discard: { es: 'Descartar', en: 'Discard' },
      loop: { es: 'Repetir', en: 'Loop' },
      pingPong: { es: 'Ida y vuelta', en: 'Ping-pong' },
      finishing: { es: 'Finalizando recorrido y restaurando el seguimiento…', en: 'Finishing route and restoring tracking…' },
      recordingCount: { es: 'pasos', en: 'steps' }
    }
  };

  // Current locale state (defaults to Spanish)
  window.__currentLocale = 'es';

  // Try to restore locale from localStorage
  try {
    var stored = localStorage.getItem('emeet-pixyd-locale');
    if (stored && (stored === 'es' || stored === 'en')) {
      window.__currentLocale = stored;
    }
  } catch(e) {}

  // Apply locale to html element
  function applyLocale() {
    var html = document.documentElement;
    html.setAttribute('data-locale', window.__currentLocale);
    html.lang = window.__currentLocale;
    try { localStorage.setItem('emeet-pixyd-locale', window.__currentLocale); } catch(e) {}
  }

  // Look up translation for a dotted key path (e.g., "route.title")
  window.__i18n = function(key) {
    var keys = key.split('.');
    var obj = i18nCatalog;
    for (var i = 0; i < keys.length; i++) {
      if (!obj || !obj[keys[i]]) return key;
      obj = obj[keys[i]];
    }
    if (typeof obj === 'object' && !Array.isArray(obj)) {
      if (obj[window.__currentLocale]) return obj[window.__currentLocale];
      if (obj.es) return obj.es; // fallback to Spanish
      return key;
    }
    return obj || key;
  };

  function translate(root) {
    var scope = root || document;
    var elements = [];
    var selector = '[data-i18n], [data-i18n-placeholder], [data-i18n-aria-label]';
    if (scope.matches && scope.matches(selector)) elements.push(scope);
    elements = elements.concat(Array.prototype.slice.call(scope.querySelectorAll(selector)));
    elements.forEach(function(el) {
      var key = el.getAttribute('data-i18n');
      if (key) el.textContent = window.__i18n(key);
      var placeholderKey = el.getAttribute('data-i18n-placeholder');
      if (placeholderKey) el.setAttribute('placeholder', window.__i18n(placeholderKey));
      var ariaLabelKey = el.getAttribute('data-i18n-aria-label');
      if (ariaLabelKey) el.setAttribute('aria-label', window.__i18n(ariaLabelKey));
    });
  }

  // Update locale and re-apply translations.
  window.setLocale = function(locale) {
    if (locale !== 'es' && locale !== 'en') return;
    window.__currentLocale = locale;
    applyLocale();
    translate(document);
    var langLabel = document.getElementById('lang-toggle-label');
    if (langLabel) langLabel.textContent = locale.toUpperCase();
    document.dispatchEvent(new CustomEvent('locale:changed', { detail: { locale: locale } }));
  };

  // Initialize on DOM content loaded
  function initializeI18n() {
    applyLocale();
    translate(document);

    // Language toggle
    var langToggle = document.getElementById('lang-toggle');
    var langLabel = document.getElementById('lang-toggle-label');
    if (langLabel) langLabel.textContent = window.__currentLocale.toUpperCase();
    if (langToggle) {
      langToggle.addEventListener('click', function() {
        window.setLocale(window.__currentLocale === 'es' ? 'en' : 'es');
      });
    }

    // DataStar replaces route controls after each action. Translate newly
    // morphed elements too, preserving the selected language across patches.
    var observer = new MutationObserver(function(mutations) {
      mutations.forEach(function(mutation) {
        mutation.addedNodes.forEach(function(node) {
          if (node.nodeType === Node.ELEMENT_NODE) translate(node);
        });
      });
    });
    observer.observe(document.body, { childList: true, subtree: true });
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initializeI18n, { once: true });
  } else {
    initializeI18n();
  }

  // Mark as initialized so other scripts can check
  window.__i18nInitialized = true;
})();
