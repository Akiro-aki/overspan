'use strict';
// Overspan browser viewer: connect, show the picture, send input back.

const $ = (s) => document.querySelector(s);
const store = {
  get(k, d) { try { const v = localStorage.getItem('ovs.' + k); return v === null ? d : JSON.parse(v); } catch { return d; } },
  set(k, v) { try { localStorage.setItem('ovs.' + k, JSON.stringify(v)); } catch { /* private mode */ } },
};

const prefs = Object.assign(
  { res: 'balanced', quality: 70, fps: 30, touch: 'direct', stats: false, rotate: true, mode: 'extend' },
  store.get('prefs', {}),
);
const savePrefs = () => store.set('prefs', prefs);

function randomId() {
  const a = new Uint8Array(16);
  (window.crypto || {}).getRandomValues ? crypto.getRandomValues(a) : a.forEach((_, i) => { a[i] = Math.random() * 256; });
  return Array.from(a, (b) => b.toString(16).padStart(2, '0')).join('');
}
let deviceId = store.get('device', '');
if (!deviceId) { deviceId = randomId(); store.set('device', deviceId); }

function guessName() {
  const ua = navigator.userAgent;
  if (/iPad/.test(ua) || (/Macintosh/.test(ua) && navigator.maxTouchPoints > 1)) return 'iPad';
  if (/iPhone/.test(ua)) return 'iPhone';
  if (/Android/.test(ua)) return /Mobile/.test(ua) ? 'Android phone' : 'Android tablet';
  if (/CrOS/.test(ua)) return 'Chromebook';
  if (/Windows/.test(ua)) return 'Windows PC';
  if (/Macintosh/.test(ua)) return 'Mac';
  if (/Linux/.test(ua)) return 'Linux PC';
  return 'Browser';
}

/* ---------- screens ---------- */
const screens = ['connect', 'state', 'stream'];
function show(name) {
  for (const s of screens) $('#screen-' + s).hidden = s !== name;
  document.body.dataset.screen = name;
}

function toast(text, ms = 2600) {
  const t = document.createElement('div');
  t.className = 'toast';
  t.textContent = text;
  $('#toasts').append(t);
  setTimeout(() => t.remove(), ms);
}

function showState({ title, text = '', kind = 'wait', retry = false, pin = false, pinError = '' }) {
  show('state');
  $('#state-title').textContent = title;
  $('#state-text').textContent = text;
  $('#state-art').dataset.kind = kind;
  $('#btn-state-retry').hidden = !retry;
  $('#pin-form').hidden = !pin;
  $('#pin-error').textContent = pinError;
  $('#pin').setAttribute('aria-invalid', pinError ? 'true' : 'false');
  if (pin) setTimeout(() => $('#pin').focus(), 50);
  else if (retry) $('#btn-state-retry').focus();
  else $('#btn-state-back').focus();
}

/* ---------- connect screen ---------- */
let info = null;
async function loadInfo() {
  const badge = $('#srv-status');
  try {
    const r = await fetch('/api/info', { cache: 'no-store' });
    if (!r.ok) throw new Error(r.status);
    info = await r.json();
    $('#srv-name').textContent = info.name || 'This computer';
    $('#srv-name').title = info.name || '';
    if (!info.enabled) { badge.className = 'badge badge-warn'; badge.textContent = 'Paused'; }
    else if (info.pin) { badge.className = 'badge badge-accent'; badge.textContent = 'PIN needed'; }
    else { badge.className = 'badge badge-ok'; badge.textContent = 'Ready'; }
    const ext = document.querySelector('input[name=mode][value=extend]');
    ext.disabled = !info.canExtend;
    if (!info.canExtend) {
      document.querySelector('input[name=mode][value=mirror]').checked = true;
    } else {
      document.querySelector(`input[name=mode][value=${prefs.mode}]`).checked = true;
    }
    updateModeHint();
    $('#btn-connect').disabled = !info.enabled;
    $('#connect-note').textContent = info.enabled ? '' : 'The computer is not accepting viewers right now. Turn it on from its dashboard.';
  } catch {
    $('#srv-name').textContent = 'Not reachable';
    badge.className = 'badge badge-fail'; badge.textContent = 'Offline';
    $('#connect-note').textContent = 'Could not reach the computer. Check that Overspan is running and both devices share a network.';
  }
}

