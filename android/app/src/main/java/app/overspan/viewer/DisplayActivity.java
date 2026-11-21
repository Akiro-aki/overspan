package app.overspan.viewer;

import android.app.Activity;
import android.app.AlertDialog;
import android.content.res.Configuration;
import android.graphics.Rect;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.util.DisplayMetrics;
import android.view.KeyEvent;
import android.view.LayoutInflater;
import android.view.View;
import android.view.WindowInsets;
import android.view.WindowInsetsController;
import android.view.WindowManager;
import android.view.inputmethod.EditorInfo;
import android.view.inputmethod.InputMethodManager;
import android.widget.EditText;
import android.widget.ImageButton;
import android.widget.RadioGroup;
import android.widget.SeekBar;
import android.widget.Switch;
import android.widget.TextView;
import android.widget.Toast;

import org.json.JSONObject;

import java.util.Locale;
import java.util.concurrent.atomic.AtomicLong;

/** C02–C04: the remote display, its status screens, toolbar and settings. */
public final class DisplayActivity extends Activity implements WsClient.Listener, DisplayView.Sink {
    private static final float[] SCALES = {1f, 1.25f, 1.5f, 2f, 2.5f, 3f};

    private final Handler ui = new Handler(Looper.getMainLooper());
    private final AtomicLong frames = new AtomicLong(), bytes = new AtomicLong();
    private Prefs prefs;
    private DisplayView display;
    private View status, toolbar, pinBox, spinner, handle;
    private TextView statusTitle, statusText, pinError, banner, stats;
    private EditText pin;
    private ImageButton touchBtn;

    private String host, name, mode;
    private int port;
    private WsClient ws;
    private volatile boolean live;
    private boolean userClosed, landscape, imeShown;
    private String pinValue = "";
    private JSONObject lastError;
    private int retries;
    private boolean resumeOnStart;

    // ------------------------------------------------------------ lifecycle

    @Override
    protected void onCreate(Bundle b) {
        super.onCreate(b);
        setContentView(R.layout.activity_display);
        getWindow().addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
        prefs = new Prefs(this);
        host = getIntent().getStringExtra("host");
        port = getIntent().getIntExtra("port", 7940);
        name = getIntent().getStringExtra("name");
        mode = getIntent().getStringExtra("mode");

        display = findViewById(R.id.display);
        status = findViewById(R.id.status);
        toolbar = findViewById(R.id.toolbar);
        handle = findViewById(R.id.handle);
        pinBox = findViewById(R.id.pin_box);
        spinner = findViewById(R.id.status_spin);
        statusTitle = findViewById(R.id.status_title);
        statusText = findViewById(R.id.status_text);
        pinError = findViewById(R.id.pin_error);
        pin = findViewById(R.id.pin);
        banner = findViewById(R.id.banner);
        stats = findViewById(R.id.stats);
        touchBtn = findViewById(R.id.tb_touch);

        display.setSink(this);
        applyTouchMode();
        handle.setOnClickListener(v -> toggleToolbar());
        findViewById(R.id.tb_keyboard).setOnClickListener(v -> toggleKeyboard());
        touchBtn.setOnClickListener(v -> {
            prefs.setTrackpad(!prefs.trackpad());
            applyTouchMode();
            toast(prefs.trackpad() ? R.string.touch_trackpad : R.string.touch_direct);
        });
        findViewById(R.id.tb_settings).setOnClickListener(v -> openSettings());
        findViewById(R.id.tb_disconnect).setOnClickListener(v -> confirmDisconnect());
        findViewById(R.id.btn_retry).setOnClickListener(v -> { retries = 0; connect(); });
        findViewById(R.id.btn_back).setOnClickListener(v -> { userClosed = true; finish(); });
        findViewById(R.id.pin_go).setOnClickListener(v -> submitPin());
        pin.setOnEditorActionListener((v, id, e) -> { if (id == EditorInfo.IME_ACTION_DONE) { submitPin(); return true; } return false; });

        landscape = isLandscape();
        immersive();
        connect();
        ui.postDelayed(statsTick, 1000);
    }

    @Override
    protected void onStart() {
        super.onStart();
        if (resumeOnStart) { resumeOnStart = false; retries = 0; connect(); }
    }

