#!/usr/bin/env python3
"""Drives the Overspan Windows viewer through its real UI, under Wine with the
null (headless) display driver, against a throwaway server on this machine.

    python3 tests/windows_e2e.py [outdir]

Needs: wine and a WINEPREFIX set up by tests/windows-e2e.sh, dist/overspan,
dist/overspan-viewer.exe and dist/winprobe.exe, nothing else on port 7940,
and a live Hyprland or Sway session (extend mode creates a real monitor).
"""
import glob
import json
import os
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import urllib.request

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
OUT = os.path.abspath(sys.argv[1] if len(sys.argv) > 1 else os.path.join(ROOT, "replica/clone-screens"))
VIEWER = os.path.join(ROOT, "dist/overspan-viewer.exe")
PROBE = os.path.join(ROOT, "dist/winprobe.exe")
SERVER = os.path.join(ROOT, "dist/overspan")
ENV = dict(os.environ, WINEDEBUG="-all", WINEDLLOVERRIDES="mscoree,mshtml=")
for k in ("DISPLAY", "WAYLAND_DISPLAY"):
    ENV.pop(k, None)
fails = 0
KEY = ""


def ok(cond, msg):
    global fails
    print(("PASS  " if cond else "FAIL  ") + msg, flush=True)
    if not cond:
        fails += 1


def probe(*args):
    r = subprocess.run(["wine", PROBE, *map(str, args)], env=ENV, capture_output=True, text=True, timeout=60)
    return r.returncode, r.stdout


def state():
    rc, out = probe("list")
    try:
        return json.loads(out)
    except ValueError:
        return {"title": "", "w": 0, "h": 0, "controls": []}


def control(text, enabled=None):
    t = text.casefold()
    for c in state()["controls"]:
        if c["visible"] and c["text"].casefold().startswith(t) and (enabled is None or c["enabled"] == enabled):
            return c
    return None


def wait(text, timeout=10, enabled=None):
    end = time.time() + timeout
    while time.time() < end:
        c = control(text, enabled)
        if c:
            return c
        time.sleep(0.3)
    return None


def click(text):
    return probe("click", text)[0] == 0


def shot(name):
    probe("shot", "Z:" + os.path.join(OUT, name + ".png"))
    print("saved", name, flush=True)