function updateModeHint() {
  const mode = document.querySelector('input[name=mode]:checked').value;
  let hint = mode === 'extend'
    ? 'Adds a new monitor next to the computer’s screen.'
    : 'Shows what is already on the computer’s screen.';
  if (info && !info.canExtend) hint = 'This computer’s desktop can only share its existing screen.';
  $('#mode-hint').textContent = hint;
}

/* ---------- sizing ---------- */
const CAPS = { native: Infinity, balanced: 1920, light: 1280 };
const SCALES = [1, 1.25, 1.5, 2, 2.5, 3];
function targetSize() {
  const dpr = window.devicePixelRatio || 1;
  let cw = screen.width, ch = screen.height;
  // iOS reports portrait numbers in landscape: follow the window's shape
  if ((window.innerWidth > window.innerHeight) !== (cw > ch)) [cw, ch] = [ch, cw];
  let pw = Math.round(cw * dpr), ph = Math.round(ch * dpr);
  const cap = CAPS[prefs.res] || CAPS.balanced;
  const long = Math.max(pw, ph);
  if (long > cap) { const f = cap / long; pw = Math.round(pw * f); ph = Math.round(ph * f); }
  const ratio = pw / cw;
  let scale = 1;
  for (const s of SCALES) if (s <= ratio + 0.05) scale = s;
  return { width: pw, height: ph, scale };
}

/* ---------- connection ---------- */
let ws = null;
let session = null; // { mode, w, h, input }
let userClosed = false;
let pinValue = '';
let retries = 0;
let retryTimer = 0;

function connect() {
  clearTimeout(retryTimer);
  const mode = document.querySelector('input[name=mode]:checked').value;
  const name = $('#dev-name').value.trim() || guessName();
  prefs.mode = mode; savePrefs();
  store.set('name', name);
  userClosed = false;
  if (!session) showState({ title: 'Connecting', text: 'Reaching ' + (info && info.name ? info.name : 'the computer') + '…' });

  const size = targetSize();
  const proto = location.protocol === 'https:' ? 'wss://' : 'ws://';
  try { ws = new WebSocket(proto + location.host + '/ws'); }
  catch { return showState({ title: 'Could not connect', text: 'The browser refused the connection.', kind: 'stop', retry: true }); }
  ws.binaryType = 'arraybuffer';
  const sock = ws;
  sock.onopen = () => sock.send(JSON.stringify({
    t: 'hello', device: deviceId, name, mode, width: size.width, height: size.height,
    scale: size.scale, pin: pinValue, quality: prefs.quality, fps: prefs.fps,
  }));
  sock.onmessage = (ev) => {
    if (sock !== ws) return;
    if (typeof ev.data === 'string') onText(JSON.parse(ev.data));
    else onFrame(ev.data);
  };
  sock.onclose = () => { if (sock === ws) onClosed(); };
}

let lastError = null;
function onText(m) {
  switch (m.t) {
    case 'wait':
      showState({ title: 'Waiting for approval', text: m.text });
      break;
    case 'setup':
      if (!session) {
        showState(m.mode === 'extend'
          ? { title: 'Adding a monitor', text: 'The computer is making room for this screen. This takes a few seconds.' }
          : { title: 'Starting the picture', text: '' });
      }
      break;
    case 'error':
      lastError = m;
      break;
    case 'ready':
      retries = 0;
      lastError = null;
      session = { mode: m.mode, w: m.display.w, h: m.display.h, input: m.input, orientation: innerWidth > innerHeight };
      rememberServer();
      startStream();
      if (!m.input) toast('The computer has input from viewers turned off.');
      break;
    case 'size':
      if (session) { session.w = m.w; session.h = m.h; }
      break;
    case 'notice':
      if (m.code === 'asleep') showBanner(m.text);
      else hideBanner();
      break;
    case 'input':
      if (session) { session.input = m.on; toast(m.on ? 'Input turned on' : 'Input turned off by the computer'); }
      break;
  }
}

