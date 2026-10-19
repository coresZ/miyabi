/**
 * TmPlayerControls — ported from player.js ControlManager / ProgressManager / LoopManager
 * Overlay-only playback UI: progress, seek presets, A-B loop, volume, play/pause, speed.
 */
(function (global) {
  'use strict';

  const PLAY_SVG = `<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M8 5.14v13.72c0 .8.87 1.3 1.56.89l10.2-6.86a1 1 0 0 0 0-1.66L9.56 4.25A1.02 1.02 0 0 0 8 5.14z"/></svg>`;
  const PAUSE_SVG = `<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M7 5h3.2a1 1 0 0 1 1 1v12a1 1 0 0 1-1 1H7a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1zm6.8 0H17a1 1 0 0 1 1 1v12a1 1 0 0 1-1 1h-3.2a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1z"/></svg>`;
  const CENTER_PLAY_SVG = `<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M8 5.14v13.72c0 .8.87 1.3 1.56.89l10.2-6.86a1 1 0 0 0 0-1.66L9.56 4.25A1.02 1.02 0 0 0 8 5.14z"/></svg>`;
  const CENTER_PAUSE_SVG = `<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M7 5h3.2a1 1 0 0 1 1 1v12a1 1 0 0 1-1 1H7a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1zm6.8 0H17a1 1 0 0 1 1 1v12a1 1 0 0 1-1 1h-3.2a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1z"/></svg>`;
  const REWIND_SVG = `<svg width="14" height="14" viewBox="0 0 12 24" fill="none" class="tm-rewind-icon"><path fill-rule="evenodd" clip-rule="evenodd" d="M3.70711 4.29289C3.31658 3.90237 2.68342 3.90237 2.29289 4.29289L-4.70711 11.2929C-5.09763 11.6834 -5.09763 12.3166 -4.70711 12.7071L2.29289 19.7071C2.68342 20.0976 3.31658 20.0976 3.70711 19.7071C4.09763 19.3166 4.09763 18.6834 3.70711 18.2929L-2.58579 12L3.70711 5.70711C4.09763 5.31658 4.09763 4.68342 3.70711 4.29289Z" fill="currentColor"/></svg>`;
  const FORWARD_SVG = `<svg width="14" height="14" viewBox="0 0 12 24" fill="none" class="tm-forward-icon"><path fill-rule="evenodd" clip-rule="evenodd" d="M8.29289 4.29289C8.68342 3.90237 9.31658 3.90237 9.70711 4.29289L16.7071 11.2929C17.0976 11.6834 17.0976 12.3166 16.7071 12.7071L9.70711 19.7071C9.31658 20.0976 8.68342 20.0976 8.29289 19.7071C7.90237 19.3166 7.90237 18.6834 8.29289 18.2929L14.5858 12L8.29289 5.70711C7.90237 5.31658 7.90237 4.68342 8.29289 4.29289Z" fill="currentColor"/></svg>`;

  function el(tag, className, html) {
    const node = document.createElement(tag);
    if (className) node.className = className;
    if (html != null) node.innerHTML = html;
    return node;
  }

  class TmPlayerControls {
    constructor(options) {
      this.host = options.host;
      this.videoWrapper = options.videoWrapper || options.host.parentElement;
      this.getVideo = options.getVideo || (() => null);
      this.hideDelay = options.hideDelay || 3200;
      this.onUserInteract = options.onUserInteract || null;
      this.onToggleFullscreen = options.onToggleFullscreen || null;
      this.fullscreenActive = false;

      this.root = null;
      this.playPauseButton = null;
      this.progressBarElement = null;
      this.progressIndicator = null;
      this.currentTimeDisplay = null;
      this.totalDurationDisplay = null;
      this.volumeLevel = null;
      this.volumeValue = null;
      this.volumeSlider = null;
      this.loopStartMarker = null;
      this.loopEndMarker = null;
      this.loopRangeElement = null;
      this.currentPositionDisplay = null;
      this.durationDisplay = null;
      this.loopToggleButton = null;
      this.updatePlaybackRateSlider = null;

      this.loopStartTime = null;
      this.loopEndTime = null;
      this.loopActive = false;
      this.isDraggingProgress = false;
      this.lastVolume = 1;
      this.supportsVolumeControl = !(/iPad|iPhone|iPod/.test(navigator.userAgent) && !window.MSStream);
      this.controlsVisible = true;
      this.hideTimer = null;
      this._pointerOverHost = false;
      this._bound = [];
      this._progressMove = null;
      this._progressUp = null;
      this.lastDragX = 0;
    }

    get video() {
      return this.getVideo();
    }

    init() {
      if (!this.host) return this;
      this.host.innerHTML = '';
      this.root = el('div', 'tm-control-buttons');
      this.root.appendChild(this._buildProgressRow());
      this.root.appendChild(this._buildSeekRow());
      this.root.appendChild(this._buildLoopRow());
      this.root.appendChild(this._buildPlaybackRow());
      this.host.appendChild(this.root);
      this.host.classList.add('tm-control-host');
      this._bindVideoEvents();
      this._bindHostInteraction();
      this.updatePlayPauseButton();
      this.updateVolumeUI();
      this.updateProgressBar();
      this.show();
      return this;
    }

    destroy() {
      this._bound.forEach(({ t, type, fn, opts }) => t.removeEventListener(type, fn, opts));
      this._bound = [];
      this._removeProgressListeners();
      clearTimeout(this.hideTimer);
      if (this.host) this.host.innerHTML = '';
    }

    setVideo() {
      this._bindVideoEvents();
      this.updatePlayPauseButton();
      this.updateVolumeUI();
      this.updateProgressBar();
      this.updateLoopMarkers();
    }

    _on(t, type, fn, opts) {
      if (!t) return;
      t.addEventListener(type, fn, opts);
      this._bound.push({ t, type, fn, opts });
    }

    _bindVideoEvents() {
      // Drop previous video-only listeners by rebinding via getVideo each time using named wrappers
      const v = this.video;
      if (!v || v === this._lastVideo) return;
      this._lastVideo = v;

      const onTime = () => {
        this.updateProgressBar();
        this._checkLoop();
      };
      const onMeta = () => {
        this.updateProgressBar();
        this.updateLoopMarkers();
      };
      const onPlay = () => {
        this.updatePlayPauseButton();
        this._showCenterFlash('play');
        this._scheduleAutoHide();
      };
      const onPause = () => {
        this.updatePlayPauseButton();
        this._showCenterFlash('pause');
        this._scheduleAutoHide();
      };
      const onVol = () => this.updateVolumeUI();
      const onRate = () => {
        const rate = v.playbackRate || 1;
        if (this.updatePlaybackRateSlider) {
          const pct = (rate - 0.1) / (3 - 0.1) * 100;
          this.updatePlaybackRateSlider(pct, true);
        }
        this._showRateIndicator(rate);
      };

      this._on(v, 'timeupdate', onTime);
      this._on(v, 'loadedmetadata', onMeta);
      this._on(v, 'durationchange', onMeta);
      this._on(v, 'play', onPlay);
      this._on(v, 'pause', onPause);
      this._on(v, 'volumechange', onVol);
      this._on(v, 'ratechange', onRate);
    }

    _bindHostInteraction() {
      // 监听整个播放区：面板收起后只剩底部进度条可点，绑在 host 上就唤不出面板了。
      const surface = this.videoWrapper || this.host;
      const bump = () => {
        this._scheduleAutoHide();
        if (typeof this.onUserInteract === 'function') this.onUserInteract();
      };
      this._on(surface, 'pointerdown', bump);
      this._on(surface, 'mousemove', bump);
      this._on(this.host, 'pointerenter', () => {
        this._pointerOverHost = true;
        this._scheduleAutoHide();
      });
      this._on(this.host, 'pointerleave', () => {
        this._pointerOverHost = false;
        this._scheduleAutoHide();
      });
      this._on(this.root, 'click', (e) => e.stopPropagation());
    }

    /** 播放中且指针不在面板上时才自动收起；暂停或指针停在面板上时常驻。 */
    _scheduleAutoHide() {
      const v = this.video;
      const idle = !this._pointerOverHost && !!v && !v.paused;
      this.show({ autoHide: idle ? 'self' : false });
    }

    show(options = {}) {
      const { autoHide = true } = options;
      this.controlsVisible = true;
      this.controlsMini = false;
      if (this.host) {
        this.host.classList.remove('controls-hidden', 'controls-mini');
        this.host.classList.add('controls-visible');
      }
      clearTimeout(this.hideTimer);
      this.hideTimer = null;
      if (autoHide === 'self') {
        const v = this.video;
        if (v && !v.paused) {
          this.hideTimer = setTimeout(() => this.hide(), this.hideDelay);
        }
      }
    }

    /** 收起为仅底部进度条（不是完全消失） */
    hide() {
      if (this.isDraggingProgress) {
        this.show({ autoHide: false });
        return;
      }
      this.controlsVisible = false;
      this.controlsMini = true;
      clearTimeout(this.hideTimer);
      this.hideTimer = null;
      if (this.host) {
        this.host.classList.remove('controls-hidden');
        this.host.classList.remove('controls-visible');
        this.host.classList.add('controls-mini');
      }
    }

    toggle() {
      if (this.controlsVisible || (this.host && this.host.classList.contains('controls-visible'))) {
        this.hide();
      } else {
        this.show({ autoHide: false });
      }
    }

    setMenuOpacity(opacity01) {
      const a = Math.max(0.08, Math.min(0.92, Number(opacity01) || 0.4));
      if (this.host) this.host.style.setProperty('--tm-menu-opacity', String(a));
      document.documentElement.style.setProperty('--tm-menu-opacity', String(a));
    }

    formatTime(sec) {
      if (!Number.isFinite(sec) || sec < 0) return '00:00:00';
      const s = Math.floor(sec);
      const h = Math.floor(s / 3600);
      const m = Math.floor((s % 3600) / 60);
      const r = s % 60;
      return `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}:${String(r).padStart(2, '0')}`;
    }

    seekRelative(delta) {
      const v = this.video;
      if (!v) return;
      const cur = Number(v.currentTime) || 0;
      let next = cur + Number(delta || 0);
      const dur = Number(v.duration);
      if (Number.isFinite(dur) && dur > 0) {
        next = Math.max(0, Math.min(dur, next));
      } else {
        next = Math.max(0, next);
      }
      try {
        v.currentTime = next;
      } catch (_) {
        return;
      }
      this.updateProgressBar();
      this.show({ autoHide: false });
      if (typeof this.onUserInteract === 'function') this.onUserInteract();
    }

    _buildProgressRow() {
      const row = el('div', 'tm-progress-row');
      const box = el('div', 'tm-progress-controls');
      const times = el('div', 'tm-time-display-container');
      this.currentTimeDisplay = el('span', 'tm-current-time');
      this.currentTimeDisplay.textContent = '00:00:00';
      this.totalDurationDisplay = el('span', 'tm-total-duration');
      this.totalDurationDisplay.textContent = '-00:00:00';
      times.append(this.currentTimeDisplay, this.totalDurationDisplay);

      const barWrap = el('div', 'tm-progress-bar-container');
      this.progressBarElement = el('div', 'tm-progress-bar tm-progress-bar-normal');
      this.progressIndicator = el('div', 'tm-progress-indicator');
      this.progressBarElement.appendChild(this.progressIndicator);

      this.loopStartMarker = el('div', 'tm-loop-marker tm-loop-start-marker');
      this.loopStartMarker.style.display = 'none';
      this.loopEndMarker = el('div', 'tm-loop-marker tm-loop-end-marker');
      this.loopEndMarker.style.display = 'none';
      this.loopRangeElement = el('div', 'tm-loop-range');
      this.loopRangeElement.style.display = 'none';

      barWrap.append(this.progressBarElement, this.loopStartMarker, this.loopEndMarker, this.loopRangeElement);
      box.append(times, barWrap);
      row.appendChild(box);

      this._on(barWrap, 'mouseenter', () => {
        this.progressBarElement.classList.add('tm-progress-bar-expanded');
        this.progressBarElement.classList.remove('tm-progress-bar-normal');
      });
      this._on(barWrap, 'mouseleave', () => {
        if (!this.isDraggingProgress) {
          this.progressBarElement.classList.add('tm-progress-bar-normal');
          this.progressBarElement.classList.remove('tm-progress-bar-expanded');
        }
      });
      this._on(this.progressBarElement, 'click', (e) => this._handleProgressClick(e));
      this._on(barWrap, 'mousedown', (e) => this._startProgressDrag(e));
      this._on(barWrap, 'touchstart', (e) => this._startProgressDrag(e), { passive: false });
      return row;
    }

    _buildSeekRow() {
      const row = el('div', 'tm-seek-control-row');
      const rewind = el('div', 'tm-rewind-group');
      const forward = el('div', 'tm-forward-group');
      const rb = el('div', 'tm-rewind-buttons-container');
      const fb = el('div', 'tm-forward-buttons-container');
      rewind.appendChild(rb);
      forward.appendChild(fb);
      row.append(rewind, forward);

      [
        ['-5s', -5], ['-10s', -10], ['-30s', -30], ['-1m', -60], ['-5m', -300], ['-10m', -600]
      ].forEach(([label, sec]) => this._addTimeBtn(rb, label, () => this.seekRelative(sec)));
      [
        ['+5s', 5], ['+10s', 10], ['+30s', 30], ['+1m', 60], ['+5m', 300], ['+10m', 600]
      ].forEach(([label, sec]) => this._addTimeBtn(fb, label, () => this.seekRelative(sec)));
      return row;
    }

    _buildLoopRow() {
      const row = el('div', 'tm-loop-control-row');
      const timeBox = el('div', 'tm-time-display');
      const loopBox = el('div', 'tm-loop-control');

      const start = el('div', 'tm-start-time-container');
      const end = el('div', 'tm-end-time-container');
      start.append(el('span', 'tm-set-loop-start-label', 'A'));
      this.currentPositionDisplay = el('span', 'tm-loop-start-position');
      this.currentPositionDisplay.textContent = '00:00:00';
      start.appendChild(this.currentPositionDisplay);

      end.append(el('span', 'tm-set-loop-end-label', 'B'));
      this.durationDisplay = el('span', 'tm-loop-end-position');
      this.durationDisplay.textContent = '00:00:00';
      end.appendChild(this.durationDisplay);

      this._on(start, 'click', () => this._setLoopStart());
      this._on(end, 'click', () => this._setLoopEnd());

      this.loopToggleButton = el('div', 'tm-loop-toggle-button');
      this.loopToggleButton.innerHTML = `<span class="tm-loop-toggle-label">Loop</span>
        <svg width="12" height="12" style="vertical-align:middle"><circle class="tm-loop-indicator-circle" cx="6" cy="6" r="5" fill="hsl(var(--shadcn-muted-foreground) / 0.5)"></circle></svg>`;
      this._on(this.loopToggleButton, 'click', () => this._toggleLoop());
      loopBox.appendChild(this.loopToggleButton);
      timeBox.append(start, end);
      row.append(timeBox, loopBox);
      return row;
    }

    _buildPlaybackRow() {
      const row = el('div', 'tm-playback-control-row');
      const left = el('div', 'tm-left-controls');
      left.style.cssText = 'display:flex;align-items:center;gap:6px;flex:1';
      const center = el('div', 'tm-center-controls');
      center.style.cssText = 'display:flex;align-items:center;justify-content:center;flex:1';
      const right = el('div', 'tm-right-controls');
      right.style.cssText = 'display:flex;align-items:center;justify-content:flex-end;flex:1;gap:6px';

      this._createVolume(left);
      this.playPauseButton = el('button', 'tm-control-button');
      this.playPauseButton.type = 'button';
      this.playPauseButton.innerHTML = PLAY_SVG;
      this._on(this.playPauseButton, 'click', () => this.togglePlayPause());
      center.appendChild(this.playPauseButton);
      this._createWindowControls(right);
      this._createSpeed(right);

      row.append(left, center, right);
      return row;
    }

    _createWindowControls(parent) {
      const wrap = el('div', 'tm-window-controls');

      // 进入全屏：四角向外
      const ICON_FS_ENTER = `<svg class="tm-win-icon" viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M8 3H3v5M16 3h5v5M8 21H3v-5M21 16v5h-5" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/></svg>`;
      // 退出全屏：四角向内（标准 compress）
      const ICON_FS_EXIT = `<svg class="tm-win-icon" viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M8 3v5H3M16 3v5h5M8 21v-5H3M21 16h-5v5" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/></svg>`;

      this._icons = { ICON_FS_ENTER, ICON_FS_EXIT };

      // 截动图(GIF):按时间段生成动图,懒加载模块 gif-capture.js
      const gifBtn = el('button', 'tm-control-button tm-window-btn');
      gifBtn.type = 'button';
      gifBtn.title = '截动图(GIF)';
      gifBtn.setAttribute('aria-label', '截动图(GIF)');
      gifBtn.innerHTML = `<svg class="tm-win-icon" viewBox="0 0 24 24" fill="none" aria-hidden="true"><rect x="3" y="5" width="18" height="14" rx="2.2" stroke="currentColor" stroke-width="1.8"/><path d="M7.5 14.5v-5h2.2M12.2 14.5v-5h.4c1.6 0 2.6 1.1 2.6 2.5s-1 2.5-2.6 2.5h-.4Z" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/></svg>`;
      this._on(gifBtn, 'click', (e) => {
        e.stopPropagation();
        if (typeof this.onCaptureGif === 'function') this.onCaptureGif();
        else if (typeof window.openGifCapture === 'function') window.openGifCapture();
        this.show({ autoHide: false });
        if (typeof this.onUserInteract === 'function') this.onUserInteract();
      });

      const fsBtn = el('button', 'tm-control-button tm-window-btn');
      fsBtn.type = 'button';
      fsBtn.innerHTML = ICON_FS_ENTER;

      this.fullscreenButton = fsBtn;

      this._on(fsBtn, 'click', (e) => {
        e.stopPropagation();
        // The icon follows the authoritative state pushed by the page/kernel.
        if (typeof this.onToggleFullscreen === 'function') this.onToggleFullscreen();
        this.show({ autoHide: false });
        if (typeof this.onUserInteract === 'function') this.onUserInteract();
      });

      wrap.append(gifBtn, fsBtn);
      parent.appendChild(wrap);
      this.updateWindowButtons();
    }

    _isFullscreen() {
      return !!this.fullscreenActive;
    }

    setFullscreen(active) {
      this.fullscreenActive = !!active;
      this.updateWindowButtons();
    }

    updateWindowButtons() {
      const fullscreen = this._isFullscreen();
      const ic = this._icons || {};

      if (this.fullscreenButton) {
        this.fullscreenButton.classList.toggle('is-active', fullscreen);
        this.fullscreenButton.classList.toggle('is-fullscreen', fullscreen);
        this.fullscreenButton.title = fullscreen ? '退出全屏' : '全屏';
        this.fullscreenButton.setAttribute('aria-label', fullscreen ? '退出全屏' : '全屏');
        this.fullscreenButton.innerHTML = fullscreen ? ic.ICON_FS_EXIT : ic.ICON_FS_ENTER;
      }
    }

    _addTimeBtn(parent, label, onClick) {
      const opacityOf = (text) => {
        const n = parseInt(text.replace(/[+-]/g, ''), 10);
        const unit = text.includes('m') ? 'm' : 's';
        if (unit === 's') return n <= 5 ? 0.5 : n <= 10 ? 0.6 : 0.7;
        return n === 1 ? 0.8 : n === 5 ? 0.9 : 1;
      };
      const btn = el('button', 'tm-time-control-button');
      btn.type = 'button';
      btn.style.backgroundColor = `hsl(var(--shadcn-secondary) / ${opacityOf(label)})`;
      const body = label.replace(/[+-]/g, '');
      if (label.includes('-')) {
        btn.innerHTML = `<div class="tm-time-control-button-inner">${REWIND_SVG}<span class="tm-time-text-margin-left">${body}</span></div>`;
      } else {
        btn.innerHTML = `<div class="tm-time-control-button-inner"><span class="tm-time-text-margin-right">${body}</span>${FORWARD_SVG}</div>`;
      }
      this._on(btn, 'click', onClick);
      parent.appendChild(btn);
      return btn;
    }

    togglePlayPause() {
      const v = this.video;
      if (!v) return;
      if (v.paused) v.play().catch(() => {});
      else v.pause();
      this.updatePlayPauseButton();
      this.show();
    }

    updatePlayPauseButton() {
      if (!this.playPauseButton) return;
      const v = this.video;
      this.playPauseButton.innerHTML = v && !v.paused ? PAUSE_SVG : PLAY_SVG;
    }

    updateProgressBar() {
      const v = this.video;
      if (!v || !this.progressIndicator) return;
      const d = v.duration;
      if (!Number.isFinite(d) || d <= 0) return;
      if (!this.isDraggingProgress) {
        this.progressIndicator.style.width = `${(v.currentTime / d) * 100}%`;
      }
      if (this.currentTimeDisplay) this.currentTimeDisplay.textContent = this.formatTime(v.currentTime);
      if (this.totalDurationDisplay) this.totalDurationDisplay.textContent = `-${this.formatTime(Math.max(0, d - v.currentTime))}`;
    }

    _handleProgressClick(e) {
      if (this.isDraggingProgress) return;
      const v = this.video;
      if (!v || !Number.isFinite(v.duration)) return;
      const rect = this.progressBarElement.getBoundingClientRect();
      const ratio = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
      v.currentTime = v.duration * ratio;
      this.updateProgressBar();
      this.show();
    }

    _startProgressDrag(e) {
      const v = this.video;
      if (!v || !Number.isFinite(v.duration)) return;
      e.preventDefault();
      e.stopPropagation();
      this.isDraggingProgress = true;
      this.lastDragX = e.type.includes('touch') ? e.touches[0].clientX : e.clientX;
      this.progressBarElement.classList.add('tm-progress-bar-expanded', 'tm-dragging');
      this.progressBarElement.classList.remove('tm-progress-bar-normal');

      this._progressMove = (ev) => this._progressDragMove(ev);
      this._progressUp = (ev) => this._progressDragUp(ev);
      if (e.type.includes('touch')) {
        document.addEventListener('touchmove', this._progressMove, { passive: false });
        document.addEventListener('touchend', this._progressUp, { passive: false });
        document.addEventListener('touchcancel', this._progressUp, { passive: false });
      } else {
        document.addEventListener('mousemove', this._progressMove);
        document.addEventListener('mouseup', this._progressUp);
      }
      this._applyProgressX(this.lastDragX);
      this.show();
    }

    _progressDragMove(e) {
      if (!this.isDraggingProgress) return;
      e.preventDefault();
      const x = e.type.includes('touch') ? e.touches[0].clientX : e.clientX;
      this.lastDragX = x;
      this._applyProgressX(x);
    }

    _progressDragUp() {
      if (!this.isDraggingProgress) return;
      this.isDraggingProgress = false;
      this.progressBarElement.classList.remove('tm-dragging');
      this.progressBarElement.classList.add('tm-progress-bar-normal');
      this.progressBarElement.classList.remove('tm-progress-bar-expanded');
      this._removeProgressListeners();
      this.show();
    }

    _removeProgressListeners() {
      if (this._progressMove) {
        document.removeEventListener('mousemove', this._progressMove);
        document.removeEventListener('touchmove', this._progressMove);
      }
      if (this._progressUp) {
        document.removeEventListener('mouseup', this._progressUp);
        document.removeEventListener('touchend', this._progressUp);
        document.removeEventListener('touchcancel', this._progressUp);
      }
      this._progressMove = null;
      this._progressUp = null;
    }

    _applyProgressX(clientX) {
      const v = this.video;
      if (!v || !Number.isFinite(v.duration)) return;
      const rect = this.progressBarElement.getBoundingClientRect();
      if (rect.width <= 0) return;
      const ratio = Math.max(0, Math.min(1, (clientX - rect.left) / rect.width));
      v.currentTime = v.duration * ratio;
      this.progressIndicator.style.width = `${ratio * 100}%`;
      this.currentTimeDisplay.textContent = this.formatTime(v.currentTime);
      this.totalDurationDisplay.textContent = `-${this.formatTime(Math.max(0, v.duration - v.currentTime))}`;
    }

    _createVolume(parent) {
      const wrap = el('div', 'tm-volume-control');
      const btn = el('button', 'tm-volume-button');
      btn.type = 'button';
      btn.innerHTML = this._volumeIcon(1);
      const slider = el('div', 'tm-volume-slider-container');
      const track = el('div', 'tm-volume-slider-track');
      this.volumeLevel = el('div', 'tm-volume-slider-level');
      this.volumeLevel.style.width = '100%';
      this.volumeValue = el('div', 'tm-volume-value');
      this.volumeValue.textContent = '100%';
      track.appendChild(this.volumeLevel);
      slider.append(track, this.volumeValue);
      wrap.appendChild(btn);
      if (this.supportsVolumeControl) wrap.appendChild(slider);
      else wrap.classList.add('tm-volume-control-no-slider');
      this.volumeSlider = wrap;
      parent.appendChild(wrap);

      let dragging = false;
      let expanded = false;
      let collapseTimer = null;

      const setVol = (clientX) => {
        if (!this.supportsVolumeControl) return;
        const v = this.video;
        if (!v) return;
        const rect = track.getBoundingClientRect();
        const pct = Math.max(0, Math.min(100, ((clientX - rect.left) / rect.width) * 100));
        v.muted = false;
        v.volume = pct / 100;
        if (pct > 0) this.lastVolume = v.volume;
        this.updateVolumeUI();
      };
      const expand = () => {
        if (!this.supportsVolumeControl) return;
        clearTimeout(collapseTimer);
        wrap.classList.add('expanded');
        expanded = true;
      };
      const collapse = () => {
        if (!dragging) {
          wrap.classList.remove('expanded');
          expanded = false;
        }
      };

      this._on(btn, 'click', (e) => {
        e.stopPropagation();
        const v = this.video;
        if (!v) return;
        if (this.supportsVolumeControl && !expanded) {
          expand();
          collapseTimer = setTimeout(collapse, 3000);
          return;
        }
        if (v.muted || v.volume === 0) {
          v.muted = false;
          if (this.supportsVolumeControl) v.volume = this.lastVolume || 1;
        } else if (this.supportsVolumeControl) {
          this.lastVolume = v.volume;
          v.volume = 0;
        } else {
          v.muted = true;
        }
        this.updateVolumeUI();
        this.show();
      });

      if (this.supportsVolumeControl) {
        this._on(track, 'click', (e) => { e.stopPropagation(); setVol(e.clientX); });
        this._on(track, 'mousedown', (e) => {
          e.stopPropagation();
          dragging = true;
          wrap.classList.add('dragging');
          expand();
          setVol(e.clientX);
        });
        this._on(document, 'mousemove', (e) => {
          if (!dragging) return;
          e.preventDefault();
          setVol(e.clientX);
        });
        this._on(document, 'mouseup', () => {
          if (!dragging) return;
          dragging = false;
          wrap.classList.remove('dragging');
          collapseTimer = setTimeout(collapse, 1500);
        });
        this._on(track, 'touchstart', (e) => {
          e.stopPropagation();
          dragging = true;
          expand();
          setVol(e.touches[0].clientX);
        }, { passive: false });
        this._on(track, 'touchmove', (e) => {
          if (!dragging) return;
          e.preventDefault();
          setVol(e.touches[0].clientX);
        }, { passive: false });
        this._on(track, 'touchend', () => {
          dragging = false;
          collapseTimer = setTimeout(collapse, 1500);
        });
      }
    }

    _volumeIcon(vol) {
      const v = this.video;
      const muted = v ? (v.muted || vol === 0) : vol === 0;
      if (muted) {
        return `<svg width="24" height="24" viewBox="0 0 24 24" fill="none"><path d="M11 5L6 9H2V15H6L11 19V5Z" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/><path d="M23 9L17 15" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/><path d="M17 9L23 15" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`;
      }
      if (this.supportsVolumeControl && vol < 0.5) {
        return `<svg width="24" height="24" viewBox="0 0 24 24" fill="none"><path d="M11 5L6 9H2V15H6L11 19V5Z" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/><path d="M15.54 8.46C16.4774 9.39764 17.0039 10.6692 17.0039 12C17.0039 13.3308 16.4774 14.6024 15.54 15.54" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`;
      }
      return `<svg width="24" height="24" viewBox="0 0 24 24" fill="none"><path d="M11 5L6 9H2V15H6L11 19V5Z" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/><path d="M15.54 8.46C16.4774 9.39764 17.0039 10.6692 17.0039 12C17.0039 13.3308 16.4774 14.6024 15.54 15.54" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/><path d="M19.07 4.93C20.9447 6.80527 21.9979 9.34855 21.9979 12C21.9979 14.6515 20.9447 17.1947 19.07 19.07" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`;
    }

    updateVolumeUI() {
      if (!this.volumeSlider) return;
      const v = this.video;
      let vol = 1;
      if (v) {
        vol = this.supportsVolumeControl ? (v.muted ? 0 : v.volume) : (v.muted ? 0 : 1);
      }
      const btn = this.volumeSlider.querySelector('.tm-volume-button');
      if (btn) btn.innerHTML = this._volumeIcon(vol);
      if (!this.supportsVolumeControl) return;
      if (this.volumeLevel) this.volumeLevel.style.width = `calc(${Math.round(vol * 100)}% - 2px)`;
      if (this.volumeValue) this.volumeValue.textContent = `${Math.round(vol * 100)}%`;
    }

    _createSpeed(parent) {
      const min = 0.1, max = 3, step = 0.1;
      const wrap = el('div', 'tm-playback-rate-slider');
      const container = el('div', 'tm-slider-container');
      const level = el('div', 'tm-slider-level');
      const marks = el('div', 'tm-slider-marks');
      const text = el('div', 'tm-slider-text');
      const label = el('div', 'tm-speed-label');
      label.textContent = 'Speed';
      const value = el('div', 'tm-speed-value tm-speed-value-normal');
      value.textContent = '1.0x';
      text.append(label, value);

      const markDefs = [0.5, 1, 1.5, 2, 3].map((r) => ({
        pos: Math.round(((r - min) / (max - min)) * 100),
        label: `${r.toFixed(1)}x`
      }));
      markDefs.forEach((m) => {
        const mark = el('div', 'tm-slider-mark');
        mark.style.left = `${m.pos}%`;
        marks.appendChild(mark);
      });

      container.append(marks, level, text);
      wrap.appendChild(container);
      parent.appendChild(wrap);

      let dragging = false;
      let current = 1;

      const applyPct = (pct, silent) => {
        pct = Math.max(0, Math.min(100, pct));
        // snap
        for (const m of markDefs) {
          if (Math.abs(pct - m.pos) < 5) {
            pct = m.pos;
            break;
          }
        }
        level.style.width = `${pct}%`;
        let rate = min + (pct / 100) * (max - min);
        rate = Math.round(rate / step) * step;
        rate = Math.max(min, Math.min(max, rate));
        if (rate !== current) {
          current = rate;
          const v = this.video;
          if (v) v.playbackRate = rate;
          try { localStorage.setItem('playbackRate', String(rate)); } catch (_) {}
          value.textContent = `${rate.toFixed(1)}x`;
          value.classList.remove('tm-speed-value-fast', 'tm-speed-value-slow', 'tm-speed-value-normal');
          if (rate > 1.5) value.classList.add('tm-speed-value-fast');
          else if (rate < 0.8) value.classList.add('tm-speed-value-slow');
          else value.classList.add('tm-speed-value-normal');
          if (!silent) this._showRateIndicator(rate);
        } else {
          level.style.width = `${pct}%`;
        }
      };

      const fromEvent = (e) => {
        const x = e.type.includes('touch') ? e.touches[0].clientX : e.clientX;
        const rect = container.getBoundingClientRect();
        applyPct(((x - rect.left) / rect.width) * 100);
      };

      this._on(container, 'mousedown', (e) => {
        e.preventDefault();
        dragging = true;
        wrap.classList.add('dragging', 'tm-playback-slider-dragging');
        fromEvent(e);
        this.show();
      }, { passive: false });
      this._on(container, 'touchstart', (e) => {
        e.preventDefault();
        dragging = true;
        fromEvent(e);
        this.show();
      }, { passive: false });
      this._on(window, 'mousemove', (e) => {
        if (!dragging) return;
        e.preventDefault();
        fromEvent(e);
      }, { passive: false });
      this._on(window, 'touchmove', (e) => {
        if (!dragging) return;
        e.preventDefault();
        fromEvent(e);
      }, { passive: false });
      this._on(window, 'mouseup', () => {
        if (!dragging) return;
        dragging = false;
        wrap.classList.remove('dragging', 'tm-playback-slider-dragging');
      });
      this._on(window, 'touchend', () => {
        dragging = false;
        wrap.classList.remove('dragging', 'tm-playback-slider-dragging');
      });
      this._on(wrap, 'dblclick', () => applyPct(30));

      this.updatePlaybackRateSlider = (pct, silent) => applyPct(pct, silent);
      const saved = parseFloat(localStorage.getItem('playbackRate') || '1');
      applyPct(((saved - min) / (max - min)) * 100, true);
    }

    _setLoopStart() {
      const v = this.video;
      if (!v) return;
      this.loopStartTime = v.currentTime;
      if (this.loopEndTime != null && this.loopStartTime > this.loopEndTime) {
        const t = this.loopStartTime;
        this.loopStartTime = this.loopEndTime;
        this.loopEndTime = t;
      }
      if (this.currentPositionDisplay) {
        this.currentPositionDisplay.textContent = this.formatTime(this.loopStartTime);
        this.currentPositionDisplay.classList.add('active');
      }
      const box = this.root.querySelector('.tm-start-time-container');
      if (box) box.classList.add('active');
      if (this.loopStartTime != null && this.loopEndTime != null) this.loopActive = true;
      this._syncLoopToggle();
      this.updateLoopMarkers();
      this.show();
    }

    _setLoopEnd() {
      const v = this.video;
      if (!v) return;
      this.loopEndTime = v.currentTime;
      if (this.loopStartTime != null && this.loopStartTime > this.loopEndTime) {
        const t = this.loopStartTime;
        this.loopStartTime = this.loopEndTime;
        this.loopEndTime = t;
      }
      if (this.durationDisplay) {
        this.durationDisplay.textContent = this.formatTime(this.loopEndTime);
        this.durationDisplay.classList.add('active');
      }
      const box = this.root.querySelector('.tm-end-time-container');
      if (box) box.classList.add('active');
      if (this.loopStartTime != null && this.loopEndTime != null) this.loopActive = true;
      this._syncLoopToggle();
      this.updateLoopMarkers();
      this.show();
    }

    _toggleLoop() {
      if (this.loopStartTime == null || this.loopEndTime == null) return;
      this.loopActive = !this.loopActive;
      this._syncLoopToggle();
      this.updateLoopMarkers();
      this.show();
    }

    _syncLoopToggle() {
      if (!this.loopToggleButton) return;
      const circle = this.loopToggleButton.querySelector('.tm-loop-indicator-circle');
      if (circle) {
        circle.setAttribute('fill', this.loopActive
          ? 'hsl(var(--shadcn-green))'
          : 'hsl(var(--shadcn-muted-foreground) / 0.5)');
      }
      this.loopToggleButton.classList.toggle('active', this.loopActive);
    }

    _checkLoop() {
      const v = this.video;
      if (!v || !this.loopActive || this.loopStartTime == null || this.loopEndTime == null) return;
      if (v.currentTime >= this.loopEndTime) v.currentTime = this.loopStartTime;
    }

    updateLoopMarkers() {
      const v = this.video;
      if (!v || !this.loopStartMarker || !Number.isFinite(v.duration) || v.duration <= 0) return;
      const place = (time, marker) => {
        if (time == null || !Number.isFinite(time)) {
          marker.style.display = 'none';
          return;
        }
        marker.style.left = `${(time / v.duration) * 100}%`;
        marker.style.display = 'block';
        marker.classList.toggle('active', this.loopActive);
      };
      place(this.loopStartTime, this.loopStartMarker);
      place(this.loopEndTime, this.loopEndMarker);
      if (this.loopActive && this.loopStartTime != null && this.loopEndTime != null) {
        const left = (this.loopStartTime / v.duration) * 100;
        const width = ((this.loopEndTime - this.loopStartTime) / v.duration) * 100;
        this.loopRangeElement.style.left = `${left}%`;
        this.loopRangeElement.style.width = `${Math.max(0, width)}%`;
        this.loopRangeElement.style.display = width > 0 ? 'block' : 'none';
        this.loopRangeElement.classList.add('active');
      } else if (this.loopRangeElement) {
        this.loopRangeElement.style.display = 'none';
        this.loopRangeElement.classList.remove('active');
      }
    }

    _showCenterFlash(kind) {
      if (!this.videoWrapper) return;
      // 清理旧指示器，避免叠多层
      this.videoWrapper.querySelectorAll('.tm-center-flash, .tm-pause-indicator').forEach((n) => n.remove());
      const isPlay = kind === 'play';
      const node = el('div', `tm-indicator-base tm-center-flash tm-pause-indicator ${isPlay ? 'is-play' : 'is-pause'}`);
      node.innerHTML = isPlay ? CENTER_PLAY_SVG : CENTER_PAUSE_SVG;
      this.videoWrapper.appendChild(node);
      // 强制 reflow 再加 visible，保证动画触发
      void node.offsetWidth;
      requestAnimationFrame(() => node.classList.add('visible'));
      clearTimeout(this._centerFlashTimer);
      this._centerFlashTimer = setTimeout(() => {
        node.classList.remove('visible');
        setTimeout(() => node.remove(), 220);
      }, 650);
    }

    _showPauseIndicator() {
      this._showCenterFlash('pause');
    }

    _showRateIndicator(rate) {
      if (!this.videoWrapper) return;
      const old = this.videoWrapper.querySelector('.tm-playback-rate-indicator');
      if (old) old.remove();
      const node = el('div', 'tm-indicator-base tm-playback-rate-indicator');
      node.textContent = `${Number(rate).toFixed(1)}x`;
      if (rate > 1.5) node.style.color = 'hsl(var(--shadcn-orange))';
      else if (rate < 0.8) node.style.color = 'hsl(var(--shadcn-blue))';
      this.videoWrapper.appendChild(node);
      void node.offsetWidth;
      requestAnimationFrame(() => node.classList.add('visible'));
      setTimeout(() => {
        node.classList.remove('visible');
        setTimeout(() => node.remove(), 220);
      }, 1200);
    }
  }

  global.TmPlayerControls = TmPlayerControls;
})(typeof window !== 'undefined' ? window : globalThis);