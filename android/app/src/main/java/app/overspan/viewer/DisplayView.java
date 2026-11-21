package app.overspan.viewer;

import android.content.Context;
import android.graphics.Bitmap;
import android.graphics.BitmapFactory;
import android.graphics.Canvas;
import android.graphics.Paint;
import android.graphics.RectF;
import android.text.InputType;
import android.util.AttributeSet;
import android.view.HapticFeedbackConstants;
import android.view.KeyEvent;
import android.view.MotionEvent;
import android.view.View;
import android.view.inputmethod.BaseInputConnection;
import android.view.inputmethod.EditorInfo;
import android.view.inputmethod.InputConnection;

import java.nio.ByteBuffer;
import java.nio.ByteOrder;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;

/**
 * Shows the computer's picture and turns touch, stylus, mouse and keys into
 * input messages. Frames arrive as JPEG strips that are painted onto a
 * persistent bitmap, so only changed areas are ever decoded.
 */
public final class DisplayView extends View {

    /** Where input goes. Coordinates are normalised to the remote display (0..1). */
    interface Sink {
        void pointer(String e, float x, float y, int button);
        void rel(float dx, float dy);
        void button(int button, boolean down);
        void wheel(int dx, int dy);
        void key(String code, boolean down);
        void text(String s);
        void pen(String e, float x, float y, float pressure, float tiltX, float tiltY, boolean barrel, boolean eraser);
        void hideToolbar();
    }

    private final Object lock = new Object();
    private final Paint paint = new Paint(Paint.FILTER_BITMAP_FLAG);
    private final RectF dst = new RectF();
    private final ExecutorService decoders = Executors.newFixedThreadPool(Math.max(2, Math.min(4, Runtime.getRuntime().availableProcessors())));
    private Bitmap frame;
    private Canvas frameCanvas;
    private Sink sink;
    private boolean trackpad;
    private boolean imeActive;

    public DisplayView(Context c) { super(c); init(); }
    public DisplayView(Context c, AttributeSet a) { super(c, a); init(); }

    private void init() {
        setFocusable(true);
        setFocusableInTouchMode(true);
        setKeepScreenOn(true);
    }

    void setSink(Sink s) { sink = s; }
    void setTrackpad(boolean t) { trackpad = t; }
    int frameWidth() { synchronized (lock) { return frame == null ? 0 : frame.getWidth(); } }
    int frameHeight() { synchronized (lock) { return frame == null ? 0 : frame.getHeight(); } }

    void release() { decoders.shutdownNow(); }

    void clear() {
        synchronized (lock) {
            frame = null;
            frameCanvas = null;
        }
        postInvalidate();
    }

    /**
     * Applies one frame message (see the server's protocol notes). Called on
     * the network thread. Returns the frame's sequence number, or -1.
     */
    long applyFrame(byte[] msg) {
        ByteBuffer b = ByteBuffer.wrap(msg).order(ByteOrder.LITTLE_ENDIAN);
        if (msg.length < 12 || b.get(0) != 'F') return -1;
        long seq = b.getInt(2) & 0xffffffffL;
        int w = b.getShort(6) & 0xffff, h = b.getShort(8) & 0xffff, n = b.getShort(10) & 0xffff;
        int off = 12;
        List<int[]> rects = new ArrayList<>(n);
        List<Future<Bitmap>> jobs = new ArrayList<>(n);
        for (int i = 0; i < n && off + 12 <= msg.length; i++) {
            final int x = b.getShort(off) & 0xffff, y = b.getShort(off + 2) & 0xffff;
            final int len = b.getInt(off + 8);
            final int start = off + 12;
            if (len < 0 || start + len > msg.length) break;
            rects.add(new int[]{x, y});
            try {
                jobs.add(decoders.submit(() -> BitmapFactory.decodeByteArray(msg, start, len)));
            } catch (java.util.concurrent.RejectedExecutionException closing) {
                return -1; // the screen is closing
            }
            off = start + len;
        }
        synchronized (lock) {
            if (frame == null || frame.getWidth() != w || frame.getHeight() != h) {
                frame = Bitmap.createBitmap(w, h, Bitmap.Config.ARGB_8888);
                frameCanvas = new Canvas(frame);
            }
        }
        for (int i = 0; i < jobs.size(); i++) {
            try {
                Bitmap patch = jobs.get(i).get();
                if (patch == null) continue;
                synchronized (lock) {
                    if (frameCanvas != null) frameCanvas.drawBitmap(patch, rects.get(i)[0], rects.get(i)[1], null);
                }
                patch.recycle();
            } catch (Exception e) {
                return -1;
            }
        }
        postInvalidateOnAnimation();
        return seq;
    }

