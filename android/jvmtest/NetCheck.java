package app.overspan.viewer;

import java.net.HttpURLConnection;
import java.net.URL;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;

/**
 * Runs the app's real Discovery, Computer and WsClient classes against a
 * running server on this machine:  java NetCheck <adminKey>
 */
public final class NetCheck {
    static int fails = 0;
    static void ok(boolean c, String m) { System.out.println((c ? "PASS  " : "FAIL  ") + m); if (!c) fails++; }

    public static void main(String[] a) throws Exception {
        String key = a[0];
        String mode = a.length > 1 ? a[1] : "mirror";
        // parse
        ok(Computer.parse("192.168.1.5").port == 7940, "parse: bare host gets port 7940");
        ok(Computer.parse("http://10.0.0.2:8000/").port == 8000, "parse: pasted URL with port");
        ok(Computer.parse("bad host!") == null, "parse: rejects garbage");

        // discovery
        CountDownLatch found = new CountDownLatch(1);
        AtomicReference<Computer> hit = new AtomicReference<>();
        Discovery d = new Discovery();
        d.addHost("127.0.0.1"); // this machine's firewall drops broadcasts; test the reply path directly
        d.start(c -> { hit.set(c); found.countDown(); });
        ok(found.await(6, TimeUnit.SECONDS), "discovery found a computer: " + (hit.get() == null ? "-" : hit.get().title() + " " + hit.get().key()));
        d.stop();

        // websocket session in mirror mode
        WsClient ws = new WsClient();
        CountDownLatch ready = new CountDownLatch(1), frame = new CountDownLatch(1), waiting = new CountDownLatch(1), asleep = new CountDownLatch(1);
        StringBuilder log = new StringBuilder();
        ws.connect("127.0.0.1", 7940, "Overspan-Android/0.1.0 (Android 15; JVM test)", new WsClient.Listener() {
            public void onOpen() {
                ws.send("{\"t\":\"hello\",\"device\":\"jvmtest0123456789\",\"name\":\"JVM check\",\"mode\":\"" + mode + "\",\"width\":1280,\"height\":800,\"scale\":1}");
            }
            public void onText(String t) {
                log.append(t).append('\n');
                if (t.contains("\"t\":\"wait\"")) waiting.countDown();
                if (t.contains("\"t\":\"ready\"")) ready.countDown();
                if (t.contains("\"code\":\"asleep\"")) asleep.countDown();
            }
            public void onBinary(byte[] b) {
                long seq = (b[2] & 0xff) | (b[3] & 0xff) << 8 | (b[4] & 0xff) << 16 | (long) (b[5] & 0xff) << 24;
                ws.send("{\"t\":\"ack\",\"sq\":" + seq + "}");
                frame.countDown();
            }
            public void onClosed(String r) { log.append("closed: ").append(r).append('\n'); }
        });
        ok(waiting.await(5, TimeUnit.SECONDS), "handshake + hello: asked to wait for approval");
        String state = http("GET", "/api/state", key);
        java.util.regex.Matcher m = java.util.regex.Pattern.compile("\"id\":\"([0-9a-f]+)\",\"deviceId\":\"jvmtest0123456789\"").matcher(state);
        ok(m.find(), "dashboard shows the JVM viewer");
        ok(state.contains("\"agent\":\"Android\""), "server recognises the app as Android");
        http("POST", "/api/viewers/" + m.group(1) + "/approve", key);
        ok(ready.await(20, TimeUnit.SECONDS), "approved: ready");
        boolean gotFrame = frame.await(7, TimeUnit.SECONDS);
        if (gotFrame) ok(true, mode + ": received frames and acked them");
        else ok(asleep.getCount() == 0, mode + ": no frames, and the server explained the screen is asleep");
        ws.send("{\"t\":\"bye\"}");
        ws.close();
        Thread.sleep(500);
        if (fails > 0) System.out.println(log);
        System.out.println(fails == 0 ? "\nall checks passed" : "\n" + fails + " failed");
        System.exit(fails == 0 ? 0 : 1);
    }

    static String http(String method, String path, String key) throws Exception {
        HttpURLConnection h = (HttpURLConnection) new URL("http://localhost:7940" + path).openConnection();
        h.setRequestMethod(method);
        h.setRequestProperty("X-Overspan-Key", key);
        if (method.equals("POST")) { h.setDoOutput(true); h.getOutputStream().close(); }
        return new String(h.getInputStream().readAllBytes());
    }
}