    @Override
    protected void onStop() {
        super.onStop();
        // Stop streaming while the app is in the background. The server keeps the
        // monitor for a while, so coming back puts the windows where they were.
        if (live && !isFinishing()) {
            resumeOnStart = true;
            userClosed = true;
            if (ws != null) { ws.send("{\"t\":\"bye\"}"); ws.close(); }
        }
    }

    @Override
    protected void onDestroy() {
        super.onDestroy();
        userClosed = true;
        ui.removeCallbacksAndMessages(null);
        if (ws != null) ws.close();
        display.release();
    }

    @Override
    public void onWindowFocusChanged(boolean hasFocus) {
        super.onWindowFocusChanged(hasFocus);
        if (hasFocus) immersive();
    }

    @SuppressWarnings("deprecation")
    private void immersive() {
        if (Build.VERSION.SDK_INT >= 30) {
            getWindow().setDecorFitsSystemWindows(false);
            WindowInsetsController c = getWindow().getInsetsController();
            if (c != null) {
                c.hide(WindowInsets.Type.systemBars());
                c.setSystemBarsBehavior(WindowInsetsController.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE);
            }
        } else {
            getWindow().getDecorView().setSystemUiVisibility(View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY
                    | View.SYSTEM_UI_FLAG_FULLSCREEN | View.SYSTEM_UI_FLAG_HIDE_NAVIGATION
                    | View.SYSTEM_UI_FLAG_LAYOUT_STABLE | View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN
                    | View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION);
        }
    }

    // ------------------------------------------------------------- sizing

    private boolean isLandscape() { int[] s = screenPixels(); return s[0] > s[1]; }

    @SuppressWarnings("deprecation")
    private int[] screenPixels() {
        if (Build.VERSION.SDK_INT >= 30) {
            Rect r = getWindowManager().getCurrentWindowMetrics().getBounds();
            return new int[]{r.width(), r.height()};
        }
        DisplayMetrics dm = new DisplayMetrics();
        getWindowManager().getDefaultDisplay().getRealMetrics(dm);
        return new int[]{dm.widthPixels, dm.heightPixels};
    }

    /** Width, height (physical pixels) and desktop scale to ask the computer for. */
    private float[] targetSize() {
        int[] px = screenPixels();
        float density = getResources().getDisplayMetrics().density;
        float cssW = px[0] / density;
        float w = px[0], h = px[1];
        int cap = prefs.res().equals("native") ? Integer.MAX_VALUE : prefs.res().equals("light") ? 1280 : 1920;
        float longEdge = Math.max(w, h);
        if (longEdge > cap) { float f = cap / longEdge; w *= f; h *= f; }
        float ratio = w / cssW, scale = 1f;
        for (float s : SCALES) if (s <= ratio + 0.05f) scale = s;
        return new float[]{Math.round(w), Math.round(h), scale};
    }

    @Override
    public void onConfigurationChanged(Configuration c) {
        super.onConfigurationChanged(c);
        immersive();
        ui.postDelayed(() -> {
            boolean now = isLandscape();
            if (now == landscape) return;
            landscape = now;
            if (live && "extend".equals(mode) && prefs.rotate()) {
                float[] s = targetSize();
                send(String.format(Locale.ROOT, "{\"t\":\"resize\",\"w\":%d,\"h\":%d}", (int) s[0], (int) s[1]));
            }
        }, 600);
    }

    // --------------------------------------------------------- connection

    private void connect() {
        userClosed = false;
        lastError = null;
        if (!live) showStatus(getString(R.string.st_connecting), getString(R.string.st_connecting_text, name), true, false, false, null);
        if (ws != null) ws.close();
        ws = new WsClient();
        String ua = "Overspan-Android/0.1.0 (Android " + Build.VERSION.RELEASE + "; " + Build.MODEL + ")";
        ws.connect(host, port, ua, this);
    }

    @Override
    public void onOpen() {
        float[] s = targetSize();
        try {
            JSONObject h = new JSONObject()
                    .put("t", "hello").put("device", prefs.deviceId()).put("name", prefs.deviceName())
                    .put("mode", mode).put("width", (int) s[0]).put("height", (int) s[1]).put("scale", s[2])
                    .put("pin", pinValue).put("quality", prefs.quality()).put("fps", prefs.fps());
            ws.send(h.toString());
        } catch (Exception ignored) { }
    }