    @Override
    protected void onDraw(Canvas c) {
        synchronized (lock) {
            if (frame == null) return;
            fit(frame.getWidth(), frame.getHeight());
            c.drawBitmap(frame, null, dst, paint);
        }
    }

    private void fit(int fw, int fh) {
        float s = Math.min(getWidth() / (float) fw, getHeight() / (float) fh);
        float dw = fw * s, dh = fh * s;
        float ox = (getWidth() - dw) / 2f, oy = (getHeight() - dh) / 2f;
        dst.set(ox, oy, ox + dw, oy + dh);
    }

    private float nx(float x) { return dst.width() <= 0 ? 0 : (x - dst.left) / dst.width(); }
    private float ny(float y) { return dst.height() <= 0 ? 0 : (y - dst.top) / dst.height(); }
    /** Screen pixels to remote pixels (for trackpad deltas). */
    private float scaleToRemote() { synchronized (lock) { return frame == null || dst.width() <= 0 ? 1 : frame.getWidth() / dst.width(); } }

    // ---------------------------------------------------------------- touch

    private static final float SLOP_DP = 10f;
    private static final long LONG_PRESS_MS = 520;
    private enum G { NONE, PENDING, DRAG, SCROLL, PAD, PAD_DRAG, DONE }
    private G gesture = G.NONE;
    private float startX, startY, lastX, lastY, accX, accY;
    private long startAt;
    private boolean moved;
    private int mouseButtons;
    private final Runnable longPress = this::onLongPress;

    private float slop() { return SLOP_DP * getResources().getDisplayMetrics().density; }

    @Override
    public boolean onTouchEvent(MotionEvent e) {
        if (sink == null) return false;
        int tool = e.getToolType(e.getActionIndex());
        if (tool == MotionEvent.TOOL_TYPE_STYLUS || tool == MotionEvent.TOOL_TYPE_ERASER) return onPen(e, false);
        if (tool == MotionEvent.TOOL_TYPE_MOUSE) return onMouse(e);
        return onFinger(e);
    }

    private boolean onFinger(MotionEvent e) {
        int action = e.getActionMasked();
        switch (action) {
            case MotionEvent.ACTION_DOWN: {
                requestFocus();
                sink.hideToolbar();
                startX = lastX = e.getX();
                startY = lastY = e.getY();
                startAt = e.getEventTime();
                moved = false;
                gesture = trackpad ? G.PAD : G.PENDING;
                if (!trackpad) sink.pointer("move", nx(startX), ny(startY), 0);
                removeCallbacks(longPress);
                postDelayed(longPress, LONG_PRESS_MS);
                return true;
            }
            case MotionEvent.ACTION_POINTER_DOWN: {
                removeCallbacks(longPress);
                if (e.getPointerCount() == 2) {
                    if (gesture == G.DRAG) sink.pointer("up", nx(lastX), ny(lastY), 0);
                    if (gesture == G.PAD_DRAG) sink.button(0, false);
                    gesture = G.SCROLL;
                    moved = false;
                    accX = accY = 0;
                    lastX = midX(e);
                    lastY = midY(e);
                    startAt = e.getEventTime();
                }
                return true;
            }
            case MotionEvent.ACTION_MOVE: {
                float x = gesture == G.SCROLL ? midX(e) : e.getX();
                float y = gesture == G.SCROLL ? midY(e) : e.getY();
                float dx = x - lastX, dy = y - lastY;
                lastX = x;
                lastY = y;
                boolean far = Math.hypot(e.getX() - startX, e.getY() - startY) > slop();
                if (far) moved = true;
                switch (gesture) {
                    case PENDING:
                        if (far) {
                            removeCallbacks(longPress);
                            sink.pointer("down", nx(startX), ny(startY), 0);
                            gesture = G.DRAG;
                            sink.pointer("move", nx(x), ny(y), 0);
                        }
                        break;
                    case DRAG:
                        sink.pointer("move", nx(x), ny(y), 0);
                        break;
                    case PAD:
                    case PAD_DRAG: {
                        if (far) removeCallbacks(longPress);
                        float accel = 1f + Math.min(2f, (float) Math.hypot(dx, dy) / 12f);
                        float s = scaleToRemote();
                        sink.rel(dx * s * accel, dy * s * accel);
                        break;
                    }
                    case SCROLL: {
                        if (Math.abs(dx) + Math.abs(dy) > 0) moved = moved || Math.hypot(x - startX, y - startY) > slop();
                        // natural scrolling; 40 dp of finger travel ≈ one wheel notch (120 units)
                        float k = 3f / getResources().getDisplayMetrics().density;
                        accX += -dx * k;
                        accY += -dy * k;
                        int sx = (int) accX, sy = (int) accY;
                        if (sx != 0 || sy != 0) {
                            sink.wheel(sx, sy);
                            accX -= sx;
                            accY -= sy;
                        }
                        break;
                    }
                    default:
                        break;
                }
                return true;
            }
            case MotionEvent.ACTION_POINTER_UP:
                if (gesture == G.SCROLL && !moved && e.getEventTime() - startAt < 300) {
                    sink.button(2, true);
                    sink.button(2, false);
                }
                if (gesture == G.SCROLL) gesture = G.DONE;
                return true;
            case MotionEvent.ACTION_UP: {
                removeCallbacks(longPress);
                boolean quick = e.getEventTime() - startAt < 300;
                switch (gesture) {
                    case PENDING:
                        sink.pointer("move", nx(startX), ny(startY), 0);
                        sink.button(0, true);
                        sink.button(0, false);
                        break;
                    case DRAG:
                        sink.pointer("up", nx(e.getX()), ny(e.getY()), 0);
                        break;
                    case PAD:
                        if (!moved && quick) { sink.button(0, true); sink.button(0, false); }
                        break;
                    case PAD_DRAG:
                        sink.button(0, false);
                        break;
                    default:
                        break;
                }
                gesture = G.NONE;
                return true;
            }
            case MotionEvent.ACTION_CANCEL:
                removeCallbacks(longPress);
                if (gesture == G.DRAG) sink.pointer("up", nx(lastX), ny(lastY), 0);
                if (gesture == G.PAD_DRAG) sink.button(0, false);
                gesture = G.NONE;
                return true;
        }
        return true;
    }

