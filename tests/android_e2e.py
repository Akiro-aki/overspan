#!/usr/bin/env python3
"""Drives the Overspan Android app in an emulator against a server on this
machine (the emulator reaches the host at 10.0.2.2), through the real UI.

    python3 tests/android_e2e.py <adminKey> [outdir]

Needs: adb on PATH, one emulator booted, the APK installed, a server on 7940.
"""
import json
import re
import subprocess
import sys
import time
import urllib.request
import xml.etree.ElementTree as ET

KEY = sys.argv[1]
OUT = sys.argv[2] if len(sys.argv) > 2 else "replica/clone-screens"
PKG = "app.overspan.viewer"
fails = 0


def ok(cond, msg):
    global fails
    print(("PASS  " if cond else "FAIL  ") + msg, flush=True)
    if not cond:
        fails += 1


def adb(*args, binary=False):
    r = subprocess.run(["adb", *args], capture_output=True, timeout=60)
    return r.stdout if binary else r.stdout.decode(errors="replace")


def api(method, path):
    req = urllib.request.Request("http://localhost:7940" + path, method=method, headers={"X-Overspan-Key": KEY})
    with urllib.request.urlopen(req, timeout=5) as r:
        return json.loads(r.read() or b"null")


def ui_nodes():
    adb("shell", "rm", "-f", "/sdcard/ui.xml")  # never read a stale dump
    res = adb("shell", "uiautomator", "dump", "/sdcard/ui.xml")
    if "ERROR" in res:
        print("   (ui dump: " + res.strip()[:80] + ")", flush=True)
        return []
    xml = adb("shell", "cat", "/sdcard/ui.xml")
    try:
        return list(ET.fromstring(xml[xml.index("<"):]).iter("node"))
    except Exception:
        return []


def find(text=None, desc=None, rid=None, timeout=15):
    end = time.time() + timeout
    while time.time() < end:
        for n in ui_nodes():
            if text and n.get("text", "").strip().casefold() != text.casefold():
                continue
            if desc and n.get("content-desc", "") != desc:
                continue
            if rid and not n.get("resource-id", "").endswith(":id/" + rid):
                continue
            return n
        time.sleep(0.7)
    return None


def center(n):
    x1, y1, x2, y2 = map(int, re.findall(r"\d+", n.get("bounds")))
    return (x1 + x2) // 2, (y1 + y2) // 2


def tap(n):
    x, y = center(n)
    adb("shell", "input", "tap", str(x), str(y))


def shot(name):
    png = adb("exec-out", "screencap", "-p", binary=True)
    with open(f"{OUT}/{name}.png", "wb") as f:
        f.write(png)
    print("saved", name, flush=True)


def hypr_cursor():
    return json.loads(subprocess.run(["hyprctl", "cursorpos", "-j"], capture_output=True, text=True).stdout)


def hypr_monitor(name):
    ms = json.loads(subprocess.run(["hyprctl", "monitors", "-j"], capture_output=True, text=True).stdout)
    return next((m for m in ms if m["name"] == name), None)


adb("shell", "pm", "clear", PKG)
# Android shows a one-time "Viewing full screen" notice over immersive apps; a
# person taps "Got it" once. Acknowledge it up front so it cannot steal focus.
adb("shell", "settings", "put", "secure", "immersive_mode_confirmations", "confirmed")
adb("shell", "am", "start", "-n", f"{PKG}/.MainActivity")
ok(find(text="Computers") is not None, "C01 computer list opens")
time.sleep(2)
shot("C01-computers-empty")

tap(find(text="Add by address"))
field = find(rid="a_addr")
ok(field is not None, "add-by-address dialog opens")
tap(field)
adb("shell", "input", "text", "not%san%saddress")
tap(find(text="Add"))
ok(find(rid="a_error", timeout=4) is not None, "invalid address shows an error")
adb("shell", "input", "keyevent", "KEYCODE_MOVE_END")
for _ in range(20):
    adb("shell", "input", "keyevent", "KEYCODE_DEL")
adb("shell", "input", "text", "10.0.2.2:7940")
tap(find(text="Add"))

status = find(rid="c_status")
ok(status is not None, "connect sheet opens")
ready = find(text="Ready", timeout=8)
ok(ready is not None, "connect sheet reads the computer's status (Ready)")
shot("C01-connect-sheet")
tap(find(text="Connect"))

ok(find(text="Waiting for approval", timeout=10) is not None, "C02 waiting for approval")
shot("C02-waiting")
st = api("GET", "/api/state")
v = next((x for x in st["viewers"] if x["state"] == "waiting"), None)
ok(v is not None and v["agent"] == "Android", "dashboard shows the request from an Android device")
api("POST", f"/api/viewers/{v['id']}/approve?remember=1")

live = None
for _ in range(40):
    st = api("GET", "/api/state")
    live = next((x for x in st["viewers"] if x["state"] == "live" and x["frames"] > 0), None)
    if live:
        break
    time.sleep(0.5)
ok(live is not None, f"C03 streaming: {live and live['output']} {live and live['width']}x{live and live['height']}")
time.sleep(2)
shot("C03-display")

if live:
    mon = hypr_monitor(live["output"])
    # the display fills the emulator screen; tap its centre
    size = re.findall(r"(\d+)x(\d+)", adb("shell", "wm", "size"))[-1]
    sw, sh = int(size[0]), int(size[1])
    adb("shell", "input", "tap", str(sw // 2), str(sh // 2))
    time.sleep(0.5)
    c = hypr_cursor()
    ex, ey = mon["x"] + mon["width"] / mon["scale"] / 2, mon["y"] + mon["height"] / mon["scale"] / 2
    ok(abs(c["x"] - ex) <= 6 and abs(c["y"] - ey) <= 6, f"tap on the phone moved the pointer to {c['x']},{c['y']} (expected ~{ex:.0f},{ey:.0f})")

adb("shell", "input", "keyevent", "KEYCODE_BACK")
settings = find(desc="Settings", timeout=5)
ok(settings is not None, "Back shows the toolbar")
shot("C03-toolbar")
tap(settings)  # tap right away: the toolbar hides itself after 6 s
ok(find(text="Picture and input", timeout=5) is not None, "C04 settings dialog opens")
shot("C04-settings")
tap(find(text="Done"))
time.sleep(0.5)

power = find(desc="Disconnect", timeout=4)  # the toolbar is still up after Settings
if power is None:
    adb("shell", "input", "keyevent", "KEYCODE_BACK")
    power = find(desc="Disconnect", timeout=4)
if power is not None:
    tap(power)
yes = find(text="Disconnect", timeout=5)
ok(yes is not None, "the toolbar's Disconnect button asks for confirmation")
if yes is not None:
    tap(yes)
ok(find(text="Computers", timeout=8) is not None, "disconnect returns to the computer list")
time.sleep(1.5)
saved = find(text="Saved", timeout=4)
ok(saved is not None or find(text="Ready", timeout=2) is not None, "the computer is now listed (saved)")
shot("C01-computers-saved")

for t in api("GET", "/api/state")["settings"]["trusted"]:
    api("POST", f"/api/trusted/{t['deviceId']}/forget")
print("\nall checks passed" if not fails else f"\n{fails} check(s) failed")
sys.exit(1 if fails else 0)
