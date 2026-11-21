package app.overspan.viewer;

import android.util.Base64;

import java.io.BufferedInputStream;
import java.io.ByteArrayOutputStream;
import java.io.DataInputStream;
import java.io.IOException;
import java.io.OutputStream;
import java.net.InetSocketAddress;
import java.net.Socket;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.SecureRandom;
import java.util.concurrent.LinkedBlockingQueue;

/**
 * Minimal RFC 6455 WebSocket client. One reader thread delivers messages;
 * one writer thread sends them, so callers may send from any thread,
 * including the UI thread.
 */
final class WsClient {
    interface Listener {
        void onOpen();
        void onText(String text);
        void onBinary(byte[] data);
        void onClosed(String reason);
    }

    private static final byte[] CLOSE_SENTINEL = new byte[0];
    private final SecureRandom random = new SecureRandom();
    private final LinkedBlockingQueue<byte[]> outbox = new LinkedBlockingQueue<>();
    private volatile Socket socket;
    private volatile boolean closed;

    void connect(String host, int port, String userAgent, Listener l) {
        Thread reader = new Thread(() -> run(host, port, userAgent, l), "ws-reader");
        reader.setDaemon(true);
        reader.start();
    }

    private void run(String host, int port, String userAgent, Listener l) {
        String reason = "closed";
        try {
            Socket s = new Socket();
            socket = s;
            s.connect(new InetSocketAddress(host, port), 6000);
            s.setTcpNoDelay(true);
            OutputStream out = s.getOutputStream();
            DataInputStream in = new DataInputStream(new BufferedInputStream(s.getInputStream(), 1 << 16));

            byte[] nonce = new byte[16];
            random.nextBytes(nonce);
            String key = Base64.encodeToString(nonce, Base64.NO_WRAP);
            String req = "GET /ws HTTP/1.1\r\nHost: " + host + ":" + port
                    + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: " + key
                    + "\r\nSec-WebSocket-Version: 13\r\nUser-Agent: " + userAgent + "\r\n\r\n";
            out.write(req.getBytes(StandardCharsets.US_ASCII));
            out.flush();

            String status = readLine(in);
            if (status == null || !status.contains(" 101")) throw new IOException("server refused the upgrade: " + status);
            String accept = null;
            for (String line; (line = readLine(in)) != null && !line.isEmpty(); ) {
                int c = line.indexOf(':');
                if (c > 0 && line.substring(0, c).trim().equalsIgnoreCase("Sec-WebSocket-Accept")) accept = line.substring(c + 1).trim();
            }
            if (!expectedAccept(key).equals(accept)) throw new IOException("bad handshake");

            Thread writer = new Thread(() -> writeLoop(out), "ws-writer");
            writer.setDaemon(true);
            writer.start();
            l.onOpen();

            ByteArrayOutputStream partial = new ByteArrayOutputStream();
            int partialOp = 0;
            while (!closed) {
                int b0 = in.readUnsignedByte();
                int b1 = in.readUnsignedByte();
                boolean fin = (b0 & 0x80) != 0;
                int op = b0 & 0x0f;
                long len = b1 & 0x7f;
                if (len == 126) len = in.readUnsignedShort();
                else if (len == 127) len = in.readLong();
                if (len > (64L << 20)) throw new IOException("frame too large");
                byte[] mask = null;
                if ((b1 & 0x80) != 0) { mask = new byte[4]; in.readFully(mask); }
                byte[] payload = new byte[(int) len];
                in.readFully(payload);
                if (mask != null) for (int i = 0; i < payload.length; i++) payload[i] ^= mask[i & 3];
                switch (op) {
                    case 0x9: enqueue(0xA, payload); continue;
                    case 0xA: continue;
                    case 0x8: reason = "closed by server"; closed = true; continue;
                    case 0x0: partial.write(payload); break;
                    default: partial.reset(); partial.write(payload); partialOp = op;
                }
                if (!fin) continue;
                byte[] msg = partial.toByteArray();
                partial.reset();
                if (partialOp == 1) l.onText(new String(msg, StandardCharsets.UTF_8));
                else if (partialOp == 2) l.onBinary(msg);
            }
        } catch (Exception e) {
            if (!closed) reason = e.getMessage() == null ? e.toString() : e.getMessage();
        } finally {
            closed = true;
            outbox.offer(CLOSE_SENTINEL);
            try { if (socket != null) socket.close(); } catch (IOException ignored) { }
            l.onClosed(reason);
        }
    }

    private static String readLine(DataInputStream in) throws IOException {
        StringBuilder sb = new StringBuilder();
        for (int c; (c = in.read()) != -1; ) {
            if (c == '\n') return sb.toString().trim();
            sb.append((char) c);
            if (sb.length() > 8192) throw new IOException("header too long");
        }
        return sb.length() > 0 ? sb.toString() : null;
    }

    private static String expectedAccept(String key) throws Exception {
        MessageDigest sha1 = MessageDigest.getInstance("SHA-1");
        byte[] d = sha1.digest((key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").getBytes(StandardCharsets.US_ASCII));
        return Base64.encodeToString(d, Base64.NO_WRAP);
    }

    private void writeLoop(OutputStream out) {
        try {
            while (true) {
                byte[] frame = outbox.take();
                if (frame == CLOSE_SENTINEL) break;
                out.write(frame);
                if (outbox.isEmpty()) out.flush();
            }
        } catch (Exception ignored) {
            closed = true;
            try { if (socket != null) socket.close(); } catch (IOException e) { /* already closing */ }
        }
    }

    private void enqueue(int op, byte[] payload) {
        if (closed && op != 0x8) return;
        int n = payload.length;
        int hdr = n < 126 ? 2 : n < 65536 ? 4 : 10;
        byte[] f = new byte[hdr + 4 + n];
        f[0] = (byte) (0x80 | op);
        if (n < 126) f[1] = (byte) (0x80 | n);
        else if (n < 65536) { f[1] = (byte) (0x80 | 126); f[2] = (byte) (n >> 8); f[3] = (byte) n; }
        else { f[1] = (byte) (0x80 | 127); for (int i = 0; i < 8; i++) f[2 + i] = (byte) ((long) n >> (56 - 8 * i)); }
        byte[] mask = new byte[4];
        random.nextBytes(mask);
        System.arraycopy(mask, 0, f, hdr, 4);
        for (int i = 0; i < n; i++) f[hdr + 4 + i] = (byte) (payload[i] ^ mask[i & 3]);
        outbox.offer(f);
    }

    void send(String text) { enqueue(0x1, text.getBytes(StandardCharsets.UTF_8)); }

    boolean isOpen() { return !closed && socket != null; }

    void close() {
        if (closed) return;
        enqueue(0x8, new byte[]{0x03, (byte) 0xe8});
        closed = true;
        outbox.offer(CLOSE_SENTINEL);
        new Thread(() -> {
            try { Thread.sleep(200); if (socket != null) socket.close(); } catch (Exception ignored) { }
        }).start();
    }
}
