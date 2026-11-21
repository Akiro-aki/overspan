package app.overspan.viewer;

import android.app.Activity;
import android.app.AlertDialog;
import android.content.Intent;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.view.LayoutInflater;
import android.view.View;
import android.widget.Button;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.RadioButton;
import android.widget.RadioGroup;
import android.widget.TextView;

import org.json.JSONObject;

import java.io.ByteArrayOutputStream;
import java.io.InputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/** C01: the list of computers (found on the network and saved) plus the connect sheet. */
public final class MainActivity extends Activity {
    private final Handler ui = new Handler(Looper.getMainLooper());
    private final Map<String, Computer> found = new LinkedHashMap<>();
    private Prefs prefs;
    private Discovery discovery;
    private LinearLayout list;
    private View empty, scanning;
    private TextView scanStatus;
    private long scanStarted;

    private final Runnable prune = new Runnable() {
        @Override public void run() {
            long now = System.currentTimeMillis();
            found.values().removeIf(c -> now - c.seenAt > 9000);
            boolean early = now - scanStarted < 6000;
            scanning.setVisibility(early ? View.VISIBLE : View.INVISIBLE);
            render();
            ui.postDelayed(this, 3000);
        }
    };

    @Override
    protected void onCreate(Bundle b) {
        super.onCreate(b);
        setContentView(R.layout.activity_main);
        prefs = new Prefs(this);
        list = findViewById(R.id.list);
        empty = findViewById(R.id.empty);
        scanning = findViewById(R.id.scanning);
        scanStatus = findViewById(R.id.scan_status);
        findViewById(R.id.btn_add).setOnClickListener(v -> showAddDialog());
        findViewById(R.id.btn_refresh).setOnClickListener(v -> { found.clear(); restartScan(); });
    }

    @Override
    protected void onResume() {
        super.onResume();
        restartScan();
    }

    @Override
    protected void onPause() {
        super.onPause();
        if (discovery != null) discovery.stop();
        ui.removeCallbacks(prune);
    }

    private void restartScan() {
        if (discovery != null) discovery.stop();
        discovery = new Discovery();
        for (Computer s : prefs.saved()) discovery.addHost(s.host);
        scanStarted = System.currentTimeMillis();
        discovery.start(c -> ui.post(() -> {
            Computer old = found.get(c.key());
            found.put(c.key(), c);
            if (old == null) render();
        }));
        ui.removeCallbacks(prune);
        prune.run();
    }

    private void render() {
        List<Computer> all = new ArrayList<>(found.values());
        List<Computer> saved = prefs.saved();
        for (Computer s : saved) {
            Computer f = found.get(s.key());
            if (f != null) f.saved = true;
            else all.add(s);
        }
        int nFound = found.size();
        scanStatus.setText(nFound == 0 ? getString(R.string.searching)
                : getResources().getQuantityString(R.plurals.found_count, nFound, nFound));
        list.removeAllViews();
        LayoutInflater inf = LayoutInflater.from(this);
        for (Computer c : all) {
            View row = inf.inflate(R.layout.item_server, list, false);
            ((TextView) row.findViewById(R.id.name)).setText(c.title());
            ((TextView) row.findViewById(R.id.addr)).setText(c.key());
            TextView badge = row.findViewById(R.id.badge);
            if (c.found && c.enabled) styleBadge(badge, R.string.status_ready, R.drawable.bg_badge_ok, R.color.ok);
            else if (c.found) styleBadge(badge, R.string.status_paused, R.drawable.bg_badge_warn, R.color.warn);
            else styleBadge(badge, R.string.status_saved, R.drawable.bg_badge_plain, R.color.text_muted);
            row.setContentDescription(c.title() + ", " + c.key() + ", " + badge.getText());
            row.setOnClickListener(v -> openConnect(c));
            list.addView(row);
        }
        empty.setVisibility(all.isEmpty() ? View.VISIBLE : View.GONE);
    }

    private void styleBadge(TextView t, int text, int bg, int color) {
        t.setText(text);
        t.setBackgroundResource(bg);
        t.setTextColor(getColor(color));
    }