    @Override
    public void onText(String text) {
        final JSONObject m;
        try { m = new JSONObject(text); } catch (Exception e) { return; }
        ui.post(() -> handle(m));
    }

    private void handle(JSONObject m) {
        switch (m.optString("t")) {
            case "wait":
                showStatus(getString(R.string.st_waiting), m.optString("text"), true, false, false, null);
                break;
            case "setup":
                if (!live) {
                    boolean ext = "extend".equals(m.optString("mode"));
                    showStatus(getString(ext ? R.string.st_setup_extend : R.string.st_setup_mirror),
                            ext ? getString(R.string.st_setup_extend_text) : "", true, false, false, null);
                }
                break;
            case "error":
                lastError = m;
                break;
            case "ready":
                live = true;
                retries = 0;
                mode = m.optString("mode", mode);
                status.setVisibility(View.GONE);
                banner.setVisibility(View.GONE);
                display.requestFocus();
                ws.send("{\"t\":\"refresh\"}");
                if (!m.optBoolean("input", true)) toast(R.string.input_off);
                if (!prefs.seenHint()) { toast(R.string.hint_toolbar); prefs.setSeenHint(); }
                break;
            case "input":
                toast(m.optBoolean("on") ? R.string.input_on : R.string.input_off);
                break;
            case "notice":
                if ("asleep".equals(m.optString("code"))) {
                    banner.setText(R.string.asleep);
                    banner.setVisibility(View.VISIBLE);
                } else {
                    banner.setVisibility(View.GONE);
                }
                break;
        }
    }

    @Override
    public void onBinary(byte[] data) {
        long seq = display.applyFrame(data);
        if (seq >= 0 && ws != null) ws.send("{\"t\":\"ack\",\"sq\":" + seq + "}");
        frames.incrementAndGet();
        bytes.addAndGet(data.length);
    }

    @Override
    public void onClosed(String reason) {
        ui.post(this::closedUi);
    }

    private void closedUi() {
        boolean wasLive = live;
        live = false;
        if (userClosed) return;
        JSONObject e = lastError;
        lastError = null;
        if (e != null && wasLive && "shutdown".equals(e.optString("code"))) e = null; // the computer restarts: keep trying
        if (e != null) {
            String code = e.optString("code");
            if (code.equals("pin") || code.equals("pin-wrong")) {
                showStatus(getString(R.string.st_pin), getString(R.string.st_pin_text), false, false, true,
                        code.equals("pin-wrong") ? getString(R.string.st_pin_wrong) : null);
                return;
            }
            int title;
            switch (code) {
                case "denied": title = R.string.st_denied; break;
                case "disabled": title = R.string.st_disabled; break;
                case "timeout": title = R.string.st_timeout; break;
                case "display": title = R.string.st_display; break;
                case "capture": title = R.string.st_capture; break;
                case "protocol": title = R.string.st_protocol; break;
                default: title = R.string.st_closed;
            }
            display.clear();
            showStatus(getString(title), e.optString("text"), false, true, false, null);
            return;
        }
        if (wasLive && retries < 6) {
            retries++;
            banner.setText(R.string.st_reconnecting);
            banner.setVisibility(View.VISIBLE);
            ui.postDelayed(this::connect, Math.min(4000, 600L * retries));
            return;
        }
        display.clear();
        showStatus(getString(R.string.st_lost), getString(R.string.st_lost_text), false, true, false, null);
    }

    private void showStatus(String title, String text, boolean spin, boolean retry, boolean askPin, String pinErr) {
        status.setVisibility(View.VISIBLE);
        toolbar.setVisibility(View.GONE);
        statusTitle.setText(title);
        statusText.setText(text);
        statusText.setVisibility(text == null || text.isEmpty() ? View.GONE : View.VISIBLE);
        spinner.setVisibility(spin ? View.VISIBLE : View.GONE);
        findViewById(R.id.btn_retry).setVisibility(retry ? View.VISIBLE : View.GONE);
        pinBox.setVisibility(askPin ? View.VISIBLE : View.GONE);
        pinError.setText(pinErr == null ? "" : pinErr);
        if (askPin) {
            pin.setText("");
            pin.requestFocus();
            ui.postDelayed(() -> ((InputMethodManager) getSystemService(INPUT_METHOD_SERVICE)).showSoftInput(pin, 0), 200);
        } else if (retry) {
            findViewById(R.id.btn_retry).requestFocus();
        }
    }

