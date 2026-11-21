// End-to-end check against a running server on this machine:
//   node tests/e2e.mjs [port]
// Plays a viewer: hello → approval → virtual monitor → frames → input → mirror.
import { execFileSync } from 'node:child_process';

const port = process.argv[2] || 7940;
const base = `http://localhost:${port}`;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let failures = 0;
const ok = (cond, msg) => { console.log(`${cond ? 'PASS' : 'FAIL'}  ${msg}`); if (!cond) failures++; };
const hypr = (...a) => execFileSync('hyprctl', a, { encoding: 'utf8' });

const page = await (await fetch(`${base}/admin`)).text();
const key = page.match(/name="overspan-key" content="([0-9a-f]+)"/)[1];
const api = async (method, path, body) => {
  const r = await fetch(base + path, { method, headers: { 'X-Overspan-Key': key, 'Content-Type': 'application/json' }, body: body && JSON.stringify(body) });
  return r.json();
};

function viewer(hello) {
  const ws = new WebSocket(`ws://localhost:${port}/ws`);
  ws.binaryType = 'arraybuffer';
  const v = { ws, texts: [], frames: [], waiters: [] };
  const wake = () => { v.waiters.forEach((f) => f()); };
  ws.onopen = () => ws.send(JSON.stringify({ t: 'hello', ...hello }));
  ws.onmessage = (ev) => {
    if (typeof ev.data === 'string') v.texts.push(JSON.parse(ev.data));
    else {
      const dv = new DataView(ev.data);
      const f = { seq: dv.getUint32(2, true), full: dv.getUint8(1) & 1, w: dv.getUint16(6, true), h: dv.getUint16(8, true), n: dv.getUint16(10, true), soi: dv.getUint16(24) === 0xffd8, bytes: ev.data.byteLength };
      v.frames.push(f);
      ws.send(JSON.stringify({ t: 'ack', sq: f.seq }));
    }
    wake();
  };
  ws.onclose = () => { v.closed = true; wake(); };
  v.until = async (pred, ms = 8000) => {
    const end = Date.now() + ms;
    while (Date.now() < end) { const r = pred(); if (r) return r; await new Promise((res) => { v.waiters.push(res); setTimeout(res, 200); }); }
    return pred();
  };
  v.text = (t) => v.until(() => v.texts.find((m) => m.t === t));
  return v;
}

const device = 'e2e' + Math.random().toString(16).slice(2, 14);

// --- extend with approval ---
const v1 = viewer({ device, name: 'E2E tablet with a deliberately long name to test truncation', mode: 'extend', width: 1280, height: 800, scale: 1 });
const wait = await v1.text('wait');
ok(!!wait, 'untrusted device is asked to wait for approval');
let st = await api('GET', '/api/state');
const pending = st.viewers.find((x) => x.state === 'waiting');
ok(pending && pending.name.length <= 60, 'dashboard lists the pending device (name capped at 60)');
await api('POST', `/api/viewers/${pending.id}/approve?remember=1`);
const ready = await v1.text('ready');
ok(ready && ready.display.w === 1280 && ready.display.h === 800, `ready with a 1280x800 display (${ready && ready.display.name})`);
const mons = JSON.parse(hypr('monitors', '-j'));
const mon = mons.find((m) => m.name === ready.display.name);
ok(!!mon && mon.width === 1280 && mon.height === 800, `Hyprland has ${ready.display.name} at ${mon && mon.x},${mon && mon.y}`);
await v1.until(() => v1.frames.length >= 1);
ok(v1.frames[0] && v1.frames[0].full && v1.frames[0].soi && v1.frames[0].w === 1280, `first frame is a full JPEG update (${v1.frames[0] && v1.frames[0].n} strips, ${v1.frames[0] && v1.frames[0].bytes} bytes)`);

