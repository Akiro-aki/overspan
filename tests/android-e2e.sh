#!/bin/sh
# Boots a headless Android emulator, installs the viewer APK and runs
# tests/android_e2e.py against a throwaway Overspan server on this machine.
# Needs ANDROID_HOME with emulator + system-images;android-35;default;x86_64.
set -u
here=$(cd "$(dirname "$0")/.." && pwd)
export ANDROID_HOME=${ANDROID_HOME:-$HOME/Android/Sdk}
export ANDROID_SDK_ROOT=$ANDROID_HOME
export ANDROID_AVD_HOME=${ANDROID_AVD_HOME:-$HOME/Android/avd}
avd=overspan_test
img=system-images/android-35/default/x86_64

if [ ! -f "$ANDROID_AVD_HOME/$avd.avd/config.ini" ]; then
  mkdir -p "$ANDROID_AVD_HOME/$avd.avd"
  printf 'avd.ini.encoding=UTF-8\npath=%s\ntarget=android-35\n' "$ANDROID_AVD_HOME/$avd.avd" > "$ANDROID_AVD_HOME/$avd.ini"
  cat > "$ANDROID_AVD_HOME/$avd.avd/config.ini" <<EOF
AvdId=$avd
avd.ini.displayname=Overspan test phone
abi.type=x86_64
hw.cpu.arch=x86_64
hw.cpu.ncore=2
image.sysdir.1=$img/
tag.id=default
hw.lcd.width=1080
hw.lcd.height=2400
hw.lcd.density=420
hw.ramSize=2048
hw.keyboard=yes
hw.gpu.enabled=yes
hw.gpu.mode=swiftshader_indirect
disk.dataPartition.size=2G
EOF
fi

if ! adb devices | grep -q "emulator-.*device$"; then
  echo "booting emulator (headless)"
  "$ANDROID_HOME/emulator/emulator" -avd $avd -no-window -no-audio -no-boot-anim -no-snapshot \
    -gpu swiftshader_indirect >"${TMPDIR:-/tmp}/overspan-emulator.log" 2>&1 &
  emu=$!
  waited=0
  until [ "$(adb shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')" = "1" ]; do
    if ! kill -0 $emu 2>/dev/null; then
      echo "emulator exited:"; tail -5 "${TMPDIR:-/tmp}/overspan-emulator.log"; exit 1
    fi
    waited=$((waited + 2))
    [ $waited -gt 300 ] && { echo "emulator did not boot in 5 minutes"; exit 1; }
    sleep 2
  done
  adb shell settings put global window_animation_scale 0
  adb shell settings put global transition_animation_scale 0
  adb shell settings put global animator_duration_scale 0
fi
adb install -r "$here/android/build/overspan-viewer.apk" | tail -1

cfg=$(mktemp -d)
XDG_CONFIG_HOME="$cfg" "$here/dist/overspan" serve >"$cfg/server.log" 2>&1 &
pid=$!
sleep 1
key=$(curl -s localhost:7940/admin | grep -o 'overspan-key" content="[0-9a-f]*' | grep -o '[0-9a-f]*$')
python3 -I "$here/tests/android_e2e.py" "$key" "$here/replica/clone-screens"
rc=$?
kill $pid; wait $pid 2>/dev/null
echo "--- server log"; grep -E "connected|Created|Removed|warn|error" "$cfg/server.log"
exit $rc