    private void showAddDialog() {
        View v = LayoutInflater.from(this).inflate(R.layout.dialog_address, null);
        EditText addr = v.findViewById(R.id.a_addr);
        TextView err = v.findViewById(R.id.a_error);
        AlertDialog d = new AlertDialog.Builder(this)
                .setTitle(R.string.address_title)
                .setView(v)
                .setPositiveButton(R.string.add, null)
                .setNegativeButton(R.string.cancel, null)
                .create();
        d.setOnShowListener(x -> d.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener(btn -> {
            Computer c = Computer.parse(addr.getText().toString());
            if (c == null) {
                err.setVisibility(View.VISIBLE);
                addr.requestFocus();
                return;
            }
            d.dismiss();
            openConnect(c);
        }));
        d.show();
        addr.requestFocus();
    }

    private void openConnect(Computer c) {
        View v = LayoutInflater.from(this).inflate(R.layout.dialog_connect, null);
        TextView name = v.findViewById(R.id.c_name);
        TextView status = v.findViewById(R.id.c_status);
        RadioGroup mode = v.findViewById(R.id.c_mode);
        RadioButton extend = v.findViewById(R.id.c_extend);
        RadioButton mirror = v.findViewById(R.id.c_mirror);
        TextView hint = v.findViewById(R.id.c_mode_hint);
        EditText device = v.findViewById(R.id.c_device);
        Button connect = v.findViewById(R.id.c_connect);
        Button forget = v.findViewById(R.id.c_forget);

        name.setText(c.title());
        device.setText(prefs.deviceName());
        (prefs.mode().equals("mirror") ? mirror : extend).setChecked(true);
        boolean[] canExtend = {true};
        Runnable updateHint = () -> hint.setText(!canExtend[0] ? R.string.mode_extend_unavailable
                : mode.getCheckedRadioButtonId() == R.id.c_mirror ? R.string.mode_mirror_hint : R.string.mode_extend_hint);
        mode.setOnCheckedChangeListener((g, id) -> updateHint.run());
        updateHint.run();
        connect.setEnabled(false);
        forget.setVisibility(c.saved ? View.VISIBLE : View.GONE);

        AlertDialog d = new AlertDialog.Builder(this).setView(v).create();
        forget.setOnClickListener(x -> { prefs.forget(c); d.dismiss(); render(); });
        connect.setOnClickListener(x -> {
            String dev = device.getText().toString().trim();
            if (!dev.isEmpty()) prefs.setDeviceName(dev);
            String m = mode.getCheckedRadioButtonId() == R.id.c_mirror ? "mirror" : "extend";
            prefs.setMode(m);
            prefs.save(c);
            d.dismiss();
            Intent i = new Intent(this, DisplayActivity.class)
                    .putExtra("host", c.host).putExtra("port", c.port)
                    .putExtra("name", c.title()).putExtra("mode", m);
            startActivity(i);
        });
        d.show();

        new Thread(() -> {
            JSONObject info = fetchInfo(c);
            ui.post(() -> {
                if (!d.isShowing()) return;
                if (info == null) {
                    status.setText(R.string.unreachable);
                    status.setTextColor(getColor(R.color.danger));
                    connect.setEnabled(true); // let the user try anyway: the info call may be blocked
                    return;
                }
                String n = info.optString("name", "");
                if (!n.isEmpty()) { c.name = n; name.setText(n); }
                boolean enabled = info.optBoolean("enabled", true);
                canExtend[0] = info.optBoolean("canExtend", true);
                extend.setEnabled(canExtend[0]);
                if (!canExtend[0]) mirror.setChecked(true);
                updateHint.run();
                status.setTextColor(getColor(enabled ? R.color.ok : R.color.warn));
                status.setText(enabled ? (info.optBoolean("pin") ? getString(R.string.status_ready) + " · " + getString(R.string.pin)
                        : getString(R.string.status_ready)) : getString(R.string.paused_text));
                connect.setEnabled(enabled);
            });
        }, "info").start();
    }

    static JSONObject fetchInfo(Computer c) {
        HttpURLConnection h = null;
        try {
            h = (HttpURLConnection) new URL("http://" + c.host + ":" + c.port + "/api/info").openConnection();
            h.setConnectTimeout(3000);
            h.setReadTimeout(3000);
            if (h.getResponseCode() != 200) return null;
            try (InputStream in = h.getInputStream()) {
                ByteArrayOutputStream bo = new ByteArrayOutputStream();
                byte[] buf = new byte[4096];
                for (int n; (n = in.read(buf)) > 0 && bo.size() < 65536; ) bo.write(buf, 0, n);
                JSONObject j = new JSONObject(bo.toString("UTF-8"));
                return "Overspan".equals(j.optString("product")) ? j : null;
            }
        } catch (Exception e) {
            return null;
        } finally {
            if (h != null) h.disconnect();
        }
    }
}