    private void submitPin() {
        pinValue = pin.getText().toString().trim();
        ((InputMethodManager) getSystemService(INPUT_METHOD_SERVICE)).hideSoftInputFromWindow(pin.getWindowToken(), 0);
        connect();
    }

    private void disconnect() {
        userClosed = true;
        live = false;
        if (ws != null) { ws.send("{\"t\":\"bye\"}"); ws.close(); }
        finish();
    }

    private void confirmDisconnect() {
        new AlertDialog.Builder(this)
                .setMessage(getString(R.string.disconnect_confirm, name))
                .setPositiveButton(R.string.tb_disconnect, (d, w) -> disconnect())
                .setNegativeButton(R.string.cancel, null)
                .show();
    }

    @Override
    @SuppressWarnings("deprecation")
    public void onBackPressed() {
        if (status.getVisibility() == View.VISIBLE) { userClosed = true; super.onBackPressed(); return; }
        if (imeShown) { toggleKeyboard(); return; }
        if (toolbar.getVisibility() == View.VISIBLE) confirmDisconnect();
        else showToolbar();
    }

    // ------------------------------------------------------------ toolbar

    private final Runnable autoHide = this::hideToolbar;

    private void showToolbar() {
        toolbar.setVisibility(View.VISIBLE);
        handle.setVisibility(View.GONE);
        ui.removeCallbacks(autoHide);
        ui.postDelayed(autoHide, 6000);
    }

    @Override
    public void hideToolbar() {
        ui.post(() -> {
            toolbar.setVisibility(View.GONE);
            handle.setVisibility(View.VISIBLE);
        });
    }

    private void toggleToolbar() { if (toolbar.getVisibility() == View.VISIBLE) hideToolbar(); else showToolbar(); }

    private void applyTouchMode() {
        display.setTrackpad(prefs.trackpad());
        touchBtn.setActivated(prefs.trackpad());
        touchBtn.setContentDescription(getString(prefs.trackpad() ? R.string.touch_trackpad : R.string.touch_direct));
    }

    private void toggleKeyboard() {
        InputMethodManager imm = (InputMethodManager) getSystemService(INPUT_METHOD_SERVICE);
        imeShown = !imeShown;
        display.setImeActive(imeShown);
        imm.restartInput(display);
        display.requestFocus();
        if (imeShown) imm.showSoftInput(display, InputMethodManager.SHOW_IMPLICIT);
        else imm.hideSoftInputFromWindow(display.getWindowToken(), 0);
        hideToolbar();
    }

    // ----------------------------------------------------------- settings

    private void openSettings() {
        ui.removeCallbacks(autoHide);
        View v = LayoutInflater.from(this).inflate(R.layout.dialog_settings, null);
        RadioGroup res = v.findViewById(R.id.s_res);
        res.check(prefs.res().equals("native") ? R.id.s_res_native : prefs.res().equals("light") ? R.id.s_res_light : R.id.s_res_balanced);
        SeekBar q = v.findViewById(R.id.s_quality);
        TextView ql = v.findViewById(R.id.s_quality_label);
        q.setProgress((prefs.quality() - 30) / 5);
        ql.setText(getString(R.string.set_quality, prefs.quality()));
        SeekBar f = v.findViewById(R.id.s_fps);
        TextView fl = v.findViewById(R.id.s_fps_label);
        f.setProgress((prefs.fps() - 10) / 5);
        fl.setText(getString(R.string.set_fps, prefs.fps()));
        RadioGroup touch = v.findViewById(R.id.s_touch);
        touch.check(prefs.trackpad() ? R.id.s_touch_pad : R.id.s_touch_direct);
        Switch st = v.findViewById(R.id.s_stats);
        st.setChecked(prefs.stats());
        Switch rot = v.findViewById(R.id.s_rotate);
        rot.setChecked(prefs.rotate());

        q.setOnSeekBarChangeListener(new Seek(p -> ql.setText(getString(R.string.set_quality, 30 + p * 5))));
        f.setOnSeekBarChangeListener(new Seek(p -> fl.setText(getString(R.string.set_fps, 10 + p * 5))));

        new AlertDialog.Builder(this)
                .setTitle(R.string.settings_title)
                .setView(v)
                .setPositiveButton(R.string.done, (d, w) -> {
                    String oldRes = prefs.res();
                    int id = res.getCheckedRadioButtonId();
                    prefs.setRes(id == R.id.s_res_native ? "native" : id == R.id.s_res_light ? "light" : "balanced");
                    prefs.setQuality(30 + q.getProgress() * 5);
                    prefs.setFps(10 + f.getProgress() * 5);
                    prefs.setTrackpad(touch.getCheckedRadioButtonId() == R.id.s_touch_pad);
                    prefs.setStats(st.isChecked());
                    prefs.setRotate(rot.isChecked());
                    applyTouchMode();
                    send(String.format(Locale.ROOT, "{\"t\":\"cfg\",\"q\":%d,\"f\":%d}", prefs.quality(), prefs.fps()));
                    if (!oldRes.equals(prefs.res()) && "extend".equals(mode)) {
                        float[] s = targetSize();
                        send(String.format(Locale.ROOT, "{\"t\":\"resize\",\"w\":%d,\"h\":%d}", (int) s[0], (int) s[1]));
                    }
                })
                .setOnDismissListener(d -> { immersive(); ui.postDelayed(autoHide, 6000); })
                .show();
    }

