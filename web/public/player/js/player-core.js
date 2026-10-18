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

    function activeVideo() {
      if (xg && xg.video) return xg.video;
      return video;
    }

    var controls = null;
    if (typeof global.TmPlayerControls === 'function') {
      controls = new global.TmPlayerControls({
        host: controlHost,
        videoWrapper: stage,
        getVideo: activeVideo,
        onToggleFullscreen: function () { toggleFullscreen(); },
        onCaptureGif: function () {
          if (typeof global.openGifCapture === 'function') global.openGifCapture();
        }
      }).init();
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

    function bindEnded(target) {
      target.addEventListener('ended', function () {
        var hasNext = index + 1 < list.length;
        if (options.onEnded) options.onEnded(index, list[index] || null, hasNext);
        if (options.continueOnEnded !== false && hasNext) next();
      });
    }

    function loadAt(target) {
      teardownEngine();
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
      } else {
        xgContainer.style.display = 'none';
        video.style.display = '';
        video.src = src.url;
        video.play().catch(function () { /* autoplay may be blocked */ });
        bindEnded(video);
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

    function toggleFullscreen() {
      var doc = document;
      if (doc.fullscreenElement || doc.webkitFullscreenElement) {
        (doc.exitFullscreen || doc.webkitExitFullscreen || function () {}).call(doc);
      } else {
        (stage.requestFullscreen || stage.webkitRequestFullscreen || function () {}).call(stage);
      }
    }

    video.addEventListener('timeupdate', function () {
      if (options.onProgress) options.onProgress(video.currentTime, video.duration);
    });

    // Clicking the picture toggles play/pause even while the control panel is collapsed.
    stage.addEventListener('click', function (event) {
      if (event.target.closest('#tmControlHost, button, input, a')) return;
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
      toggleFullscreen: toggleFullscreen,
      destroy: function () {
        if (destroyed) return;
        destroyed = true;
        teardownEngine();
        if (controls && typeof controls.destroy === 'function') controls.destroy();
        container.classList.remove('miyabi-player');
        container.innerHTML = '';
      }
    };
  }

  global.MiyabiPlayer = { create: createPlayer };
})(window);