function onClosed() {
  const wasStreaming = !!session;
  stopStream();
  if (userClosed) { session = null; show('connect'); loadInfo(); return; }
  const e = lastError;
  lastError = null;
  if (e) {
    session = null;
    if (e.code === 'pin' || e.code === 'pin-wrong') {
      return showState({ title: 'Enter the PIN', text: 'This computer asks for a PIN before it shows its screen.', kind: 'lock', pin: true, pinError: e.code === 'pin-wrong' ? 'That PIN did not match. Try again.' : '' });
    }
    const titles = {
      denied: 'Connection declined', disabled: 'The computer is paused', timeout: 'No answer',
      closed: 'Disconnected', display: 'No display available', capture: 'Picture unavailable', protocol: 'Update needed',
    };
    return showState({ title: titles[e.code] || 'Disconnected', text: e.text, kind: 'stop', retry: true });
  }
  if (wasStreaming && retries < 6) {
    retries++;
    showBanner('Connection lost. Reconnecting…');
    show('stream');
    retryTimer = setTimeout(connect, Math.min(4000, 600 * retries));
    return;
  }
  session = null;
  showState({ title: 'Connection lost', text: 'The link to the computer dropped. Make sure both devices are on the same network.', kind: 'stop', retry: true });
}

function disconnect() {
  userClosed = true;
  clearTimeout(retryTimer);
  if (ws && ws.readyState === WebSocket.OPEN) { try { ws.send('{"t":"bye"}'); } catch { /* closing */ } ws.close(); }
  else { session = null; stopStream(); show('connect'); loadInfo(); }
  if (document.fullscreenElement) document.exitFullscreen().catch(() => {});
}

function send(obj) { if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify(obj)); }

function rememberServer() {
  const list = store.get('recent', []).filter((h) => h !== location.host);
  list.unshift(location.host);
  store.set('recent', list.slice(0, 6));
}

/* ---------- frames ---------- */
const canvas = $('#stage');
const ctx = canvas.getContext('2d', { alpha: false, desynchronized: true });
let chain = Promise.resolve();
let stat = { frames: 0, bytes: 0, t: performance.now(), fps: 0, kbps: 0 };

function decode(blob) {
  if (window.createImageBitmap) return createImageBitmap(blob);
  return new Promise((res, rej) => {
    const img = new Image();
    const url = URL.createObjectURL(blob);
    img.onload = () => { URL.revokeObjectURL(url); res(img); };
    img.onerror = (e) => { URL.revokeObjectURL(url); rej(e); };
    img.src = url;
  });
}

function onFrame(buf) {
  chain = chain.then(() => drawFrame(buf)).catch((e) => console.warn('frame dropped', e));
}

async function drawFrame(buf) {
  const dv = new DataView(buf);
  if (dv.getUint8(0) !== 0x46) return; // 'F'
  const seq = dv.getUint32(2, true);
  const w = dv.getUint16(6, true), h = dv.getUint16(8, true), n = dv.getUint16(10, true);
  const rects = [];
  let off = 12;
  for (let i = 0; i < n; i++) {
    const x = dv.getUint16(off, true), y = dv.getUint16(off + 2, true);
    const len = dv.getUint32(off + 8, true);
    rects.push({ x, y, blob: new Blob([new Uint8Array(buf, off + 12, len)], { type: 'image/jpeg' }) });
    off += 12 + len;
  }
  const imgs = await Promise.all(rects.map((r) => decode(r.blob)));
  if (canvas.width !== w || canvas.height !== h) { canvas.width = w; canvas.height = h; }
  imgs.forEach((img, i) => { ctx.drawImage(img, rects[i].x, rects[i].y); if (img.close) img.close(); });
  send({ t: 'ack', sq: seq });
  stat.frames++; stat.bytes += buf.byteLength;
}

function tickStats() {
  const now = performance.now();
  const dt = (now - stat.t) / 1000;
  stat.fps = stat.frames / dt; stat.kbps = stat.bytes / 1024 / dt;
  stat.frames = 0; stat.bytes = 0; stat.t = now;
  if (prefs.stats && session) {
    $('#stats').hidden = false;
    $('#stats').textContent = `${canvas.width}×${canvas.height}  ${stat.fps.toFixed(0)} fps  ${stat.kbps.toFixed(0)} KB/s`;
  } else $('#stats').hidden = true;
}
setInterval(tickStats, 1000);

