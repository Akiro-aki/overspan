#!/bin/sh
# Builds the Overspan Android viewer with the plain SDK tools (no Gradle, no
# third-party libraries): aapt2 -> javac -> d8 -> zipalign -> apksigner.
#
#   ./build.sh            debug-signed APK at build/overspan-viewer.apk
#
# Needs: a JDK (17 or newer), ANDROID_HOME (default ~/Android/Sdk) with
# platforms;android-35 and build-tools;35.0.0.
set -eu
cd "$(dirname "$0")"
SDK=${ANDROID_HOME:-$HOME/Android/Sdk}
BT=$SDK/build-tools/35.0.0
JAR=$SDK/platforms/android-35/android.jar
SRC=app/src/main
OUT=build
for f in "$BT/aapt2" "$BT/d8" "$BT/zipalign" "$BT/apksigner" "$JAR"; do
  [ -e "$f" ] || { echo "missing $f (install build-tools 35.0.0 and platform 35)" >&2; exit 1; }
done

rm -rf ./build
mkdir -p $OUT/gen $OUT/classes $OUT/dex

echo "resources"
"$BT/aapt2" compile --dir $SRC/res -o $OUT/res.zip
"$BT/aapt2" link -I "$JAR" --manifest $SRC/AndroidManifest.xml --java $OUT/gen \
  --min-sdk-version 26 --target-sdk-version 35 -o $OUT/unsigned.apk $OUT/res.zip

echo "java"
find $SRC/java $OUT/gen -name '*.java' > $OUT/sources.txt
javac --release 17 -encoding UTF-8 -Xlint:-options -nowarn -classpath "$JAR" -d $OUT/classes @$OUT/sources.txt

echo "dex"
find $OUT/classes -name '*.class' > $OUT/classes.txt
"$BT/d8" --release --min-api 26 --lib "$JAR" --output $OUT/dex @$OUT/classes.txt
(cd $OUT/dex && zip -q -j ../unsigned.apk classes.dex)

echo "sign"
KS=${OVERSPAN_KEYSTORE:-keystore/debug.keystore}
if [ ! -f "$KS" ]; then
  mkdir -p "$(dirname "$KS")"
  keytool -genkeypair -keystore "$KS" -storepass android -keypass android -alias overspan \
    -keyalg RSA -keysize 2048 -validity 10000 -dname "CN=Overspan Debug" >/dev/null 2>&1
fi
"$BT/zipalign" -p -f 4 $OUT/unsigned.apk $OUT/aligned.apk
"$BT/apksigner" sign --ks "$KS" --ks-pass pass:${OVERSPAN_KS_PASS:-android} --ks-key-alias overspan \
  --out $OUT/overspan-viewer.apk $OUT/aligned.apk
"$BT/apksigner" verify $OUT/overspan-viewer.apk
ls -l $OUT/overspan-viewer.apk
