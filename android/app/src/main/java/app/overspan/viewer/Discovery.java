package app.overspan.viewer;

import org.json.JSONObject;

import java.net.DatagramPacket;
import java.net.DatagramSocket;
import java.net.InetAddress;
import java.net.InterfaceAddress;
import java.net.NetworkInterface;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.CopyOnWriteArrayList;

/**
 * Finds Overspan computers on the local network: broadcasts "OVERSPAN?" to
 * UDP 7941 on every interface and collects the JSON cards that come back.
 */
final class Discovery {
    interface Listener { void onFound(Computer c); }

    static final int PORT = 7941;
    private volatile boolean running;
    private DatagramSocket socket;
    private final List<String> direct = new CopyOnWriteArrayList<>();

    /**
     * Also probe this host directly. Saved computers are probed this way, which
     * still works on networks or firewalls that drop broadcasts.
     */
    void addHost(String host) { if (!direct.contains(host)) direct.add(host); }

    void start(Listener l) {
        if (running) return;
        running = true;
        Thread t = new Thread(() -> loop(l), "discovery");
        t.setDaemon(true);
        t.start();
    }

    void stop() {
        running = false;
        if (socket != null) socket.close();
    }

    private void loop(Listener l) {
        try (DatagramSocket s = new DatagramSocket()) {
            socket = s;
            s.setBroadcast(true);
            s.setSoTimeout(500);
            byte[] probe = "OVERSPAN?".getBytes(StandardCharsets.US_ASCII);
            byte[] buf = new byte[2048];
            long nextProbe = 0;
            while (running) {
                long now = System.currentTimeMillis();
                if (now >= nextProbe) {
                    List<InetAddress> all = targets();
                    for (String h : direct) {
                        try { all.add(InetAddress.getByName(h)); } catch (Exception ignored) { }
                    }
                    for (InetAddress target : all) {
                        try { s.send(new DatagramPacket(probe, probe.length, target, PORT)); } catch (Exception ignored) { }
                    }
                    nextProbe = now + 2500;
                }
                try {
                    DatagramPacket p = new DatagramPacket(buf, buf.length);
                    s.receive(p);
                    JSONObject j = new JSONObject(new String(p.getData(), 0, p.getLength(), StandardCharsets.UTF_8));
                    if (!"Overspan".equals(j.optString("product"))) continue;
                    Computer c = new Computer(p.getAddress().getHostAddress(), j.optInt("port", 7940), j.optString("name", ""));
                    c.enabled = j.optBoolean("enabled", true);
                    c.version = j.optString("version", "");
                    c.found = true;
                    c.seenAt = System.currentTimeMillis();
                    l.onFound(c);
                } catch (java.net.SocketTimeoutException ignored) {
                    // no reply this round
                } catch (Exception e) {
                    if (!running) return;
                }
            }
        } catch (Exception ignored) {
            running = false;
        }
    }

    private static List<InetAddress> targets() {
        List<InetAddress> out = new ArrayList<>();
        try {
            for (NetworkInterface ni : Collections.list(NetworkInterface.getNetworkInterfaces())) {
                if (!ni.isUp() || ni.isLoopback()) continue;
                for (InterfaceAddress a : ni.getInterfaceAddresses()) {
                    if (a.getBroadcast() != null) out.add(a.getBroadcast());
                }
            }
            out.add(InetAddress.getByName("255.255.255.255"));
        } catch (Exception ignored) { }
        return out;
    }
}