/* ---------- stream screen ---------- */
let wakeLock = null;
function startStream() {
  show('stream');
  hideBanner();
  applyTouchMode();
  canvas.focus({ preventScroll: true });
  send({ t: 'refresh' });
  if (navigator.wakeLock) navigator.wakeLock.request('screen').then((l) => { wakeLock = l; }).catch(() => {});
  if (!store.get('seenToolbarHint', false)) {
    toast('Tap the tab at the top for controls.', 4000);
    store.set('seenToolbarHint', true);
  }
}

function stopStream() {
  releaseAll();
  hideToolbar();
  if (wakeLock) { wakeLock.release().catch(() => {}); wakeLock = null; }
}

function showBanner(text) { const b = $('#stream-banner'); b.textContent = text; b.hidden = false; }
function hideBanner() { $('#stream-banner').hidden = true; }

/* toolbar */
let tbTimer = 0;
function showToolbar() {
  $('#toolbar').hidden = false;
  $('#tb-handle').setAttribute('aria-expanded', 'true');
  clearTimeout(tbTimer);
  tbTimer = setTimeout(() => { if (!$('#toolbar').contains(document.activeElement)) hideToolbar(); }, 6000);
}
function hideToolbar() {
  $('#toolbar').hidden = true;
  $('#tb-handle').setAttribute('aria-expanded', 'false');
}
$('#tb-handle').addEventListener('click', () => ($('#toolbar').hidden ? showToolbar() : hideToolbar()));
$('#toolbar').addEventListener('pointerdown', () => showToolbar());
$('#toolbar').addEventListener('focusin', () => showToolbar());
$('#toolbar').addEventListener('click', (e) => {
  const b = e.target.closest('[data-act]');
  if (!b) return;
  const act = b.dataset.act;
  if (act === 'keyboard') toggleKeyboard();
  else if (act === 'touchmode') { prefs.touch = prefs.touch === 'direct' ? 'trackpad' : 'direct'; savePrefs(); applyTouchMode(); syncSettings(); }
  else if (act === 'fullscreen') toggleFullscreen();
  else if (act === 'settings') openSettings();
  else if (act === 'disconnect') disconnect();
});

function toggleFullscreen() {
  const el = document.documentElement;
  if (document.fullscreenElement || document.webkitFullscreenElement) {
    (document.exitFullscreen || document.webkitExitFullscreen).call(document);
  } else {
    const req = el.requestFullscreen || el.webkitRequestFullscreen;
    if (req) Promise.resolve(req.call(el)).catch(() => toast('This browser does not allow full screen here.'));
    else toast('Add this page to your home screen for full screen.');
  }
}

function applyTouchMode() {
  const pad = prefs.touch === 'trackpad';
  $('#screen-stream').classList.toggle('trackpad', pad);
  const btn = document.querySelector('[data-act=touchmode]');
  btn.setAttribute('aria-pressed', String(pad));
  $('#touchmode-label').textContent = pad ? 'Trackpad' : 'Direct';
}

/* ---------- pointer input ---------- */
function norm(clientX, clientY) {
  const r = canvas.getBoundingClientRect();
  const cw = canvas.width || 1, ch = canvas.height || 1;
  const s = Math.min(r.width / cw, r.height / ch);
  const dw = cw * s, dh = ch * s;
  const ox = r.left + (r.width - dw) / 2, oy = r.top + (r.height - dh) / 2;
  return { x: (clientX - ox) / dw, y: (clientY - oy) / dh, s };
}

let pendingMove = null;
let rafQueued = false;
function queueMove(msg) {
  pendingMove = msg;
  if (rafQueued) return;
  rafQueued = true;
  requestAnimationFrame(() => {
    rafQueued = false;
    if (pendingMove) { send(pendingMove); pendingMove = null; }
  });
}
function flushMove() { if (pendingMove) { send(pendingMove); pendingMove = null; } }

function ptr(e, kind, b = 0) {
  const p = norm(e.clientX, e.clientY);
  const msg = { t: 'p', e: kind, x: p.x, y: p.y, b };
  if (kind === 'move') queueMove(msg); else { flushMove(); send(msg); }
}
function click(b = 0, at) {
  if (at) send({ t: 'p', e: 'move', x: at.x, y: at.y });
  send({ t: 'btn', b, d: true });
  send({ t: 'btn', b, d: false });
}

