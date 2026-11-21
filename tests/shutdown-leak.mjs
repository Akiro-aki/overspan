// Regression: stopping the server while a viewer is connected must remove
// that viewer's virtual monitor (it used to be left behind).
import { spawn, execFileSync } from 'node:child_process';
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

const port = 7950;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const overspanMonitors = () => JSON.parse(execFileSync('hyprctl', ['monitors', '-j'], { encoding: 'utf8' })).filter((m) => m.name.startsWith('OVERSPAN-')).map((m) => m.name);

const before = overspanMonitors();
const cfg = mkdtempSync(join(tmpdir(), 'ovs-leak-'));
const srv = spawn(fileURLToPath(new URL('../dist/overspan', import.meta.url)), ['serve', '--port', String(port)], { env: { ...process.env, XDG_CONFIG_HOME: cfg }, stdio: 'ignore' });
await sleep(1000);
const page = await (await fetch(`http://localhost:${port}/admin`)).text();
const key = page.match(/overspan-key" content="([0-9a-f]+)/)[1];
const ws = new WebSocket(`ws://localhost:${port}/ws`);
let ready = false;
ws.onopen = () => ws.send(JSON.stringify({ t: 'hello', device: 'leaktest0123456789', name: 'leak test', mode: 'extend', width: 800, height: 600 }));
ws.onmessage = async (e) => {
  if (typeof e.data !== 'string') return;
  const m = JSON.parse(e.data);
  if (m.t === 'wait') {
    const st = await (await fetch(`http://localhost:${port}/api/state`, { headers: { 'X-Overspan-Key': key } })).json();
    await fetch(`http://localhost:${port}/api/viewers/${st.viewers[0].id}/approve`, { method: 'POST', headers: { 'X-Overspan-Key': key } });
  }
  if (m.t === 'ready') ready = true;
};
for (let i = 0; i < 60 && !ready; i++) await sleep(250);
const during = overspanMonitors().filter((n) => !before.includes(n));
srv.kill('SIGTERM');
await new Promise((r) => srv.on('exit', r));
const after = overspanMonitors().filter((n) => !before.includes(n));
const ok = ready && during.length === 1 && after.length === 0;
console.log(`${ok ? 'PASS' : 'FAIL'}  stopping the server with a live viewer removes its monitor (during: ${during.join(',') || 'none'}; after: ${after.join(',') || 'none'})`);
for (const n of after) execFileSync('hyprctl', ['output', 'remove', n]); // never leave one behind
process.exit(ok ? 0 : 1);