    private interface IntFn { void run(int v); }

    private static final class Seek implements SeekBar.OnSeekBarChangeListener {
        private final IntFn fn;
        Seek(IntFn fn) { this.fn = fn; }
        @Override public void onProgressChanged(SeekBar s, int p, boolean user) { fn.run(p); }
        @Override public void onStartTrackingTouch(SeekBar s) { }
        @Override public void onStopTrackingTouch(SeekBar s) { }
    }

    private final Runnable statsTick = new Runnable() {
        @Override public void run() {
            long fr = frames.getAndSet(0), by = bytes.getAndSet(0);
            if (prefs.stats() && live) {
                stats.setVisibility(View.VISIBLE);
                stats.setText(String.format(Locale.ROOT, "%d×%d  %d fps  %d KB/s", display.frameWidth(), display.frameHeight(), fr, by / 1024));
            } else {
                stats.setVisibility(View.GONE);
            }
            ui.postDelayed(this, 1000);
        }
    };

    private void toast(int res) { Toast.makeText(this, res, Toast.LENGTH_SHORT).show(); }

    // ---------------------------------------------------------- input sink

    private void send(String json) { if (live && ws != null) ws.send(json); }

    private static String f(float v) { return String.format(Locale.ROOT, "%.5f", v); }

    @Override public void pointer(String e, float x, float y, int b) {
        send("{\"t\":\"p\",\"e\":\"" + e + "\",\"x\":" + f(x) + ",\"y\":" + f(y) + ",\"b\":" + b + "}");
    }
    @Override public void rel(float dx, float dy) { send("{\"t\":\"rel\",\"dx\":" + f(dx) + ",\"dy\":" + f(dy) + "}"); }
    @Override public void button(int b, boolean down) { send("{\"t\":\"btn\",\"b\":" + b + ",\"d\":" + down + "}"); }
    @Override public void wheel(int dx, int dy) { send("{\"t\":\"w\",\"dx\":" + dx + ",\"dy\":" + dy + "}"); }
    @Override public void key(String code, boolean down) { send("{\"t\":\"k\",\"c\":\"" + code + "\",\"d\":" + down + "}"); }
    @Override public void text(String s) {
        try { send(new JSONObject().put("t", "txt").put("s", s).toString()); } catch (Exception ignored) { }
    }
    @Override public void pen(String e, float x, float y, float p, float tx, float ty, boolean barrel, boolean eraser) {
        send("{\"t\":\"pen\",\"e\":\"" + e + "\",\"x\":" + f(x) + ",\"y\":" + f(y) + ",\"p\":" + f(p)
                + ",\"tx\":" + f(tx) + ",\"ty\":" + f(ty) + ",\"b\":" + (barrel ? 1 : 0) + ",\"r\":" + eraser + "}");
    }

    @Override
    public boolean dispatchKeyEvent(KeyEvent event) {
        // keep hardware keys flowing to the display even when focus wandered
        if (live && status.getVisibility() != View.VISIBLE && event.getKeyCode() != KeyEvent.KEYCODE_BACK && !display.hasFocus()) {
            display.requestFocus();
        }
        return super.dispatchKeyEvent(event);
    }
}