const touches = new Map(); // pointerId -> {x, y, sx, sy, t}
let gesture = null; // null | {kind:'pending'|'drag'|'scroll'|'pad'|'padDrag', ...}
let longTimer = 0;
const SLOP = 10;

canvas.addEventListener('contextmenu', (e) => e.preventDefault());

// pen: real pressure, tilt, barrel button and eraser through the server's tablet device
function pen(e, kind) {
  const p = norm(e.clientX, e.clientY);
  const msg = { t: 'pen', e: kind, x: p.x, y: p.y, p: e.pressure || 0, tx: e.tiltX || 0, ty: e.tiltY || 0,
    b: (e.buttons & 2) ? 1 : 0, r: (e.buttons & 32) !== 0 };
  if (kind === 'move' || kind === 'hover') queueMove(msg); else { flushMove(); send(msg); }
}

canvas.addEventListener('pointerdown', (e) => {
  if (!session) return;
  canvas.setPointerCapture(e.pointerId);
  hideToolbar();
  if (e.pointerType === 'pen') { pen(e, 'down'); return; }
  if (e.pointerType !== 'touch') { ptr(e, 'down', e.button); return; }
  e.preventDefault();
  touches.set(e.pointerId, { x: e.clientX, y: e.clientY, sx: e.clientX, sy: e.clientY, t: performance.now() });
  if (touches.size === 1) {
    gesture = { kind: prefs.touch === 'trackpad' ? 'pad' : 'pending', moved: false, start: performance.now() };
    if (prefs.touch === 'direct') ptr(e, 'move');
    clearTimeout(longTimer);
    longTimer = setTimeout(() => {
      if (!gesture || gesture.moved || touches.size !== 1) return;
      if (gesture.kind === 'pending') { click(2); gesture.kind = 'done'; navigator.vibrate && navigator.vibrate(15); }
      else if (gesture.kind === 'pad') { send({ t: 'btn', b: 0, d: true }); gesture.kind = 'padDrag'; navigator.vibrate && navigator.vibrate(15); }
    }, 520);
  } else if (touches.size === 2) {
    clearTimeout(longTimer);
    if (gesture && gesture.kind === 'drag') ptr(e, 'up', 0);
    gesture = { kind: 'scroll', moved: false, start: performance.now(), acc: { x: 0, y: 0 } };
  }
});

canvas.addEventListener('pointermove', (e) => {
  if (!session) return;
  if (e.pointerType === 'pen') { pen(e, e.buttons & 1 ? 'move' : 'hover'); return; }
  if (e.pointerType !== 'touch') { ptr(e, 'move'); return; }
  const t = touches.get(e.pointerId);
  if (!t || !gesture) return;
  const dx = e.clientX - t.x, dy = e.clientY - t.y;
  t.x = e.clientX; t.y = e.clientY;
  const far = Math.hypot(e.clientX - t.sx, e.clientY - t.sy) > SLOP;
  if (far) gesture.moved = true;
  switch (gesture.kind) {
    case 'pending':
      if (far) {
        const p = norm(t.sx, t.sy);
        flushMove();
        send({ t: 'p', e: 'down', x: p.x, y: p.y, b: 0 });
        gesture.kind = 'drag';
        ptr(e, 'move');
      }
      break;
    case 'drag': ptr(e, 'move'); break;
    case 'pad': case 'padDrag': {
      const s = norm(0, 0).s || 1;
      const accel = 1 + Math.min(2, Math.hypot(dx, dy) / 12);
      send({ t: 'rel', dx: (dx / s) * accel, dy: (dy / s) * accel });
      break;
    }
    case 'scroll': {
      // average the fingers: half per pointer event; natural scrolling
      gesture.acc.y += -dy * 1.5;
      gesture.acc.x += -dx * 1.5;
      const sy = Math.trunc(gesture.acc.y), sx = Math.trunc(gesture.acc.x);
      if (Math.abs(sy) >= 1 || Math.abs(sx) >= 1) {
        send({ t: 'w', dx: sx, dy: sy });
        gesture.acc.y -= sy; gesture.acc.x -= sx;
      }
      break;
    }
  }
});