def api(method, path, body=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request("http://localhost:7940" + path, data=data, method=method,
                                 headers={"X-Overspan-Key": KEY, "Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=5) as r:
        return json.loads(r.read() or b"null")


def viewers(st=None):
    return [v for v in api("GET", "/api/state")["viewers"] if st is None or v["state"] == st]


def wait_viewer(st, timeout=20, frames=False):
    end = time.time() + timeout
    while time.time() < end:
        for v in viewers(st):
            if not frames or v["frames"] > 0:
                return v
        time.sleep(0.4)
    return None


def monitors():
    out = subprocess.run(["hyprctl", "monitors", "-j"], capture_output=True, text=True).stdout
    return json.loads(out or "[]")


def monitor(name):
    return next((m for m in monitors() if m["name"] == name), None)


def cursor():
    return json.loads(subprocess.run(["hyprctl", "cursorpos", "-j"], capture_output=True, text=True).stdout)


def reveal_toolbar():
    probe("mouse", "move", 500, 300)
    probe("mouse", "move", 500, 0)
    return wait("Settings", 3)


def start_viewer():
    probe("close")
    time.sleep(1)
    p = subprocess.Popen(["wine", VIEWER], env=dict(ENV, OVERSPAN_TEST_HOOKS="1"),
                         stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, start_new_session=True)
    wait("Add by address", 20)
    return p


def start_server(cfg):
    global KEY
    log = open(os.path.join(cfg, "server.log"), "a")
    p = subprocess.Popen([SERVER, "serve"], env=dict(os.environ, XDG_CONFIG_HOME=cfg), stdout=log, stderr=log)
    for _ in range(40):
        try:
            html = urllib.request.urlopen("http://localhost:7940/admin", timeout=1).read().decode()
            KEY = html.split('overspan-key" content="')[1].split('"')[0]
            return p
        except Exception:
            time.sleep(0.25)
    raise SystemExit("server did not start")


def stop_server(p):
    p.send_signal(signal.SIGTERM)
    p.wait(20)


def connect_from_list(card_text):
    if not click(card_text):
        return False
    return wait("Connect", 6, enabled=True) is not None and click("Connect")


os.makedirs(OUT, exist_ok=True)
try:
    urllib.request.urlopen("http://localhost:7940/api/info", timeout=1)
    raise SystemExit("something already listens on port 7940; stop it first")
except OSError:
    pass

# a clean viewer: no saved computers, no device id
for f in glob.glob(os.path.join(ENV["WINEPREFIX"], "drive_c/users/*/AppData/Roaming/Overspan/viewer.json")):
    os.remove(f)

viewer = start_viewer()
st = state()
ok(st["title"] == "Overspan" and control("Add by address") is not None, "D01 computer list opens")
time.sleep(1.5)
shot("D01-computers-empty")

cfg = tempfile.mkdtemp(prefix="overspan-e2e-")
server = start_server(cfg)
try:
    # ---- D01: add by address, validation, connect sheet
    click("Add by address")
    ok(wait("Cancel", 6) is not None, "add-by-address sheet opens")
    probe("settext", 0, "not an address")
    click("Add")
    time.sleep(0.4)
    ok(control("Cancel") is not None, "an invalid address keeps the sheet open")
    shot("D01-add-error")
    probe("settext", 0, "127.0.0.1:7940")
    click("Add")
    go = wait("Connect", 6, enabled=True)
    ok(go is not None, "connect sheet reads the computer's status (Ready: Connect enabled)")
    ok(control("Extra monitor") is not None and control("In a window") is not None, "connect sheet offers mode and view")
    shot("D01-connect-sheet")
    click("Connect")

    # ---- D02: approval
    ok(wait("Back", 1) is None, "no error buttons while connecting")
    v = wait_viewer("waiting", 10)
    ok(v is not None and v["agent"] == "Windows", "dashboard shows the request from a Windows device")
    time.sleep(0.5)
    shot("D02-waiting")
    api("POST", f"/api/viewers/{v['id']}/approve?remember=1")

    # ---- D03: picture and input
    live = wait_viewer("live", 25, frames=True)
    ok(live is not None, f"D03 streaming: {live and live['output']} {live and live['width']}x{live and live['height']}")
    st = state()
    ok(live is not None and abs(live["width"] - st["w"]) <= 8 and abs(live["height"] - st["h"]) <= 8,
       f"full screen asks for the screen's size ({st['w']}x{st['h']})")
    time.sleep(1.5)
    shot("D03-display")
    if live:
        mon = monitor(live["output"])
        for px, py in ((st["w"] // 2, st["h"] // 2), (100, st["h"] - 60)):
            probe("mouse", "move", px, py)
            time.sleep(0.4)
            c = cursor()
            ex = mon["x"] + px * mon["width"] / st["w"] / mon["scale"]
            ey = mon["y"] + py * mon["height"] / st["h"] / mon["scale"]
            ok(abs(c["x"] - ex) <= 3 and abs(c["y"] - ey) <= 3,
               f"pointer at {px},{py} on the PC lands at {c['x']},{c['y']} (expected ~{ex:.0f},{ey:.0f})")

    ok(reveal_toolbar() is not None and control("Disconnect") is not None, "the top edge reveals the toolbar")
    shot("D03-toolbar")
    click("Settings")
    ok(wait("Done", 3) is not None, "D04 settings sheet opens")
    click("Show connection stats")
    time.sleep(0.3)
    shot("D04-settings")
    click("Done")
    ok(wait("Done", 0.5) is None, "Done closes the settings")

    reveal_toolbar()
    ok(click("Leave full screen"), "toolbar switches to a window")
    resized = None
    for _ in range(30):
        time.sleep(0.5)
        st = state()
        m = monitor(live["output"]) if live else None
        if m and abs(m["width"] - st["w"]) <= 8 and abs(m["height"] - st["h"]) <= 8:
            resized = m
            break
    ok(resized is not None, f"the monitor follows the window: {resized and resized['width']}x{resized and resized['height']} for a {st['w']}x{st['h']} window")
    shot("D03-window")

    # ---- reconnect after the computer restarts
    stop_server(server)
    time.sleep(1)
    server = start_server(cfg)
    back = wait_viewer("live", 30, frames=True)
    ok(back is not None, "reconnects by itself after the computer restarts")

    # ---- disconnect
    reveal_toolbar()
    click("Disconnect")
    ok(wait("Cancel", 3) is not None, "Disconnect asks for confirmation")
    shot("D03-confirm")
    click("Disconnect")
    ok(wait("Add by address", 8) is not None, "disconnect returns to the computer list")
    ok(any("127.0.0.1:7940" in c["text"] for c in state()["controls"] if c["visible"]), "the computer is now listed (saved)")
    time.sleep(1)
    shot("D01-computers-saved")

    # ---- PIN
    api("POST", "/api/settings", {"pin": "2468"})
    saved = next((c["text"] for c in state()["controls"] if c["visible"] and "127.0.0.1:7940" in c["text"]), "")
    ok(saved and connect_from_list(saved), "connect again from the saved card")
    ok(wait("Continue", 10) is not None, "D02 asks for the PIN")
    probe("settext", 0, "1111")
    click("Continue")
    time.sleep(2)
    ok(wait("Continue", 5) is not None, "a wrong PIN asks again")
    shot("D02-pin-wrong")
    probe("settext", 0, "2468")
    click("Continue")
    ok(wait_viewer("live", 25, frames=True) is not None, "the right PIN connects (trusted device: no approval)")
    reveal_toolbar()
    click("Disconnect")
    wait("Cancel", 3)
    click("Disconnect")
    wait("Add by address", 8)
    api("POST", "/api/settings", {"pin": ""})

    # ---- paused computer
    api("POST", "/api/settings", {"enabled": False})
    for c in state()["controls"]:
        if c["visible"] and "127.0.0.1" in c["text"]:
            click(c["text"])
            break
    time.sleep(2)
    ok(control("Connect", enabled=False) is not None, "a paused computer cannot be connected to")
    shot("D01-connect-paused")
    click("Cancel")
    api("POST", "/api/settings", {"enabled": True})

    # ---- long names and a narrow window
    api("POST", "/api/settings", {"serverName": "A computer with a deliberately long name for layout tests ok"})
    probe("size", 600, 560)
    time.sleep(4)
    shot("D01-narrow-long-name")
    api("POST", "/api/settings", {"serverName": ""})
finally:
    probe("close")
    try:
        viewer.wait(10)
    except subprocess.TimeoutExpired:
        viewer.kill()
    for t in api("GET", "/api/state")["settings"]["trusted"]:
        api("POST", f"/api/trusted/{t['deviceId']}/forget")
    stop_server(server)
    left = []
    for _ in range(20):
        left = [m["name"] for m in monitors() if m["name"].startswith("OVERSPAN")]
        if not left:
            break
        time.sleep(0.5)
    ok(not left, f"no virtual monitors left behind {left or ''}")
    print("--- server log")
    with open(os.path.join(cfg, "server.log")) as f:
        for line in f:
            if any(w in line for w in ("connected", "Created", "Removed", "Resized", "warn", "error")):
                print(line.rstrip())
    shutil.rmtree(cfg, ignore_errors=True)

print("\nall checks passed" if not fails else f"\n{fails} check(s) failed")
sys.exit(1 if fails else 0)