// input: move to the middle of the virtual monitor and read the cursor back
const before = JSON.parse(hypr('cursorpos', '-j'));
v1.ws.send(JSON.stringify({ t: 'p', e: 'move', x: 0.5, y: 0.5 }));
await sleep(300);
const pos = JSON.parse(hypr('cursorpos', '-j'));
const ex = mon.x + 640, ey = mon.y + 400;
ok(Math.abs(pos.x - ex) <= 3 && Math.abs(pos.y - ey) <= 3, `pointer landed at ${pos.x},${pos.y} (expected ~${ex},${ey})`);
await v1.until(() => v1.frames.length >= 2, 3000);
ok(v1.frames.length >= 2, `cursor movement produced a partial update (${v1.frames.length} frames)`);
const later = v1.frames.slice(1);
ok(later.length === 0 || later.every((f) => !f.full), 'later frames are partial');
try { hypr('dispatch', 'movecursor', String(before.x), String(before.y)); } catch { /* Lua-config Hyprland uses a different dispatch syntax */ }

st = await api('GET', '/api/state');
ok(st.viewers.some((x) => x.state === 'live'), 'dashboard shows the device as live');
ok(st.settings.trusted.some((t) => t.deviceId === device), 'device was remembered');

// drop and reconnect: the monitor is kept and reused
v1.ws.send('{"t":"bye"}');
await v1.until(() => v1.closed);
await sleep(500);
ok(JSON.parse(hypr('monitors', '-j')).some((m) => m.name === ready.display.name), 'monitor kept after disconnect');
const v2 = viewer({ device, name: 'E2E tablet', mode: 'extend', width: 1280, height: 800, scale: 1 });
const ready2 = await v2.text('ready');
ok(ready2 && ready2.display.name === ready.display.name, 'trusted device reconnects without approval and reuses its monitor');

// rotation
v2.ws.send(JSON.stringify({ t: 'resize', w: 800, h: 1280 }));
const size = await v2.until(() => v2.texts.find((m) => m.t === 'size' && m.w === 800), 10000);
ok(!!size, 'rotating the device resizes the monitor to 800x1280');

await api('POST', '/api/settings', { ...st.settings, keepSeconds: 0 });
v2.ws.send('{"t":"bye"}');
await v2.until(() => v2.closed);
await sleep(800);
ok(!JSON.parse(hypr('monitors', '-j')).some((m) => m.name === ready.display.name), 'monitor removed when keep time is 0');

// --- mirror ---
const v3 = viewer({ device, name: 'E2E mirror', mode: 'mirror', width: 1280, height: 800 });
const r3 = await v3.text('ready');
ok(r3 && r3.mode === 'mirror' && !r3.display.name.startsWith('OVERSPAN'), `mirror mode shares ${r3 && r3.display.name}`);
await v3.until(() => v3.frames.length >= 1);
ok(v3.frames[0] && v3.frames[0].w === r3.display.w, `mirror frame ${v3.frames[0] && v3.frames[0].w}x${v3.frames[0] && v3.frames[0].h}`);
v3.ws.close();

// --- PIN ---
await api('POST', '/api/settings', { ...st.settings, keepSeconds: 0, pin: '4321' });
const v4 = viewer({ device, name: 'E2E pin', mode: 'mirror', width: 800, height: 600 });
const e4 = await v4.text('error');
ok(e4 && e4.code === 'pin', 'PIN is required when set');
const v5 = viewer({ device, name: 'E2E pin', mode: 'mirror', width: 800, height: 600, pin: '0000' });
const e5 = await v5.text('error');
ok(e5 && e5.code === 'pin-wrong', 'wrong PIN is refused');
const v6 = viewer({ device, name: 'E2E pin', mode: 'mirror', width: 800, height: 600, pin: '4321' });
ok(!!(await v6.text('ready')), 'right PIN connects');
v6.ws.close();

// restore settings and forget the test device
await api('POST', '/api/settings', { ...st.settings, keepSeconds: 15, pin: '' });
await api('POST', `/api/trusted/${device}/forget`);
await sleep(500);
console.log(failures ? `\n${failures} check(s) failed` : '\nall checks passed');
process.exit(failures ? 1 : 0);