function endTouch(e) {
  if (e.pointerType === 'pen') { if (session) pen(e, 'hover'); return; }
  if (e.pointerType !== 'touch') { if (session) ptr(e, 'up', e.button); return; }
  const t = touches.get(e.pointerId);
  touches.delete(e.pointerId);
  if (!t || !gesture) return;
  clearTimeout(longTimer);
  const quick = performance.now() - gesture.start < 300;
  if (e.type === 'pointercancel') {
    if (gesture.kind === 'drag') ptr(e, 'up', 0);
    if (gesture.kind === 'padDrag') send({ t: 'btn', b: 0, d: false });
    gesture = null; touches.clear();
    return;
  }
  switch (gesture.kind) {
    case 'pending': click(0, norm(t.sx, t.sy)); break;
    case 'drag': ptr(e, 'up', 0); break;
    case 'pad': if (!gesture.moved && quick) click(0); break;
    case 'padDrag': send({ t: 'btn', b: 0, d: false }); break;
    case 'scroll':
      if (!gesture.moved && quick && touches.size === 1) click(2);
      if (touches.size > 0) { gesture.kind = 'done'; return; }
      break;
  }
  if (touches.size === 0) gesture = null;
}
canvas.addEventListener('pointerup', endTouch);
canvas.addEventListener('pointercancel', endTouch);
canvas.addEventListener('pointerleave', (e) => { if (session && e.pointerType === 'pen') pen(e, 'leave'); });

canvas.addEventListener('wheel', (e) => {
  if (!session) return;
  e.preventDefault();
  const mul = e.deltaMode === 1 ? 40 : e.deltaMode === 2 ? 800 : 1.2;
  send({ t: 'w', dx: Math.round(e.deltaX * mul), dy: Math.round(e.deltaY * mul) });
}, { passive: false });

/* ---------- keyboard ---------- */
const soft = $('#soft-kbd');
const downKeys = new Set();
function keyEvent(e, down) {
  if (!session || document.body.dataset.screen !== 'stream') return;
  if ($('#settings').open || $('#other').open) return;
  // Alt+Shift+O stays on this device: opens the toolbar for keyboard users
  if (down && e.altKey && e.shiftKey && e.code === 'KeyO') {
    e.preventDefault();
    releaseAll();
    showToolbar();
    $('#toolbar .v-tool').focus();
    return;
  }
  if ($('#toolbar').contains(document.activeElement) || document.activeElement === $('#tb-handle')) {
    if (down && e.code === 'Escape') { hideToolbar(); canvas.focus({ preventScroll: true }); e.preventDefault(); }
    return; // let Tab, Enter and Space work the toolbar locally
  }
  const code = e.code;
  if (!code || code === 'Unidentified') return; // soft keyboards: handled by the input event
  e.preventDefault();
  if (down) downKeys.add(code); else downKeys.delete(code);
  send({ t: 'k', c: code, d: down });
}
document.addEventListener('keydown', (e) => keyEvent(e, true));
document.addEventListener('keyup', (e) => keyEvent(e, false));
window.addEventListener('blur', releaseAll);

function releaseAll() {
  for (const c of downKeys) send({ t: 'k', c, d: false });
  downKeys.clear();
}

function toggleKeyboard() {
  if (document.activeElement === soft) { soft.blur(); canvas.focus({ preventScroll: true }); }
  else { soft.value = ''; soft.focus({ preventScroll: true }); }
}
function tapKey(code) { send({ t: 'k', c: code, d: true }); send({ t: 'k', c: code, d: false }); }
soft.addEventListener('beforeinput', (e) => {
  if (e.inputType === 'deleteContentBackward') { e.preventDefault(); tapKey('Backspace'); }
  else if (e.inputType === 'insertLineBreak' || e.inputType === 'insertParagraph') { e.preventDefault(); tapKey('Enter'); }
});
soft.addEventListener('input', (e) => {
  const text = e.data || soft.value;
  if (text) send({ t: 'txt', s: text });
  soft.value = '';
});

/* ---------- rotation / resize ---------- */
let resizeTimer = 0;
window.addEventListener('resize', () => {
  clearTimeout(resizeTimer);
  resizeTimer = setTimeout(() => {
    if (!session || session.mode !== 'extend' || !prefs.rotate) return;
    const landscape = innerWidth > innerHeight;
    if (landscape === session.orientation) return;
    session.orientation = landscape;
    const s = targetSize();
    send({ t: 'resize', w: s.width, h: s.height });
  }, 700);
});

