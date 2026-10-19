(function (global) {
  'use strict';

  var BASE_STYLE_ID = 'miyabi-player-base';

  function ensureBaseStyle() {
    if (document.getElementById(BASE_STYLE_ID)) return;
    var style = document.createElement('style');
    style.id = BASE_STYLE_ID;
    style.textContent = [
      '.miyabi-player{position:relative;width:100%;height:100%;background:#000;overflow:hidden}',
      '.miyabi-player-stage{position:relative;width:100%;height:100%;background:#000}',
      '.miyabi-player-video{position:absolute;inset:0;width:100%;height:100%;object-fit:contain;background:#000}',
      // Fill the frame when the window ratio no longer matches the source.
      '.miyabi-player-cover .miyabi-player-video{object-fit:cover}',
      '.miyabi-player-cover .miyabi-player-xg video{object-fit:cover}',
      '.miyabi-player-snapshot{position:absolute;inset:0;width:100%;height:100%;object-fit:contain;background:#000;z-index:1;transition:opacity .16s ease-out}',
      '.miyabi-player-xg{position:absolute;inset:0}',
      '.miyabi-player-xg .xgplayer{width:100%;height:100%}',
      '.miyabi-player:fullscreen .miyabi-player-stage{height:100vh}'
    ].join('');
    document.head.appendChild(style);
  }

  function extensionOf(url) {
    var path = String(url || '').split('#')[0].split('?')[0];
    var dot = path.lastIndexOf('.');
    return dot >= 0 ? path.slice(dot + 1).toLowerCase() : '';
  }

  function createPlayer(container, options) {
    if (!container) throw new Error('createPlayer requires a container element');
    options = options || {};
    ensureBaseStyle();

    container.classList.add('miyabi-player');
    container.innerHTML = '';

    var stage = document.createElement('div');
    stage.className = 'miyabi-player-stage';
    var xgContainer = document.createElement('div');
    xgContainer.id = 'xgplayerContainer';
    xgContainer.className = 'miyabi-player-xg';
    var video = document.createElement('video');
    video.id = 'videoPlayer';
    video.className = 'miyabi-player-video';
    video.playsInline = true;
    video.setAttribute('playsinline', '');
    if (options.poster) video.poster = options.poster;
    var controlHost = document.createElement('div');
    controlHost.id = 'tmControlHost';

    xgContainer.style.display = 'none';
    stage.appendChild(xgContainer);
    stage.appendChild(video);
    stage.appendChild(controlHost);
    container.appendChild(stage);

    var list = Array.isArray(options.sources) ? options.sources.slice() : [];
    var index = 0;
    var hls = null;
    var xg = null;
    var destroyed = false;
    var controlsHidden = false;
    var prefs = {
      volume: typeof options.volume === 'number' ? options.volume : 1,
      muted: !!options.muted,
      rate: typeof options.rate === 'number' ? options.rate : 1
    };

    function activeVideo() {
      if (xg && xg.video) return xg.video;
      return video;
    }

    // Volume, mute and rate live on the media element, so every new element must
    // be seeded again; changes are reported upward for persistence.
    function applyPrefs(target) {
      if (!target) return;
      try {
        target.volume = Math.min(1, Math.max(0, prefs.volume));
        target.muted = prefs.muted;
        target.playbackRate = prefs.rate;
      } catch (e) { /* element not ready */ }
      if (target.__miyabiPrefsBound) return;
      target.__miyabiPrefsBound = true;
      function report() {
        prefs.volume = target.volume;
        prefs.muted = target.muted;
        prefs.rate = target.playbackRate;
        if (options.onPrefsChange) {
          options.onPrefsChange({ volume: prefs.volume, muted: prefs.muted, rate: prefs.rate });
        }
      }
      target.addEventListener('volumechange', report);
      target.addEventListener('ratechange', report);
    }

    function setVolume(value) {
      var v = activeVideo();
      if (!v) return;
      v.muted = false;
      v.volume = Math.min(1, Math.max(0, value));
    }

    var controls = null;
    if (typeof global.TmPlayerControls === 'function') {
      controls = new global.TmPlayerControls({
        host: controlHost,
        videoWrapper: stage,
        getVideo: activeVideo,
        onToggleFullscreen: function () { requestFullscreenToggle(); },
        onCaptureGif: function () {
          if (typeof global.openGifCapture === 'function') global.openGifCapture();
        }
      }).init();
    }

    // The page owns the fullscreen state in the desktop shell (window mode), so
    // the control bar must render from an explicit flag instead of sniffing the
    // DOM. In the browser the kernel still drives element fullscreen itself.
    function setFullscreenState(active) {
      if (controls && typeof controls.setFullscreen === 'function') {
        controls.setFullscreen(active);
      }
    }

    function onFullscreenChange() {
      var active = !!(document.fullscreenElement || document.webkitFullscreenElement);
      setFullscreenState(active);
      if (options.onFullscreenChange) options.onFullscreenChange(active);
    }

    function teardownEngine() {
      if (hls) {
        try { hls.destroy(); } catch (e) { /* ignore */ }
        hls = null;
      }
      if (xg) {
        try { xg.destroy(); } catch (e) { /* ignore */ }
        xg = null;
      }
      xgContainer.style.display = 'none';
      xgContainer.innerHTML = '';
      video.style.display = '';
      video.removeAttribute('src');
      try { video.load(); } catch (e) { /* ignore */ }
    }

    // A switch tears the engine down before the next source is decoded, which
    // used to show a black frame. Hold the last painted frame instead.
    var snapshot = null;

    function captureFrame() {
      var v = activeVideo();
      if (!v || !v.videoWidth || v.readyState < 2) return null;
      try {
        var canvas = document.createElement('canvas');
        canvas.width = v.videoWidth;
        canvas.height = v.videoHeight;
        canvas.getContext('2d').drawImage(v, 0, 0);
        return canvas;
      } catch (e) {
        return null; // tainted canvas: fall back to black
      }
    }

    function showSnapshot(frame) {
      hideSnapshot();
      var node = document.createElement('canvas');
      node.width = frame.width;
      node.height = frame.height;
      node.className = 'miyabi-player-snapshot';
      node.getContext('2d').drawImage(frame, 0, 0);
      stage.appendChild(node);
      snapshot = node;
    }

    function hideSnapshot() {
      if (!snapshot) return;
      var node = snapshot;
      snapshot = null;
      node.style.opacity = '0';
      setTimeout(function () {
        if (node.parentNode) node.parentNode.removeChild(node);
      }, 200);
    }

    function bindFirstFrame(target) {
      if (target.__miyabiFirstFrameBound) return;
      target.__miyabiFirstFrameBound = true;
      target.addEventListener('loadeddata', hideSnapshot);
    }

    function bindEnded(target) {
      if (target.__miyabiEndedBound) return;
      target.__miyabiEndedBound = true;
      target.addEventListener('ended', function () {
        var hasNext = index + 1 < list.length;
        if (options.onEnded) options.onEnded(index, list[index] || null, hasNext);
        if (options.continueOnEnded !== false && hasNext) next();
      });
    }

    // Reports the real frame size so the desktop shell can size the borderless
    // window to the source instead of guessing 16:9.
    function bindMetadata(target) {
      if (target.__miyabiMetadataBound) return;
      target.__miyabiMetadataBound = true;
      target.addEventListener('loadedmetadata', function () {
        if (!options.onMetadata) return;
        if (!target.videoWidth || !target.videoHeight) return;
        options.onMetadata(target.videoWidth, target.videoHeight);
      });
    }

    function loadAt(target) {
      var frame = captureFrame();
      teardownEngine();
      if (frame) showSnapshot(frame);
      var src = list[target];
      if (!src) return;
      index = target;
      var kind = extensionOf(src.url);
      if ((kind === 'flv' || kind === 'mkv') && typeof global.Player === 'function') {
        video.style.display = 'none';
        xgContainer.style.display = 'block';
        var config = {
          id: 'xgplayerContainer', url: src.url, playsinline: true, fluid: true,
          controls: false, autoplay: true, isLive: kind === 'flv'
        };
        if (kind === 'flv' && typeof global.FlvPlayer === 'function') config.plugins = [global.FlvPlayer];
        xg = new global.Player(config);
        bindEnded(xg.video);
        bindMetadata(xg.video);
        bindFirstFrame(xg.video);
        applyPrefs(xg.video);
      } else if (kind === 'm3u8' && global.Hls && global.Hls.isSupported()) {
        xgContainer.style.display = 'none';
        video.style.display = '';
        hls = new global.Hls({ enableWorker: true });
        hls.loadSource(src.url);
        hls.attachMedia(video);
        hls.on(global.Hls.Events.MANIFEST_PARSED, function () {
          video.play().catch(function () { /* autoplay may be blocked */ });
        });
        bindEnded(video);
        bindMetadata(video);
        bindFirstFrame(video);
        applyPrefs(video);
      } else {
        xgContainer.style.display = 'none';
        video.style.display = '';
        video.src = src.url;
        video.play().catch(function () { /* autoplay may be blocked */ });
        bindEnded(video);
        bindMetadata(video);
        bindFirstFrame(video);
        applyPrefs(video);
      }
      if (controls && typeof controls.setVideo === 'function') {
        setTimeout(function () { controls.setVideo(); }, 0);
      }
      if (options.onChange) options.onChange(index, src);
    }

    function next() {
      if (index + 1 < list.length) loadAt(index + 1);
    }

    function prev() {
      if (index > 0) loadAt(index - 1);
    }

    function seekBy(delta) {
      var v = activeVideo();
      if (!v || !isFinite(v.duration)) return;
      v.currentTime = Math.min(Math.max(0, v.currentTime + delta), v.duration);
    }

    function togglePlay() {
      var v = activeVideo();
      if (!v) return;
      if (v.paused) v.play().catch(function () { /* autoplay may be blocked */ });
      else v.pause();
    }

    function isTypingTarget(target) {
      if (!target) return false;
      var tag = target.tagName;
      return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || target.isContentEditable;
    }

    function isButtonTarget(target) {
      return !!target && (target.tagName === 'BUTTON' || target.tagName === 'A');
    }

    function isCoarsePointer() {
      return !!(window.matchMedia && window.matchMedia('(pointer: coarse)').matches);
    }

    // Element fullscreen where available; iOS only exposes native video
    // fullscreen, which is the only way to leave Safari's chrome there.
    function toggleElementFullscreen() {
      var doc = document;
      var active = doc.fullscreenElement || doc.webkitFullscreenElement;
      if (active) {
        (doc.exitFullscreen || doc.webkitExitFullscreen || function () {}).call(doc);
        if (screen.orientation && screen.orientation.unlock) {
          try { screen.orientation.unlock(); } catch (e) { /* ignore */ }
        }
        return;
      }
      if (stage.requestFullscreen) {
        stage.requestFullscreen().then(function () {
          // Mobile browsers only honour the lock while fullscreen.
          if (isCoarsePointer() && screen.orientation && screen.orientation.lock) {
            screen.orientation.lock('landscape').catch(function () { /* unsupported */ });
          }
        }).catch(function () { /* rejected */ });
        return;
      }
      var v = activeVideo();
      if (v && v.webkitEnterFullscreen) v.webkitEnterFullscreen();
    }

    // The shell routes this to its window mode; the browser falls back to the
    // element fullscreen implemented above.
    function requestFullscreenToggle() {
      if (typeof options.onToggleFullscreen === 'function') {
        options.onToggleFullscreen();
        return;
      }
      toggleElementFullscreen();
    }

    function onKeyDown(event) {
      if (destroyed || event.defaultPrevented) return;
      if (event.ctrlKey || event.metaKey || event.altKey) return;
      if (isTypingTarget(event.target)) return;
      var v = activeVideo();
      if (!v) return;
      switch (event.key) {
        case ' ':
          if (isButtonTarget(event.target)) return;
          togglePlay();
          event.preventDefault();
          break;
        case 'k':
        case 'K':
          togglePlay();
          event.preventDefault();
          break;
        case 'Enter':
          if (isButtonTarget(event.target)) return;
          togglePlay();
          event.preventDefault();
          break;
        case 'ArrowLeft':
          seekBy(event.shiftKey ? -30 : -3);
          event.preventDefault();
          break;
        case 'ArrowRight':
          seekBy(event.shiftKey ? 30 : 3);
          event.preventDefault();
          break;
        case 'ArrowUp':
          setVolume(v.volume + 0.05);
          event.preventDefault();
          break;
        case 'ArrowDown':
          setVolume(v.volume - 0.05);
          event.preventDefault();
          break;
        case 'm':
        case 'M':
          v.muted = !v.muted;
          event.preventDefault();
          break;
        default:
          break;
      }
    }

    document.addEventListener('keydown', onKeyDown);
    document.addEventListener('fullscreenchange', onFullscreenChange);
    document.addEventListener('webkitfullscreenchange', onFullscreenChange);

    video.addEventListener('timeupdate', function () {
      if (options.onProgress) options.onProgress(video.currentTime, video.duration);
    });

    // Clicking the picture toggles play/pause even while the control panel is collapsed.
    stage.addEventListener('click', function (event) {
      if (event.target.closest('#tmControlHost, #gcPanel, button, input, a')) return;
      var v = activeVideo();
      if (!v) return;
      if (v.paused) v.play().catch(function () {});
      else v.pause();
    });

    if (list.length) loadAt(Number.isInteger(options.startIndex) ? options.startIndex : 0);

    return {
      play: function () { var v = activeVideo(); if (v && v.paused) v.play().catch(function () {}); },
      pause: function () { var v = activeVideo(); if (v && !v.paused) v.pause(); },
      seek: function (seconds) { var v = activeVideo(); if (v) v.currentTime = seconds; },
      seekBy: seekBy,
      setVolume: setVolume,
      next: next,
      prev: prev,
      hasNext: function () { return index + 1 < list.length; },
      hasPrev: function () { return index > 0; },
      current: function () { return { index: index, source: list[index] || null }; },
      setSources: function (sources, startIndex) {
        list = Array.isArray(sources) ? sources.slice() : [];
        loadAt(Number.isInteger(startIndex) ? startIndex : 0);
      },
      setQueue: function (sources) {
        if (Array.isArray(sources)) list = sources.slice();
      },
      stop: function () { teardownEngine(); },
      setControlsHidden: function (hidden) {
        controlsHidden = !!hidden;
        controlHost.style.display = controlsHidden ? 'none' : '';
        if (!controlsHidden && controls) controls.show({ autoHide: false });
      },
      isControlsHidden: function () { return controlsHidden; },
      toggleFullscreen: requestFullscreenToggle,
      setFullscreenState: setFullscreenState,
      destroy: function () {
        if (destroyed) return;
        destroyed = true;
        document.removeEventListener('keydown', onKeyDown);
        document.removeEventListener('fullscreenchange', onFullscreenChange);
        document.removeEventListener('webkitfullscreenchange', onFullscreenChange);
        hideSnapshot();
        teardownEngine();
        if (controls && typeof controls.destroy === 'function') controls.destroy();
        container.classList.remove('miyabi-player');
        container.innerHTML = '';
      }
    };
  }

  global.MiyabiPlayer = { create: createPlayer };
})(window);