    private void onLongPress() {
        if (moved) return;
        if (gesture == G.PENDING) {
            sink.button(2, true);
            sink.button(2, false);
            gesture = G.DONE;
            performHapticFeedback(HapticFeedbackConstants.LONG_PRESS);
        } else if (gesture == G.PAD) {
            sink.button(0, true);
            gesture = G.PAD_DRAG;
            performHapticFeedback(HapticFeedbackConstants.LONG_PRESS);
        }
    }

    private static float midX(MotionEvent e) { return (e.getX(0) + e.getX(Math.min(1, e.getPointerCount() - 1))) / 2f; }
    private static float midY(MotionEvent e) { return (e.getY(0) + e.getY(Math.min(1, e.getPointerCount() - 1))) / 2f; }

    // --------------------------------------------------------------- stylus

    private boolean onPen(MotionEvent e, boolean hover) {
        boolean eraser = e.getToolType(0) == MotionEvent.TOOL_TYPE_ERASER;
        boolean barrel = (e.getButtonState() & (MotionEvent.BUTTON_STYLUS_PRIMARY | MotionEvent.BUTTON_SECONDARY)) != 0;
        String kind;
        switch (e.getActionMasked()) {
            case MotionEvent.ACTION_DOWN: kind = "down"; requestFocus(); sink.hideToolbar(); break;
            case MotionEvent.ACTION_MOVE: kind = "move"; break;
            case MotionEvent.ACTION_UP: case MotionEvent.ACTION_CANCEL: kind = "hover"; break;
            case MotionEvent.ACTION_HOVER_ENTER: case MotionEvent.ACTION_HOVER_MOVE: kind = "hover"; break;
            case MotionEvent.ACTION_HOVER_EXIT: kind = "leave"; break;
            default: return true;
        }
        if (kind.equals("move")) {
            for (int h = 0; h < e.getHistorySize(); h++) {
                sendPen("move", e.getHistoricalX(h), e.getHistoricalY(h), e.getHistoricalPressure(h),
                        e.getHistoricalAxisValue(MotionEvent.AXIS_TILT, h), e.getHistoricalAxisValue(MotionEvent.AXIS_ORIENTATION, h), barrel, eraser);
            }
        }
        sendPen(kind, e.getX(), e.getY(), e.getPressure(), e.getAxisValue(MotionEvent.AXIS_TILT), e.getAxisValue(MotionEvent.AXIS_ORIENTATION), barrel, eraser);
        return true;
    }

    private void sendPen(String kind, float x, float y, float pressure, float tilt, float orientation, boolean barrel, boolean eraser) {
        double deg = Math.toDegrees(tilt);
        float tx = (float) (deg * Math.sin(orientation));
        float ty = (float) (-deg * Math.cos(orientation));
        sink.pen(kind, nx(x), ny(y), Math.max(0f, Math.min(1f, pressure)), tx, ty, barrel, eraser);
    }

    // ---------------------------------------------------------------- mouse