/* ---------- settings ---------- */
function syncSettings() {
  $('#set-res').value = prefs.res;
  $('#set-quality').value = prefs.quality; $('#out-quality').textContent = prefs.quality;
  $('#set-fps').value = prefs.fps; $('#out-fps').textContent = prefs.fps;
  document.querySelector(`input[name=touch][value=${prefs.touch}]`).checked = true;
  $('#set-stats').checked = prefs.stats;
  $('#set-rotate').checked = prefs.rotate;
}
function openSettings() { syncSettings(); $('#settings').showModal(); }
$('#set-res').addEventListener('change', (e) => {
  prefs.res = e.target.value; savePrefs();
  if (session && session.mode === 'extend') { const s = targetSize(); send({ t: 'resize', w: s.width, h: s.height }); }
});
$('#set-quality').addEventListener('input', (e) => { prefs.quality = +e.target.value; $('#out-quality').textContent = prefs.quality; });
$('#set-quality').addEventListener('change', () => { savePrefs(); send({ t: 'cfg', q: prefs.quality }); });
$('#set-fps').addEventListener('input', (e) => { prefs.fps = +e.target.value; $('#out-fps').textContent = prefs.fps; });
$('#set-fps').addEventListener('change', () => { savePrefs(); send({ t: 'cfg', f: prefs.fps }); });
document.querySelectorAll('input[name=touch]').forEach((r) => r.addEventListener('change', (e) => { prefs.touch = e.target.value; savePrefs(); applyTouchMode(); }));
$('#set-stats').addEventListener('change', (e) => { prefs.stats = e.target.checked; savePrefs(); });
$('#set-rotate').addEventListener('change', (e) => { prefs.rotate = e.target.checked; savePrefs(); });
document.querySelectorAll('dialog [data-close]').forEach((b) => b.addEventListener('click', () => b.closest('dialog').close()));
$('#settings').addEventListener('close', () => { if (session) canvas.focus({ preventScroll: true }); });

/* ---------- other computer ---------- */
function openOther() {
  const box = $('#recent');
  box.replaceChildren();
  const list = store.get('recent', []).filter((h) => h !== location.host);
  if (list.length) {
    const p = document.createElement('p'); p.className = 'field-label'; p.textContent = 'Recent';
    box.append(p);
    for (const h of list) {
      const a = document.createElement('a');
      a.className = 'btn btn-block mono'; a.href = 'http://' + h + '/'; a.textContent = h;
      box.append(a);
    }
  }
  $('#other').showModal();
}
$('#other-form').addEventListener('submit', (e) => {
  e.preventDefault();
  let v = $('#other-addr').value.trim().replace(/^https?:\/\//, '').replace(/\/.*$/, '');
  if (!v) return;
  if (!/:\d+$/.test(v)) v += ':7940';
  location.href = 'http://' + v + '/';
});

/* ---------- wiring ---------- */
$('#dev-name').value = store.get('name', '') || guessName();
document.querySelectorAll('input[name=mode]').forEach((r) => r.addEventListener('change', updateModeHint));
$('#btn-connect').addEventListener('click', () => {
  if (matchMedia('(pointer: coarse)').matches && !document.fullscreenElement) {
    const el = document.documentElement;
    const req = el.requestFullscreen || el.webkitRequestFullscreen;
    if (req) Promise.resolve(req.call(el)).catch(() => {});
  }
  pinValue = '';
  connect();
});
$('#btn-open-settings').addEventListener('click', openSettings);
$('#btn-other').addEventListener('click', openOther);
$('#btn-state-retry').addEventListener('click', () => { retries = 0; connect(); });
$('#btn-state-back').addEventListener('click', () => { userClosed = true; if (ws) ws.close(); session = null; show('connect'); loadInfo(); });
$('#pin-form').addEventListener('submit', (e) => { e.preventDefault(); pinValue = $('#pin').value.trim(); connect(); });
document.addEventListener('visibilitychange', () => {
  if (document.visibilityState === 'visible' && session) send({ t: 'refresh' });
});

show('connect');
loadInfo();
