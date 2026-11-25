package cn.jinchuan.transfer;

import android.Manifest;
import android.app.Activity;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.graphics.Canvas;
import android.graphics.Color;
import android.graphics.ImageFormat;
import android.graphics.Matrix;
import android.graphics.Paint;
import android.graphics.RectF;
import android.graphics.SurfaceTexture;
import android.graphics.Typeface;
import android.hardware.Camera;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.view.Gravity;
import android.view.TextureView;
import android.view.View;
import android.view.WindowInsets;
import android.widget.Button;
import android.widget.FrameLayout;
import android.widget.TextView;

import java.io.IOException;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

@SuppressWarnings("deprecation")
public final class ScanActivity extends Activity implements TextureView.SurfaceTextureListener {
    private final Handler handler = new Handler(Looper.getMainLooper());
    private final ExecutorService readerThread = Executors.newSingleThreadExecutor();
    private final QrDecoder decoder = new QrDecoder();
    private TextureView preview;
    private Camera camera;
    private TextView instructions;
    private boolean active;

    @Override
    protected void onCreate(Bundle state) {
        super.onCreate(state);
        getWindow().setStatusBarColor(Color.BLACK);
        getWindow().setNavigationBarColor(Color.BLACK);
        getWindow().getDecorView().setSystemUiVisibility(0);

        FrameLayout root = new FrameLayout(this);
        root.setBackgroundColor(Color.BLACK);
        preview = new TextureView(this);
        preview.setSurfaceTextureListener(this);
        root.addView(preview);
        root.addView(new ScanFrame(), new FrameLayout.LayoutParams(-1, -1));

        FrameLayout controls = new FrameLayout(this);
        if (Build.VERSION.SDK_INT >= 30) {
            controls.setOnApplyWindowInsetsListener((view, insets) -> {
                android.graphics.Insets bars = insets.getInsets(WindowInsets.Type.systemBars() | WindowInsets.Type.displayCutout());
                controls.setPadding(bars.left, bars.top, bars.right, bars.bottom);
                return insets;
            });
        }

        Button back = new Button(this);
        back.setAllCaps(false);
        back.setText("返回");
        back.setContentDescription("返回近传");
        back.setTextColor(Color.WHITE);
        back.setBackgroundTintList(android.content.res.ColorStateList.valueOf(0xff282b40));
        back.setOnClickListener(v -> finish());
        FrameLayout.LayoutParams backPosition = new FrameLayout.LayoutParams(dp(77), dp(48), Gravity.START | Gravity.TOP);
        backPosition.setMargins(dp(14), dp(12), dp(14), 0);
        controls.addView(back, backPosition);

        TextView heading = new TextView(this);
        heading.setText("扫码连接");
        heading.setTextColor(Color.WHITE);
        heading.setTextSize(18);
        heading.setTypeface(Typeface.DEFAULT, Typeface.BOLD);
        heading.setGravity(Gravity.CENTER);
        FrameLayout.LayoutParams headPosition = new FrameLayout.LayoutParams(-2, dp(48), Gravity.TOP | Gravity.CENTER_HORIZONTAL);
        headPosition.topMargin = dp(12);
        controls.addView(heading, headPosition);

        instructions = new TextView(this);
        instructions.setText("将电脑上的二维码放入框内");
        instructions.setTextColor(Color.WHITE);
        instructions.setTextSize(15);
        instructions.setGravity(Gravity.CENTER);
        instructions.setPadding(dp(13), dp(9), dp(13), dp(9));
        instructions.setBackgroundColor(0xdb202337);
        FrameLayout.LayoutParams instructionPosition = new FrameLayout.LayoutParams(-2, -2, Gravity.BOTTOM | Gravity.CENTER_HORIZONTAL);
        instructionPosition.setMargins(dp(14), 0, dp(14), dp(42));
        controls.addView(instructions, instructionPosition);

        root.addView(controls, new FrameLayout.LayoutParams(-1, -1));
        setContentView(root);
        if (checkSelfPermission(Manifest.permission.CAMERA) != PackageManager.PERMISSION_GRANTED) {
            requestPermissions(new String[]{Manifest.permission.CAMERA}, 1);
        }
    }

    @Override
    protected void onResume() {
        super.onResume();
        active = true;
        startCamera();
    }

    @Override
    protected void onPause() {
        active = false;
        stopCamera();
        super.onPause();
    }

    @Override
    protected void onDestroy() {
        handler.removeCallbacksAndMessages(null);
        readerThread.shutdownNow();
        super.onDestroy();
    }

    @Override
    public void onRequestPermissionsResult(int requestCode, String[] permissions, int[] results) {
        super.onRequestPermissionsResult(requestCode, permissions, results);
        if (requestCode == 1 && results.length > 0 && results[0] == PackageManager.PERMISSION_GRANTED) {
            startCamera();
        } else if (requestCode == 1) {
            fail("相机权限未开启，可以填写地址和配对码。");
        }
    }

