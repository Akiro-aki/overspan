package app.overspan.viewer;

import android.content.Context;
import android.content.SharedPreferences;
import android.os.Build;

import org.json.JSONArray;
import org.json.JSONObject;

import java.security.SecureRandom;
import java.util.ArrayList;
import java.util.List;

/** Viewer preferences and saved computers. */
final class Prefs {
    private final SharedPreferences p;

    Prefs(Context c) { p = c.getSharedPreferences("overspan", Context.MODE_PRIVATE); }

    String deviceId() {
        String id = p.getString("device", "");
        if (id.length() < 16) {
            byte[] b = new byte[16];
            new SecureRandom().nextBytes(b);
            StringBuilder sb = new StringBuilder();
            for (byte x : b) sb.append(String.format("%02x", x));
            id = sb.toString();
            p.edit().putString("device", id).apply();
        }
        return id;
    }

    String deviceName() {
        String n = p.getString("name", "");
        if (!n.isEmpty()) return n;
        String model = Build.MODEL == null ? "" : Build.MODEL;
        String maker = Build.MANUFACTURER == null ? "" : Build.MANUFACTURER;
        if (!model.toLowerCase().startsWith(maker.toLowerCase()) && !maker.isEmpty()) {
            model = Character.toUpperCase(maker.charAt(0)) + maker.substring(1) + " " + model;
        }
        return model.isEmpty() ? "Android device" : model;
    }

    void setDeviceName(String n) { p.edit().putString("name", n).apply(); }

    String mode() { return p.getString("mode", "extend"); }
    void setMode(String m) { p.edit().putString("mode", m).apply(); }
    String res() { return p.getString("res", "balanced"); }
    void setRes(String r) { p.edit().putString("res", r).apply(); }
    int quality() { return p.getInt("quality", 70); }
    void setQuality(int q) { p.edit().putInt("quality", q).apply(); }
    int fps() { return p.getInt("fps", 30); }
    void setFps(int f) { p.edit().putInt("fps", f).apply(); }
    boolean trackpad() { return p.getBoolean("trackpad", false); }
    void setTrackpad(boolean t) { p.edit().putBoolean("trackpad", t).apply(); }
    boolean stats() { return p.getBoolean("stats", false); }
    void setStats(boolean s) { p.edit().putBoolean("stats", s).apply(); }
    boolean rotate() { return p.getBoolean("rotate", true); }
    void setRotate(boolean r) { p.edit().putBoolean("rotate", r).apply(); }
    boolean seenHint() { return p.getBoolean("seenHint", false); }
    void setSeenHint() { p.edit().putBoolean("seenHint", true).apply(); }

    List<Computer> saved() {
        List<Computer> out = new ArrayList<>();
        try {
            JSONArray a = new JSONArray(p.getString("saved", "[]"));
            for (int i = 0; i < a.length(); i++) {
                JSONObject o = a.getJSONObject(i);
                Computer c = new Computer(o.getString("host"), o.optInt("port", 7940), o.optString("name", ""));
                c.saved = true;
                out.add(c);
            }
        } catch (Exception ignored) { }
        return out;
    }

    void save(Computer c) {
        List<Computer> list = saved();
        JSONArray a = new JSONArray();
        try {
            a.put(new JSONObject().put("host", c.host).put("port", c.port).put("name", c.name == null ? "" : c.name));
            for (Computer o : list) if (!o.key().equals(c.key()) && a.length() < 12) {
                a.put(new JSONObject().put("host", o.host).put("port", o.port).put("name", o.name));
            }
        } catch (Exception ignored) { }
        p.edit().putString("saved", a.toString()).apply();
    }

    void forget(Computer c) {
        JSONArray a = new JSONArray();
        try {
            for (Computer o : saved()) if (!o.key().equals(c.key())) {
                a.put(new JSONObject().put("host", o.host).put("port", o.port).put("name", o.name));
            }
        } catch (Exception ignored) { }
        p.edit().putString("saved", a.toString()).apply();
    }
}
