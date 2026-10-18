/*!
 * gif-capture.js — 视频 GIF 截动图(时间段 -> 动图)
 * 自包含模块:不依赖播放器内部变量,直接使用当前可见的 <video>
 * 在页面引入:gifenc.js 之后引入本文件,控制条点击按钮时调用 window.openGifCapture()
 */
(function () {
  'use strict';

  var LS_KEY = 'player.gifCapture.settings';
  var DEFAULTS = { fps: 10, scale: 75, loop: 'infinite' };   // scale 为百分比
  var MAX_SECONDS = 30;      // 单段上限
  var MAX_FRAMES = 300;      // 总帧数上限
  var ABORT_SECONDS = 90;    // 采集兜底超时

  var panel = null, enc = null, canvas = null, ctx = null;
  var capturing = false, cancelled = false, rafId = 0;
  var restoreState = null;
  var lastDlUrl = null;   // 上一张下载用 blob URL(生成新图时回收,防泄漏)

  /* ---------------- 工具 ---------------- */

  function $(id) { return document.getElementById(id); }

  function activeVideo() {
    var candidates = [];
    var xg = document.querySelector('#xgplayerContainer video');
    if (xg) candidates.push(xg);
    var native = document.getElementById('videoPlayer');
    if (native) candidates.push(native);
    for (var i = 0; i < candidates.length; i++) {
      var v = candidates[i];
      if (!v) continue;
      var visible = v.offsetParent !== null;                    // display:none 不可见
      var hasMedia = !!v.currentSrc || !!v.srcObject || v.readyState >= 1;
      if (visible && hasMedia) return v;
    }
    for (i = 0; i < candidates.length; i++) {                   // 都不可见时退而求其次
      if (candidates[i] && (candidates[i].currentSrc || candidates[i].srcObject)) return candidates[i];
    }
    return document.getElementById('videoPlayer') || null;
  }

  function fmt(s) {
    if (!isFinite(s) || s < 0) s = 0;
    var h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), sec = s % 60;
    var secStr = (sec < 10 ? '0' : '') + sec.toFixed(3);
    return (h < 10 ? '0' : '') + h + ':' + (m < 10 ? '0' : '') + m + ':' + secStr;
  }

  function parseTime(str) {
    if (str == null) return NaN;
    str = String(str).trim();
    if (!str) return NaN;
    if (/^\d+(\.\d+)?$/.test(str)) return parseFloat(str);      // 纯秒
    var parts = str.split(':');
    var last = parts.pop();
    var ms = 0;
    var lsp = last.split(/[.,]/);
    var s = parseFloat(lsp[0] || '0');
    if (lsp[1] != null) ms = parseFloat('0.' + lsp[1]) * 1000;
    var m = parts.length ? parseInt(parts.pop(), 10) : 0;
    var h = parts.length ? parseInt(parts.pop(), 10) : 0;
    if (isNaN(h) || isNaN(m) || isNaN(s)) return NaN;
    return h * 3600 + m * 60 + s + ms / 1000;
  }

  function baseName(url) {
    if (!url) return '动图';
    var n = String(url).split(/[?#]/)[0].split('/').pop();
    n = n.replace(/\.[^.]+$/, '');
    return n || '动图';
  }

  function loadSettings() {
    try { return Object.assign({}, DEFAULTS, JSON.parse(localStorage.getItem(LS_KEY) || '{}')); }
    catch (e) { return Object.assign({}, DEFAULTS); }
  }
  function saveSettings(s) {
    try { localStorage.setItem(LS_KEY, JSON.stringify(s)); } catch (e) {}
  }

  /* ---------------- 面板 ---------------- */

  function ensureStyle() {
    if (document.getElementById('gcStyle')) return;
    var st = document.createElement('style');
    st.id = 'gcStyle';
    /* 与原控制条(tm-player-controls)同一套令牌:--tm-* */
    st.textContent = [
      '#gcPanel{position:fixed;left:50%;bottom:104px;transform:translateX(-50%);z-index:2147483000;',
      'width:420px;max-width:calc(100vw - 24px);padding:14px 16px;border-radius:12px;',
      'background:hsla(var(--tm-panel) / max(var(--tm-menu-opacity), 0.78));',
      'border:1px solid hsla(var(--tm-line) / var(--tm-line-a));color:hsl(var(--tm-fg));',
      'font:13px/1.6 "SF Pro Display","SF Pro Text","Segoe UI","PingFang SC","Microsoft YaHei",sans-serif;',
      '-webkit-font-smoothing:antialiased;',
      'backdrop-filter:blur(18px) saturate(160%);-webkit-backdrop-filter:blur(18px) saturate(160%);',
      'box-shadow:0 8px 28px rgba(0,0,0,.35), inset 0 1px 0 hsla(var(--tm-line) / 0.06);',
      'color-scheme:dark;',   /* 原生下拉列表/控件按深色渲染,避免白底黑字 */
      'display:none}',
      '#gcPanel .gc-title{font-size:14px;font-weight:700;margin-bottom:12px;display:flex;justify-content:space-between;align-items:center}',
      '#gcPanel .gc-title-actions{display:flex;align-items:center;gap:2px}',
      '#gcPanel .gc-tip{position:relative;display:inline-flex;align-items:center}',
      '#gcPanel .gc-tip-trigger{width:24px;height:24px;padding:0;border:0;border-radius:var(--tm-r-sm);background:transparent;color:hsl(var(--tm-muted));font-size:12px;font-weight:700;line-height:1;cursor:help}',
      '#gcPanel .gc-tip-trigger:hover,#gcPanel .gc-tip-trigger:focus-visible{background:hsla(var(--tm-fill) / var(--tm-fill-h));color:hsl(var(--tm-fg));outline:none}',
      '#gcPanel .gc-tip-content{position:absolute;top:calc(100% + 8px);right:0;z-index:70;display:none;width:250px;padding:9px 11px;border:1px solid hsla(var(--tm-line) / var(--tm-line-a));border-radius:var(--tm-r-sm);background:hsla(var(--tm-panel) / .98);box-shadow:0 8px 28px rgba(0,0,0,.4);color:hsl(var(--tm-muted));font-size:11.5px;font-weight:400;line-height:1.6;text-align:left;pointer-events:none}',
      '#gcPanel .gc-tip:hover .gc-tip-content,#gcPanel .gc-tip:focus-within .gc-tip-content{display:block}',
      '#gcPanel .gc-close{background:none;border:none;color:hsl(var(--tm-muted));font-size:15px;cursor:pointer;padding:2px 6px;border-radius:var(--tm-r-sm);transition:background var(--tm-ease),color var(--tm-ease)}',
      '#gcPanel .gc-close:hover{background:hsla(var(--tm-fill) / var(--tm-fill-a));color:hsl(var(--tm-fg))}',
      '#gcPanel .gc-row{display:flex;align-items:center;gap:8px;margin-bottom:10px}',
      '#gcPanel .gc-row label{width:56px;flex:none;color:hsl(var(--tm-muted));font-size:12px}',
      '#gcPanel input[type=text]{flex:1;background:hsla(var(--tm-fill) / var(--tm-fill-a));border:1px solid hsla(var(--tm-line) / var(--tm-line-a));',
      'color:hsl(var(--tm-fg));border-radius:var(--tm-r-sm);padding:5px 9px;font-size:12.5px;font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;outline:none;transition:border-color var(--tm-ease),box-shadow var(--tm-ease)}',
      '#gcPanel input[type=text]:focus{border-color:hsl(var(--tm-accent));box-shadow:0 0 0 2px hsla(var(--tm-accent) / .25)}',
      '#gcPanel select{background:hsla(var(--tm-fill) / var(--tm-fill-a));border:1px solid hsla(var(--tm-line) / var(--tm-line-a));color:hsl(var(--tm-fg));',
      'border-radius:var(--tm-r-sm);padding:5px 8px;font-size:12.5px;outline:none;transition:border-color var(--tm-ease)}',
      '#gcPanel select:focus{border-color:hsl(var(--tm-accent))}',
      '#gcPanel .gc-btn{background:hsla(var(--tm-fill) / var(--tm-fill-a));border:1px solid hsla(var(--tm-line) / var(--tm-line-a));color:hsl(var(--tm-fg));',
      'border-radius:var(--tm-r-sm);padding:6px 14px;font-size:12.5px;font-weight:600;cursor:pointer;transition:background var(--tm-ease),transform var(--tm-press)}',
      '#gcPanel .gc-btn:hover{background:hsla(var(--tm-fill) / var(--tm-fill-h))}',
      '#gcPanel .gc-btn:active{transform:scale(.96)}',
      '#gcPanel .gc-btn.primary{background:hsl(var(--tm-accent));border-color:transparent;color:#fff;font-weight:700}',
      '#gcPanel .gc-btn.primary:hover{filter:brightness(1.12)}',
      '#gcPanel .gc-btn:disabled{opacity:.45;cursor:not-allowed}',
      '#gcPanel .gc-now{flex:none;font-size:11.5px;color:hsl(var(--tm-muted));background:hsla(var(--tm-fill) / var(--tm-fill-a));border:1px dashed hsla(var(--tm-line) / .35);',
      'border-radius:var(--tm-r-sm);padding:4px 9px;cursor:pointer;white-space:nowrap;transition:color var(--tm-ease),border-color var(--tm-ease),background var(--tm-ease)}',
      '#gcPanel .gc-now:hover{color:hsl(var(--tm-fg));border-color:hsl(var(--tm-accent));background:hsla(var(--tm-fill) / var(--tm-fill-h))}',
      /* 尺寸滑块:自由调节,等比缩放 */
      '#gcScale{flex:1;accent-color:hsl(var(--tm-accent));cursor:pointer;height:18px}',
      '#gcScaleVal{width:44px;text-align:right;font-size:12px;color:hsl(var(--tm-fg));font-variant-numeric:tabular-nums}',
      '#gcPxHint{flex:none;font-size:11px;color:hsl(var(--tm-muted));font-family:ui-monospace,Consolas,monospace;white-space:nowrap}',
      /* 自定义下拉(替代原生 select,避免浏览器弹层白底/样式不一致) */
      '.gc-select{position:relative;flex:1;min-width:0}',
      '.gc-select-trigger{width:100%;display:flex;align-items:center;justify-content:space-between;gap:8px;',
      'background:hsla(var(--tm-fill) / var(--tm-fill-a));border:1px solid hsla(var(--tm-line) / var(--tm-line-a));',
      'color:hsl(var(--tm-fg));border-radius:var(--tm-r-sm);padding:5px 10px;font-size:12.5px;font-family:inherit;',
      'cursor:pointer;transition:background var(--tm-ease),border-color var(--tm-ease)}',
      '.gc-select-trigger:hover{background:hsla(var(--tm-fill) / var(--tm-fill-h))}',
      '.gc-select-trigger .gc-caret{font-size:9px;opacity:.65;transition:transform .2s cubic-bezier(.16,1,.3,1)}',
      '.gc-select.open .gc-caret{transform:rotate(180deg)}',
      '.gc-select-menu{position:absolute;top:calc(100% + 4px);left:0;min-width:100%;z-index:60;display:none;padding:3px;',
      'background:hsla(var(--tm-panel) / .97);border:1px solid hsla(var(--tm-line) / var(--tm-line-a));border-radius:var(--tm-r-sm);',
      'box-shadow:0 8px 28px rgba(0,0,0,.4);color-scheme:dark}',
      '.gc-select.open .gc-select-menu{display:block}',
      '.gc-option{padding:5px 10px;border-radius:var(--tm-r-sm);font-size:12.5px;color:hsl(var(--tm-muted));cursor:pointer;',
      'transition:background var(--tm-ease),color var(--tm-ease)}',
      '.gc-option:hover{background:hsla(var(--tm-fill) / var(--tm-fill-h));color:hsl(var(--tm-fg))}',
      '.gc-option.active{color:hsl(var(--tm-accent));background:hsla(var(--tm-accent) / .14)}',
      '#gcMsg{min-height:18px;font-size:12px;color:hsl(var(--tm-muted));margin-bottom:10px;transition:color var(--tm-ease)}',
      '#gcMsg.err{color:hsl(var(--tm-red))}',
      '#gcMsg.ok{color:hsl(var(--tm-green))}',
      '#gcBarWrap{height:5px;background:hsla(var(--tm-fill) / var(--tm-fill-a));border-radius:4px;overflow:hidden;margin:2px 0 8px;display:none}',
      '#gcBar{height:100%;width:0;background:linear-gradient(90deg,hsl(var(--tm-accent)),hsl(var(--tm-blue)));border-radius:4px;transition:width .15s}',
      '#gcProgress{font-size:11.5px;color:hsl(var(--tm-muted));margin-bottom:10px;display:none;font-variant-numeric:tabular-nums}',
      '#gcActions{display:flex;gap:8px;justify-content:flex-end}',
      '#gcResult{display:none;margin-top:12px;text-align:center}',
      '#gcResult img{max-width:100%;max-height:220px;border-radius:var(--tm-r);background:#000;border:1px solid hsla(var(--tm-line) / var(--tm-line-a))}',
      '#gcResult .gc-meta{font-size:11.5px;color:hsl(var(--tm-muted));margin-top:6px}',
      '#gcDownload{display:inline-block;margin-top:8px;text-decoration:none;background:hsl(var(--tm-accent));color:#fff;',
      'border-radius:var(--tm-r-sm);padding:7px 18px;font-size:12.5px;font-weight:700;transition:filter var(--tm-ease)}',
      '#gcDownload:hover{filter:brightness(1.12)}',
      '#gcPanel .gc-hint{font-size:11px;color:hsl(var(--tm-muted));opacity:.85;margin-top:10px;line-height:1.5}'
    ].join('\n');
    (document.head || document.documentElement).appendChild(st);
  }

  function ensurePanel() {
    if (panel) return panel;
    ensureStyle();
    panel = document.createElement('div');
    panel.id = 'gcPanel';
    panel.innerHTML = [
      '<div class="gc-title">截动图 GIF<span class="gc-title-actions"><span class="gc-tip"><button class="gc-tip-trigger" type="button" aria-label="查看 GIF 操作说明" aria-describedby="gcTipContent">i</button><span class="gc-tip-content" id="gcTipContent" role="tooltip">快捷键：G 打开/关闭面板；Q 设置开始时间；W 设置结束时间；Enter 开始生成；Esc 取消或关闭。拖动标题栏可移动面板，双击标题栏复位。</span></span><button class="gc-close" id="gcClose" type="button" title="关闭">✕</button></span></div>',
      '<div class="gc-row"><label>开始</label><input type="text" id="gcStart" placeholder="00:00:00.000 或 秒"><button class="gc-now" id="gcNowStart" type="button">取当前时间</button></div>',
      '<div class="gc-row"><label>结束</label><input type="text" id="gcEnd" placeholder="00:00:00.000 或 秒"><button class="gc-now" id="gcNowEnd" type="button">取当前时间</button></div>',
      '<div class="gc-row"><label>帧率</label><div class="gc-select" id="gcFpsSel"></div>',
      '<label style="width:44px">循环</label><div class="gc-select" id="gcLoopSel"></div></div>',
      '<div class="gc-row"><label>尺寸</label><input type="range" id="gcScale" min="10" max="100" step="5" value="75">',
      '<span id="gcScaleVal">75%</span><span id="gcPxHint"></span></div>',
      '<div id="gcMsg">设定时间段,或直接「取当前时间」</div>',
      '<div id="gcBarWrap"><div id="gcBar"></div></div>',
      '<div id="gcProgress">准备中…</div>',
      '<div id="gcResult"><img id="gcPreview" alt="预览"><div class="gc-meta" id="gcMeta"></div><br><a id="gcDownload" download>下载 GIF</a></div>',
      '<div class="gc-hint">说明:采样区间上限 ' + MAX_SECONDS + ' 秒 / ' + MAX_FRAMES + ' 帧;跨域且无 CORS 的视频源无法截取画面;GIF 无声音。</div>',
      '<div id="gcActions"><button class="gc-btn" id="gcCancel" type="button">取消</button><button class="gc-btn primary" id="gcGenerate" type="button">开始生成</button></div>'
    ].join('');
    document.body.appendChild(panel);
    bindKeyboardShield();   // 移动端虚拟键盘遮挡适配
    makePanelDraggable(panel); // PC:按住标题栏拖动面板(GIF 结果挡菜单时可挪开)

    /* 自定义下拉控件(替代原生 select,弹层深色一致) */
    buildSelect('gcFpsSel', [[5, '5'], [10, '10'], [15, '15'], [20, '20']], '10');
    buildSelect('gcLoopSel', [['infinite', '无限'], ['once', '一次']], 'infinite');
    panel.addEventListener('click', function (e) {
      if (!e.target.closest('.gc-select')) closeOtherSelects(null);   // 点面板其他区域收起下拉
    });

    $(('gcClose')).addEventListener('click', closePanel);
    $(('gcCancel')).addEventListener('click', cancelCapture);
    $(('gcGenerate')).addEventListener('click', onGenerate);
    $(('gcScale')).addEventListener('input', function () { updateScaleUI(activeVideo()); });
    $(('gcNowStart')).addEventListener('click', function () {
      var v = activeVideo();
      if (v && isFinite(v.currentTime)) $(('gcStart')).value = fmt(v.currentTime);
    });
    $(('gcNowEnd')).addEventListener('click', function () {
      var v = activeVideo();
      if (v && isFinite(v.currentTime)) $(('gcEnd')).value = fmt(v.currentTime);
    });
    panel.addEventListener('mousedown', function (e) { e.stopPropagation(); });
    panel.addEventListener('click', function (e) { e.stopPropagation(); });
    return panel;
  }

  function setMsg(text, kind) {
    var m = $('gcMsg');
    m.textContent = text || '';
    m.className = kind || '';
  }

  /* ---------------- 面板拖动(PC) ----------------
   * GIF 结果挡住播放器菜单时,按住标题栏即可拖走;双击标题复位。
   * 仅 hover 设备启用(触屏拖动与页面滚动冲突) */
  function makePanelDraggable(panel) {
    var bar = panel.querySelector('.gc-title');
    if (!bar || bar.dataset.draggable) return;
    bar.dataset.draggable = '1';
    bar.style.cursor = 'move';
    bar.style.userSelect = 'none';
    bar.title = '拖动移动面板 · 双击复位';
    var dragging = false, startX = 0, startY = 0, baseLeft = 0, baseTop = 0;

    function clamp(v, lo, hi) { return Math.min(Math.max(v, lo), hi); }
    function toAbsolute() { // 初始 bottom+translateX 居中定位 → left/top 绝对定位
      var r = panel.getBoundingClientRect();
      panel.style.transform = 'none';
      panel.style.left = r.left + 'px';
      panel.style.top = r.top + 'px';
      panel.style.bottom = 'auto';
      panel.style.right = 'auto';
      return panel.getBoundingClientRect();
    }
    bar.addEventListener('pointerdown', function (e) {
      if (window.matchMedia && matchMedia('(hover: none) and (pointer: coarse)').matches) return;
      if (e.target.closest('.gc-close, .gc-tip-trigger')) return;
      var r = toAbsolute();
      dragging = true;
      startX = e.clientX; startY = e.clientY;
      baseLeft = r.left; baseTop = r.top;
      if (bar.setPointerCapture) { try { bar.setPointerCapture(e.pointerId); } catch (err) {} }
      e.preventDefault();
    });
    bar.addEventListener('pointermove', function (e) {
      if (!dragging) return;
      var r = panel.getBoundingClientRect();
      var nl = clamp(baseLeft + (e.clientX - startX), 8, window.innerWidth - r.width - 8);
      var nt = clamp(baseTop + (e.clientY - startY), 8, window.innerHeight - 48);
      panel.style.left = nl + 'px';
      panel.style.top = nt + 'px';
    });
    function endDrag() { dragging = false; }
    bar.addEventListener('pointerup', endDrag);
    bar.addEventListener('pointercancel', endDrag);
    bar.addEventListener('dblclick', function () { // 双击复位到底部居中
      panel.style.left = '';
      panel.style.top = '';
      panel.style.bottom = '';
      panel.style.transform = '';
    });
  }

  /* ---------------- 采集 ---------------- */

  function onGenerate() {
    if (capturing) return;
    var video = activeVideo();
    if (!video) { setMsg('未找到可用的视频画面', 'err'); return; }
    if (!video.videoWidth || !video.videoHeight) { setMsg('视频元数据尚未就绪,请稍后再试', 'err'); return; }

    var start = parseTime($('gcStart').value);
    var end = parseTime($('gcEnd').value);
    if (!isFinite(start) || !isFinite(end)) { setMsg('请填写有效的开始/结束时间', 'err'); return; }
    var duration = isFinite(video.duration) ? video.duration : Infinity;
    if (start < 0 || end > duration) { setMsg('时间超出视频范围(时长 ' + fmt(Math.min(duration, 0)) + ' 至 ' + (isFinite(duration) ? fmt(duration) : '未知') + ')', 'err'); return; }
    if (start >= end) { setMsg('结束时间必须大于开始时间', 'err'); return; }
    if (end - start > MAX_SECONDS) { setMsg('区间超过上限 ' + MAX_SECONDS + ' 秒', 'err'); return; }

    var fps = parseInt(gcGet('gcFpsSel'), 10) || 10;
    var scale = (parseInt($('gcScale').value, 10) || 75) / 100;   // 百分比 -> 比例(等比缩放)
    var loopMode = gcGet('gcLoopSel') || 'infinite';
    var frames = Math.round((end - start) * fps);
    if (frames > MAX_FRAMES) { setMsg('帧数超过上限 ' + MAX_FRAMES + '(可降低帧率或缩短区间)', 'err'); return; }

    var s = loadSettings();
    s.fps = fps; s.scale = Math.round(scale * 100); s.loop = loopMode;
    saveSettings(s);

    setMsg('从 ' + fmt(start) + ' 截取到 ' + fmt(end) + ',约 ' + frames + ' 帧…', '');
    $('gcGenerate').disabled = true;
    $('gcCancel').disabled = false;
    $('gcResult').style.display = 'none';
    $('gcBarWrap').style.display = 'block';
    $('gcProgress').style.display = 'block';
    updateProgress(0, frames, start, end);

    startCapture(video, start, end, fps, scale, loopMode, frames);
  }

  function startCapture(video, start, end, fps, scale, loopMode, totalFrames) {
    capturing = true; cancelled = false;
    restoreState = { wasPlaying: !video.paused, muted: video.muted, time: video.currentTime, volume: video.volume };
    var w = Math.max(2, Math.round(video.videoWidth * scale));
    var h = Math.max(2, Math.round(video.videoHeight * scale));
    canvas = document.createElement('canvas');
    canvas.width = w; canvas.height = h;
    ctx = canvas.getContext('2d', { willReadFrequently: true });

    enc = window.gifenc.GIFEncoder();
    var repeatVal = loopMode === 'infinite' ? 0 : -1;

    var startedAt = Date.now();
    var frameCount = 0;
    var done = false;
    var playFailTimer = 0;                                  // 采集起播兜底计时器
    var startWall = 0, deadline = 0, nextFrameAt = 0;       // 墙钟采样(兼容直播流 currentTime=0)

    function cleanup(fn) {
      done = true;
      window.clearTimeout(playFailTimer);
      cancelAnimationFrame(rafId);
      capturing = false;
      $('gcGenerate').disabled = false;
      $('gcBarWrap').style.display = 'none';
      $('gcProgress').style.display = 'none';
      fn && fn();
    }

    function restorePlayback() {
      var v = restoreState;
      try {
        video.muted = v.muted;
        if (v.time < (isFinite(video.duration) ? video.duration : Infinity)) video.currentTime = Math.min(v.time, (isFinite(video.duration) ? video.duration : 0) - 0.05);
        if (v.wasPlaying) video.play().catch(function () {});
      } catch (e) {}
      restoreState = null;
    }

    function fail(msg) {
      cleanup(function () { setMsg(msg, 'err'); restorePlayback(); });
    }

    function captureFrame() {
      try {
        ctx.drawImage(video, 0, 0, w, h);
        var data = ctx.getImageData(0, 0, w, h).data;
        /* gifenc:量化(256色)-> 索引 -> 写帧(自带调色板,延迟 ms) */
        var pal = window.gifenc.quantize(data, 256);
        var idx = window.gifenc.applyPalette(data, pal);
        enc.writeFrame(idx, w, h, {
          palette: pal,
          delay: Math.round(1000 / fps),
          repeat: repeatVal,
          dispose: 1
        });
        frameCount++;
        updateProgress(frameCount, totalFrames, start, end);
        $('gcProgress').textContent = '采集中 ' + frameCount + '/' + totalFrames + ' 帧（' + fmt(Math.min(video.currentTime, end)) + 's）';
        $('gcBar').style.width = Math.min(100, (frameCount / totalFrames) * 100) + '%';
      } catch (e) {
        /* 任何采集异常都兜底终止,避免采集循环悬死 */
        fail('采集失败: ' + (e && e.name === 'SecurityError' ? '该视频源跨域且未开放 CORS,浏览器禁止读取画面' : (e && e.message || e)));
      }
    }

    function tick(now, meta) {
      if (done || cancelled) return;
      if (Date.now() - startedAt > ABORT_SECONDS * 1000) { finish(); return; }
      var wall = performance.now();
      if (wall >= deadline || video.ended) { finish(); return; }
      while (wall >= nextFrameAt) {
        captureFrame();
        nextFrameAt += 1000 / fps;
      }
      schedule();
    }

    function schedule() {
      if (done || cancelled) return;
      if (video.requestVideoFrameCallback) video.requestVideoFrameCallback(tick);
      else rafId = requestAnimationFrame(function () { tick(0, null); });
    }

    function finish() {
      if (done) return;
      var ok = frameCount > 0;
      try {
        enc.finish();
        var bytes = enc.bytes();
        var blob = new Blob([bytes], { type: 'image/gif' });
        var url = URL.createObjectURL(blob);       // 预览用
        var dlUrl = URL.createObjectURL(blob);     // 下载用(独立,避免 revoke 连累)
        var img = $('gcPreview');
        img.onload = function () { URL.revokeObjectURL(url); };
        img.src = url;
        $('gcMeta').textContent = frameCount + ' 帧 · ' + (bytes.length / 1024).toFixed(1) + ' KB · ' + (w + 'x' + h);
        var dl = $('gcDownload');
        dl.href = dlUrl;
        dl.download = baseName(video.currentSrc || video.src) + '_' + fmt(start).replace(/[:.]/g, '-') + '-'
          + fmt(end).replace(/[:.]/g, '-') + '.gif';
        if (lastDlUrl) URL.revokeObjectURL(lastDlUrl);   // 回收上一张
        lastDlUrl = dlUrl;
        $('gcResult').style.display = 'block';
        cleanup(function () { setMsg(ok ? '生成成功' : '没有采到帧', ok ? 'ok' : 'err'); restorePlayback(); });
      } catch (e) {
        ok = false;
        cleanup(function () { setMsg('编码失败: ' + (e && e.message || e), 'err'); restorePlayback(); });
      }
    }

    /* 开始:暂停 -> seek 到起点(可 seek 源) -> 起播采集(墙钟节奏) */
    video.pause();
    var fired = false;
    var finishSeek = function () {
      if (fired) return;
      fired = true;
      video.removeEventListener('seeked', finishSeek);
      video.removeEventListener('loadeddata', finishSeek);
      try {
        ctx.drawImage(video, 0, 0, 1, 1);
      } catch (e) {
        if (e && e.name === 'SecurityError') { fail('该视频源跨域且未开放 CORS,浏览器禁止读取画面,无法截动图'); return; }
        fail('画面读取失败: ' + (e && e.message || e));
        return;
      }
      startWall = performance.now();
      deadline = startWall + (end - start) * 1000;
      nextFrameAt = startWall + 1000 / fps;
      video.muted = true;
      video.play().catch(function () {});
      /* 播放被拒/暂停状态兜底:5 秒仍未起播则终止采集,避免悬死 */
      window.clearTimeout(playFailTimer);
      playFailTimer = window.setTimeout(function () {
        if (!done && !cancelled && video.paused) fail('无法启动采集:视频未进入播放状态');
      }, 5000);
      schedule();
    };
    video.addEventListener('seeked', finishSeek);
    video.addEventListener('loadeddata', finishSeek);
    var t0 = setTimeout(function () { finishSeek(); }, 2500);
    var seekable = isFinite(video.duration) && video.duration > 0 && start > 0.001;
    if (!seekable) {                    // 直播流:不可 seek,直接开始
      clearTimeout(t0);
      finishSeek();
    } else {
      try { video.currentTime = start; }
      catch (e) { clearTimeout(t0); finishSeek(); }
    }
  }

  function updateProgress(frames, total, start, end) {
    $('gcProgress').textContent = '进程 ' + frames + '/' + total + ' 帧';
  }

  function cancelCapture() {
    if (!capturing) { closePanel(); return; }
    /* 立即复位,避免采集循环/编码卡住后 capturing 永久为 true */
    cancelled = true;
    window.clearTimeout(playFailTimer);
    cancelAnimationFrame(rafId);
    capturing = false;
    $('gcGenerate').disabled = false;
    $('gcBarWrap').style.display = 'none';
    $('gcProgress').style.display = 'none';
    if (restoreState) {
      var v = activeVideo();
      if (v) {
        v.muted = restoreState.muted;
        if (restoreState.wasPlaying) v.play().catch(function () {});
        v.currentTime = Math.min(restoreState.time,
          (isFinite(v.duration) ? v.duration : restoreState.time) - 0.05);
      }
      restoreState = null;
    }
  }

  function closePanel() {
    if (capturing) cancelCapture();
    if (panel) panel.style.display = 'none';
  }

  function isPanelOpen() {
    return !!panel && panel.style.display !== 'none';
  }

  function setCurrentTime(inputId, message) {
    if (!isPanelOpen() || capturing) return false;
    var video = activeVideo();
    var current = video && Number(video.currentTime);
    if (!video || !isFinite(current) || current < 0) return false;
    $(inputId).value = fmt(current);
    setMsg(message + ' ' + fmt(current), '');
    return true;
  }

  function togglePanel() {
    if (isPanelOpen()) {
      closePanel();
    } else {
      window.openGifCapture();
    }
    return true;
  }

  /* 移动端:虚拟键盘弹出(visualViewport 收缩)时把面板抬到键盘上方 */
  function bindKeyboardShield() {
    if (!window.visualViewport || !panel) return;
    var onVv = function () {
      if (panel.style.display === 'none') return;
      var kb = window.innerHeight - window.visualViewport.height;   // 键盘占用高度
      panel.style.bottom = (kb > 80 ? kb + 10 : 104) + 'px';
    };
    window.visualViewport.addEventListener('resize', onVv);
    window.visualViewport.addEventListener('scroll', onVv);
  }

  /* 尺寸滑块联动:百分比 + 成片像素(等比) */
  function updateScaleUI(video) {
    var p = parseInt($('gcScale').value, 10) || 75;
    $('gcScaleVal').textContent = p + '%';
    var vw = (video && video.videoWidth) || 1920;
    var vh = (video && video.videoHeight) || 1080;
    $('gcPxHint').textContent = '→ ' + Math.round(vw * p / 100) + '×' + Math.round(vh * p / 100);
  }

  /* ---- 自定义下拉(深色弹层,任何浏览器一致) ---- */
  function buildSelect(wrapId, options, initial) {
    var wrap = document.getElementById(wrapId);
    if (!wrap) return;
    wrap._value = String(initial);
    wrap._options = options;
    var btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'gc-select-trigger';
    var menu = document.createElement('div');
    menu.className = 'gc-select-menu';
    options.forEach(function (o) {
      var opt = document.createElement('div');
      opt.className = 'gc-option' + (String(o[0]) === String(initial) ? ' active' : '');
      opt.dataset.v = String(o[0]);
      opt.textContent = String(o[1]);
      opt.addEventListener('click', function () {
        gcSet(wrapId, o[0]);
        wrap.classList.remove('open');
      });
      menu.appendChild(opt);
    });
    var label = document.createElement('span');
    label.textContent = String(options[0][1]);
    var caret = document.createElement('span');
    caret.className = 'gc-caret';
    caret.textContent = '▾';
    btn.appendChild(label);
    btn.appendChild(caret);
    btn.addEventListener('click', function (e) {
      e.stopPropagation();
      var opening = !wrap.classList.contains('open');
      closeOtherSelects(null);
      if (opening) wrap.classList.add('open');
    });
    wrap.appendChild(btn);
    wrap.appendChild(menu);
    gcSet(wrapId, initial);
  }
  function closeOtherSelects(except) {
    var all = document.querySelectorAll('#gcPanel .gc-select.open');
    for (var i = 0; i < all.length; i++) if (all[i] !== except) all[i].classList.remove('open');
  }
  function gcGet(id) {
    var w = document.getElementById(id);
    return w && w._value != null ? w._value : '';
  }
  function gcSet(id, v) {
    var w = document.getElementById(id);
    if (!w) return;
    w._value = String(v);
    var label = '';
    (w._options || []).forEach(function (o) {
      if (String(o[0]) === String(v)) label = String(o[1]);
    });
    var spans = w.querySelectorAll('.gc-select-trigger span');
    if (spans.length) spans[0].textContent = label;
    var opts = w.querySelectorAll('.gc-option');
    for (var i = 0; i < opts.length; i++) {
      opts[i].classList.toggle('active', opts[i].dataset.v === String(v));
    }
  }

  /* ---------------- 对外入口 ---------------- */

  window.openGifCapture = function () {
    var video = activeVideo();
    if (!video) { alert('请先打开一个视频'); return; }
    var p = ensurePanel();
    var s = loadSettings();
    gcSet('gcFpsSel', String(s.fps));
    /* 兼容旧存档(0.75)与新存档(75) */
    var sc = typeof s.scale === 'number' && s.scale > 1 ? Math.round(s.scale) : Math.round((s.scale || 0.75) * 100);
    sc = Math.min(100, Math.max(10, sc));
    $('gcScale').value = String(sc);
    gcSet('gcLoopSel', s.loop);
    updateScaleUI(video);
    var cur = video.currentTime;
    var dur = isFinite(video.duration) ? video.duration : cur + 5;
    $('gcStart').value = fmt(Math.min(cur, dur - 0.001));
    $('gcEnd').value = fmt(Math.min(cur + 5, dur));
    setMsg('设定时间段,或直接「取当前时间」', '');
    $('gcResult').style.display = 'none';
    $('gcBarWrap').style.display = 'none';
    $('gcProgress').style.display = 'none';
    $('gcGenerate').disabled = false;
    $('gcCancel').disabled = false;
    p.style.display = 'block';
  };

  window.GifCapture = {
    open: window.openGifCapture,
    toggle: togglePanel,
    close: closePanel,
    setStartFromCurrent: function () { return setCurrentTime('gcStart', '已设置开始时间'); },
    setEndFromCurrent: function () { return setCurrentTime('gcEnd', '已设置结束时间'); },
    generate: function () {
      if (!isPanelOpen() || capturing) return false;
      onGenerate();
      return true;
    },
    _isOpen: isPanelOpen,
    _isCapturing: function () { return capturing; }
  };
})();