    private void startCamera() {
        if (!active || camera != null || !preview.isAvailable() ||
                checkSelfPermission(Manifest.permission.CAMERA) != PackageManager.PERMISSION_GRANTED) return;
        try {
            int count = Camera.getNumberOfCameras();
            if (count == 0) throw new IOException("No camera");
            int selected = 0;
            Camera.CameraInfo info = new Camera.CameraInfo();
            for (int i = 0; i < count; i++) {
                Camera.getCameraInfo(i, info);
                if (info.facing == Camera.CameraInfo.CAMERA_FACING_BACK) {
                    selected = i;
                    break;
                }
            }
            Camera.getCameraInfo(selected, info);
            camera = Camera.open(selected);
            Camera.Parameters parameters = camera.getParameters();
            Camera.Size size = parameters.getPreviewSize();
            Camera.Size chosen = null;
            for (Camera.Size candidate : parameters.getSupportedPreviewSizes()) {
                if (candidate.width <= 1280 && (chosen == null || candidate.width > chosen.width)) chosen = candidate;
            }
            if (chosen != null) size = chosen;
            parameters.setPreviewSize(size.width, size.height);
            parameters.setPreviewFormat(ImageFormat.NV21);
            java.util.List<String> focusModes = parameters.getSupportedFocusModes();
            if (focusModes != null && focusModes.contains(Camera.Parameters.FOCUS_MODE_CONTINUOUS_PICTURE)) {
                parameters.setFocusMode(Camera.Parameters.FOCUS_MODE_CONTINUOUS_PICTURE);
            }
            camera.setParameters(parameters);
            int rotation = getWindowManager().getDefaultDisplay().getRotation() * 90;
            int orientation = info.facing == Camera.CameraInfo.CAMERA_FACING_FRONT
                    ? (720 - info.orientation - rotation) % 360 : (info.orientation - rotation + 360) % 360;
            camera.setDisplayOrientation(orientation);
            adjustPreview(size, orientation);
            camera.setPreviewTexture(preview.getSurfaceTexture());
            camera.startPreview();
            requestFrame(camera, size.width, size.height);
        } catch (IOException | RuntimeException error) {
            stopCamera();
            fail("相机暂时无法打开，可以手动连接。");
        }
    }

    private void adjustPreview(Camera.Size size, int orientation) {
        float w = preview.getWidth();
        float h = preview.getHeight();
        boolean rotated = orientation % 180 != 0;
        float imageRatio = rotated ? (float) size.height / size.width : (float) size.width / size.height;
        Matrix matrix = new Matrix();
        float ratio = imageRatio / (w / h);
        matrix.setScale(Math.max(1, ratio), Math.max(1, 1 / ratio), w / 2, h / 2);
        preview.setTransform(matrix);
    }

    private void requestFrame(Camera source, int width, int height) {
        if (!active || camera != source || isFinishing()) return;
        source.setOneShotPreviewCallback((bytes, capturedBy) -> {
            if (bytes == null || bytes.length < width * height) {
                handler.postDelayed(() -> requestFrame(source, width, height), 150);
                return;
            }
            readerThread.execute(() -> {
                String link = decoder.read(bytes, width, height);
                handler.post(() -> {
                    if (camera != source || isFinishing()) return;
                    if (link != null) {
                        try {
                            TransferApi.scan(link);
                            setResult(RESULT_OK, new Intent().putExtra("pairing", link));
                            finish();
                            return;
                        } catch (IllegalArgumentException ignored) {
                            instructions.setText("请扫描电脑端近传显示的二维码");
                        }
                    }
                    handler.postDelayed(() -> requestFrame(source, width, height), 130);
                });
            });
        });
    }

    private void stopCamera() {
        if (camera == null) return;
        camera.setOneShotPreviewCallback(null);
        camera.release();
        camera = null;
    }

    private void fail(String message) {
        setResult(RESULT_CANCELED, new Intent().putExtra("message", message));
        finish();
    }

    @Override public void onSurfaceTextureAvailable(SurfaceTexture surface, int width, int height) { startCamera(); }
    @Override public void onSurfaceTextureSizeChanged(SurfaceTexture surface, int width, int height) { }
    @Override public boolean onSurfaceTextureDestroyed(SurfaceTexture surface) { stopCamera(); return true; }
    @Override public void onSurfaceTextureUpdated(SurfaceTexture surface) { }

    private int dp(float value) { return Math.round(value * getResources().getDisplayMetrics().density); }

    private final class ScanFrame extends View {
        private final Paint paint = new Paint(Paint.ANTI_ALIAS_FLAG);

        ScanFrame() { super(ScanActivity.this); }

        @Override
        protected void onDraw(Canvas canvas) {
            super.onDraw(canvas);
            float half = Math.min(dp(130), Math.min(getWidth(), getHeight()) * .37f);
            float cx = getWidth() / 2f, cy = getHeight() / 2f;
            RectF box = new RectF(cx - half, cy - half, cx + half, cy + half);
            paint.setStyle(Paint.Style.FILL);
            paint.setColor(0x960c0e18);
            canvas.drawRect(0, 0, getWidth(), box.top, paint);
            canvas.drawRect(0, box.bottom, getWidth(), getHeight(), paint);
            canvas.drawRect(0, box.top, box.left, box.bottom, paint);
            canvas.drawRect(box.right, box.top, getWidth(), box.bottom, paint);
            paint.setStyle(Paint.Style.STROKE);
            paint.setStrokeWidth(dp(3));
            paint.setColor(0xffcbc3ff);
            canvas.drawRoundRect(box, dp(13), dp(13), paint);
        }
    }
}