    private boolean onMouse(MotionEvent e) {
        sink.pointer("move", nx(e.getX()), ny(e.getY()), 0);
        int state = e.getButtonState();
        int changed = state ^ mouseButtons;
        int[][] map = {{MotionEvent.BUTTON_PRIMARY, 0}, {MotionEvent.BUTTON_TERTIARY, 1}, {MotionEvent.BUTTON_SECONDARY, 2},
                {MotionEvent.BUTTON_BACK, 3}, {MotionEvent.BUTTON_FORWARD, 4}};
        for (int[] m : map) if ((changed & m[0]) != 0) sink.button(m[1], (state & m[0]) != 0);
        mouseButtons = state;
        if (e.getActionMasked() == MotionEvent.ACTION_DOWN) { requestFocus(); sink.hideToolbar(); }
        return true;
    }

    @Override
    public boolean onGenericMotionEvent(MotionEvent e) {
        if (sink == null) return false;
        int tool = e.getToolType(0);
        if (tool == MotionEvent.TOOL_TYPE_STYLUS || tool == MotionEvent.TOOL_TYPE_ERASER) return onPen(e, true);
        switch (e.getActionMasked()) {
            case MotionEvent.ACTION_HOVER_MOVE:
                sink.pointer("move", nx(e.getX()), ny(e.getY()), 0);
                return true;
            case MotionEvent.ACTION_SCROLL:
                sink.wheel(Math.round(e.getAxisValue(MotionEvent.AXIS_HSCROLL) * 120f), Math.round(-e.getAxisValue(MotionEvent.AXIS_VSCROLL) * 120f));
                return true;
            case MotionEvent.ACTION_BUTTON_PRESS:
            case MotionEvent.ACTION_BUTTON_RELEASE:
                return onMouse(e);
        }
        return super.onGenericMotionEvent(e);
    }

    // ------------------------------------------------------------- keyboard

    @Override
    public boolean onKeyDown(int keyCode, KeyEvent e) { return key(keyCode, e, true) || super.onKeyDown(keyCode, e); }

    @Override
    public boolean onKeyUp(int keyCode, KeyEvent e) { return key(keyCode, e, false) || super.onKeyUp(keyCode, e); }

    private boolean key(int keyCode, KeyEvent e, boolean down) {
        if (sink == null || keyCode == KeyEvent.KEYCODE_BACK) return false;
        // volume keys stay with the phone unless a hardware keyboard sent them
        if ((keyCode == KeyEvent.KEYCODE_VOLUME_UP || keyCode == KeyEvent.KEYCODE_VOLUME_DOWN) && !e.isFromSource(android.view.InputDevice.SOURCE_KEYBOARD)) return false;
        String code = KeyMap.code(keyCode);
        if (code == null) {
            if (down && e.getUnicodeChar() != 0) sink.text(String.valueOf((char) e.getUnicodeChar()));
            return e.getUnicodeChar() != 0;
        }
        if (down && e.getRepeatCount() > 0) { sink.key(code, false); }
        sink.key(code, down);
        return true;
    }

    void setImeActive(boolean on) { imeActive = on; }

    @Override
    public boolean onCheckIsTextEditor() { return imeActive; }

    @Override
    public InputConnection onCreateInputConnection(EditorInfo out) {
        out.inputType = InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_VISIBLE_PASSWORD | InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS;
        out.imeOptions = EditorInfo.IME_FLAG_NO_EXTRACT_UI | EditorInfo.IME_FLAG_NO_FULLSCREEN | EditorInfo.IME_ACTION_NONE;
        return new Bridge(this);
    }

    /** Soft keyboards talk in text, not keys: translate edits into key presses. */
    private final class Bridge extends BaseInputConnection {
        private String composing = "";

        Bridge(View v) { super(v, false); }

        private void tap(String code) { sink.key(code, true); sink.key(code, false); }

        private void replace(String now) {
            int common = 0;
            while (common < composing.length() && common < now.length() && composing.charAt(common) == now.charAt(common)) common++;
            for (int i = common; i < composing.length(); i++) tap("Backspace");
            if (common < now.length()) sink.text(now.substring(common));
        }

        @Override public boolean commitText(CharSequence text, int pos) { if (sink != null) replace(text.toString()); composing = ""; return true; }
        @Override public boolean setComposingText(CharSequence text, int pos) { if (sink != null) replace(text.toString()); composing = text.toString(); return true; }
        @Override public boolean finishComposingText() { composing = ""; return true; }
        @Override public boolean deleteSurroundingText(int before, int after) {
            if (sink != null) { for (int i = 0; i < before; i++) tap("Backspace"); for (int i = 0; i < after; i++) tap("Delete"); }
            return true;
        }
        @Override public boolean sendKeyEvent(KeyEvent e) {
            if (e.getAction() == KeyEvent.ACTION_DOWN) onKeyDown(e.getKeyCode(), e);
            else if (e.getAction() == KeyEvent.ACTION_UP) onKeyUp(e.getKeyCode(), e);
            return true;
        }
    }
}
