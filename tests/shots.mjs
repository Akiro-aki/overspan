// Screenshots every screen and state through a headless Chromium (Brave,
// Chrome or Chromium) driven over the DevTools protocol, and fails on any
// console error or uncaught exception.
//   node tests/shots.mjs <outdir> [port]
import { spawn, execFileSync } from 'node:child_process';
import { mkdtempSync, writeFileSync, mkdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const out = process.argv[2] || 'replica/clone-screens';
const port = process.argv[3] || 7940;
const base = `http://localhost:${port}`;
mkdirSync(out, { recursive: true });
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const browserBin = ['brave', 'chromium', 'google-chrome-stable', 'chromium-browser'].find((b) => { try { execFileSync('which', [b], { stdio: 'ignore' }); return true; } catch { return false; } });
if (!browserBin) { console.error('no Chromium-family browser found'); process.exit(2); }

const prof = mkdtempSync(join(tmpdir(), 'ovs-shots-'));
const br = spawn(browserBin, ['--headless=new', '--remote-debugging-port=9333', `--user-data-dir=${prof}`, '--no-first-run', '--no-default-browser-check', '--hide-scrollbars', 'about:blank'], { stdio: 'ignore' });
let wsUrl;
for (let i = 0; i < 50 && !wsUrl; i++) { await sleep(200); try { wsUrl = (await (await fetch('http://127.0.0.1:9333/json/version')).json()).webSocketDebuggerUrl; } catch { /* starting */ } }
const cdp = new WebSocket(wsUrl);
await new Promise((r) => { cdp.onopen = r; });
let nextId = 1;
const pending = new Map();
const problems = [];
cdp.onmessage = (ev) => {
  const m = JSON.parse(ev.data);
  if (m.id && pending.has(m.id)) { pending.get(m.id)(m); pending.delete(m.id); return; }
  if (m.method === 'Runtime.exceptionThrown') problems.push(`[${m.sessionId.slice(0, 4)}] exception: ${m.params.exceptionDetails.exception?.description || m.params.exceptionDetails.text}`);
  if (m.method === 'Runtime.consoleAPICalled' && m.params.type === 'error') problems.push(`[${m.sessionId.slice(0, 4)}] console.error: ${m.params.args.map((a) => a.value ?? a.description).join(' ')}`);
  if (m.method === 'Log.entryAdded' && m.params.entry.level === 'error') problems.push(`log: ${m.params.entry.text} ${m.params.entry.url || ''}`);
};
const call = (method, params = {}, sessionId) => new Promise((res, rej) => {
  const id = nextId++;
  pending.set(id, (m) => (m.error ? rej(new Error(method + ': ' + m.error.message)) : res(m.result)));
  cdp.send(JSON.stringify({ id, method, params, sessionId }));
});

async function tab(w, h, mobile, dpr = 1) {
  const { targetId } = await call('Target.createTarget', { url: 'about:blank' });
  const { sessionId } = await call('Target.attachToTarget', { targetId, flatten: true });
  await call('Page.enable', {}, sessionId);
  await call('Runtime.enable', {}, sessionId);
  await call('Log.enable', {}, sessionId);
  await call('Emulation.setDeviceMetricsOverride', { width: w, height: h, deviceScaleFactor: dpr, mobile, screenWidth: w, screenHeight: h }, sessionId);
  if (mobile) await call('Emulation.setTouchEmulationEnabled', { enabled: true, maxTouchPoints: 5 }, sessionId);
  const t = {
    go: async (url) => { await call('Page.navigate', { url }, sessionId); await sleep(900); },
    js: async (expr) => (await call('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true }, sessionId)).result.value,
    shot: async (name) => {
      const { data } = await call('Page.captureScreenshot', { format: 'png' }, sessionId);
      writeFileSync(join(out, name + '.png'), Buffer.from(data, 'base64'));
      console.log('saved', name);
    },
    size: (w2, h2) => call('Emulation.setDeviceMetricsOverride', { width: w2, height: h2, deviceScaleFactor: dpr, mobile, screenWidth: w2, screenHeight: h2 }, sessionId),
  };
  return t;
}

const page = await (await fetch(`${base}/admin`)).text();
const key = page.match(/name="overspan-key" content="([0-9a-f]+)"/)[1];
const api = async (method, path, body) => (await fetch(base + path, { method, headers: { 'X-Overspan-Key': key, 'Content-Type': 'application/json' }, body: body && JSON.stringify(body) })).json();
const st0 = await api('GET', '/api/state');

try {
  const admin = await tab(1440, 900, false);
  await admin.go(`${base}/admin#/overview`);
  await sleep(600);
  await admin.shot('S01-overview-empty-1440');

  // V01 connect on a phone
  const phone = await tab(390, 844, true, 2);
  await phone.go(`${base}/`);
  await phone.js(`document.querySelector('#dev-name').value = 'Pixel tablet in the kitchen with a very long name'`);
  await phone.shot('V01-connect-390');
  await phone.js(`document.querySelector('#btn-open-settings').click()`);
  await sleep(300);
  await phone.shot('V04-settings-390');
  await phone.js(`document.querySelector('#settings').close()`);

  // V02 waiting + S01 with a pending request
  await phone.js(`document.querySelector('#btn-connect').click()`);
  await sleep(900);
  await phone.shot('V02-waiting-390');
  await sleep(300);
  await admin.shot('S01-overview-pending-1440');
  const st = await api('GET', '/api/state');
  const req = st.viewers.find((v) => v.state === 'waiting');
  await api('POST', `/api/viewers/${req.id}/approve?remember=1`);
  await sleep(300);
  await phone.shot('V02-setup-390');
  // wait for the stream
  for (let i = 0; i < 40; i++) { if (await phone.js(`document.body.dataset.screen === 'stream' && document.querySelector('#stage').width > 0`)) break; await sleep(250); }
  await sleep(1200);
  await phone.shot('V03-stream-390');
  await phone.js(`document.querySelector('#tb-handle').click()`);
  await sleep(200);
  await phone.shot('V03-stream-toolbar-390');
  await sleep(1500);
  await admin.shot('S01-overview-live-1440');
  await admin.size(390, 844);
  await sleep(500);
  await admin.shot('S01-overview-live-390');
  await admin.size(1440, 900);

  for (const [id, r] of [['S02', 'display'], ['S03', 'security'], ['S04', 'system']]) {
    await admin.js(`location.hash = '#/${r}'`);
    await sleep(900);
    await admin.shot(`${id}-${r}-1440`);
    await admin.size(390, 844); await sleep(400);
    await admin.shot(`${id}-${r}-390`);
    await admin.size(1440, 900);
  }

  // V02 error (server paused while streaming)
  await api('POST', '/api/settings', { ...st0.settings, enabled: false });
  await sleep(1200);
  await phone.shot('V02-error-390');
  await admin.js(`location.hash = '#/overview'`); await sleep(700);
  await admin.shot('S01-overview-paused-1440');

  // V02 PIN
  await api('POST', '/api/settings', { ...st0.settings, enabled: true, pin: '2468' });
  await phone.go(`${base}/`);
  await phone.js(`document.querySelector('#btn-connect').click()`);
  await sleep(1000);
  await phone.shot('V02-pin-390');

  // V01 at desktop width
  const laptop = await tab(1440, 900, false);
  await laptop.go(`${base}/`);
  await laptop.shot('V01-connect-1440');
} finally {
  await api('POST', '/api/settings', { ...st0.settings, enabled: true, pin: '' });
  for (const t of (await api('GET', '/api/state')).settings.trusted) await api('POST', `/api/trusted/${t.deviceId}/forget`);
  br.kill();
}
const real = problems.filter((p) => !/WebSocket connection to .* failed|ERR_CONNECTION_REFUSED/.test(p));
console.log(real.length ? 'PROBLEMS:\n' + real.join('\n') : 'no console errors');
process.exit(real.length ? 1 : 0);
