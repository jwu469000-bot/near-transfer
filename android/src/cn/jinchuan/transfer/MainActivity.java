package cn.jinchuan.transfer;

import android.app.Activity;
import android.app.AlertDialog;
import android.content.ClipData;
import android.content.Intent;
import android.content.SharedPreferences;
import android.content.res.ColorStateList;
import android.graphics.Color;
import android.graphics.Typeface;
import android.graphics.drawable.GradientDrawable;
import android.graphics.drawable.RippleDrawable;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.text.InputFilter;
import android.text.InputType;
import android.text.TextUtils;
import android.view.Gravity;
import android.view.View;
import android.view.WindowInsets;
import android.view.WindowManager;
import android.view.inputmethod.EditorInfo;
import android.view.inputmethod.InputMethodManager;
import android.webkit.MimeTypeMap;
import android.widget.Button;
import android.widget.EditText;
import android.widget.FrameLayout;
import android.widget.ImageView;
import android.widget.LinearLayout;
import android.widget.ProgressBar;
import android.widget.ScrollView;
import android.widget.TextView;

import java.util.ArrayList;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Locale;
import java.util.Set;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

public final class MainActivity extends Activity {
    private static final int SCAN = 1;
    private static final int PICK = 2;
    private static final int SAVE = 3;
    private static final int CANVAS = 0xfff6f7fb;
    private static final int WHITE = Color.WHITE;
    private static final int INK = 0xff182039;
    private static final int MUTED = 0xff617089;
    private static final int LINE = 0xffe5e8f1;
    private static final int BRAND = 0xff4d45d5;
    private static final int LILAC = 0xffeeedff;
    private static final int GREEN = 0xff157b61;
    private static final int MINT = 0xffe4f5ef;

    private final Handler handler = new Handler(Looper.getMainLooper());
    private final ExecutorService connectionWorker = Executors.newSingleThreadExecutor();
    private final ExecutorService fileWorker = Executors.newSingleThreadExecutor();
    private final Runnable pollTask = new Runnable() {
        @Override
        public void run() {
            poll();
            if (visible) handler.postDelayed(this, 1600);
        }
    };
    private SharedPreferences prefs;
    private TransferApi.Session session;
    private TransferApi.State state;
    private TransferApi.Record pendingSave;
    private boolean visible;
    private boolean online;
    private boolean polling;
    private boolean pairing;
    private boolean transferring;
    private boolean leaving;
    private int filter;
    private String rendered = "";
    private String hint = "";
    private String addressDraft = "";
    private TextView status;
    private TextView message;
    private TextView progressText;
    private ProgressBar progress;
    private Button scan;
    private Button connect;
    private Button chooseFiles;
    private EditText address;
    private EditText code;
    private LinearLayout historyRows;
    private Button[] filters;

    @Override
    protected void onCreate(Bundle saved) {
        super.onCreate(saved);
        prefs = getSharedPreferences("near-transfer", MODE_PRIVATE);
        session = TransferApi.restore(prefs.getString("origin", null), prefs.getString("cookie", null));
        addressDraft = prefs.getString("last-address", "");
        if (saved != null && saved.containsKey("save-id")) {
            pendingSave = new TransferApi.Record(saved.getString("save-id"), saved.getString("save-name"),
                    saved.getLong("save-size"), saved.getBoolean("save-incoming"));
        }
        getWindow().setSoftInputMode(WindowManager.LayoutParams.SOFT_INPUT_ADJUST_RESIZE);
        if (Build.VERSION.SDK_INT >= 30) {
            getWindow().setDecorFitsSystemWindows(false);
        }
        showScreen();
    }

    @Override
    protected void onStart() {
        super.onStart();
        visible = true;
        handler.post(pollTask);
    }

    @Override
    protected void onStop() {
        visible = false;
        handler.removeCallbacks(pollTask);
        super.onStop();
    }

    @Override
    protected void onSaveInstanceState(Bundle out) {
        super.onSaveInstanceState(out);
        if (pendingSave != null) {
            out.putString("save-id", pendingSave.id);
            out.putString("save-name", pendingSave.name);
            out.putLong("save-size", pendingSave.size);
            out.putBoolean("save-incoming", pendingSave.incoming);
        }
    }

    @Override
    protected void onDestroy() {
        handler.removeCallbacksAndMessages(null);
        connectionWorker.shutdownNow();
        fileWorker.shutdownNow();
        super.onDestroy();
    }

    private void showScreen() {
        rendered = "";
        ScrollView root = new ScrollView(this);
        root.setFillViewport(true);
        root.setBackground(new GradientDrawable(GradientDrawable.Orientation.TOP_BOTTOM, new int[]{0xfff0f0fc, CANVAS, CANVAS}));
        if (Build.VERSION.SDK_INT >= 30) {
            root.setOnApplyWindowInsetsListener((view, insets) -> {
                android.graphics.Insets bars = insets.getInsets(WindowInsets.Type.systemBars() | WindowInsets.Type.displayCutout());
                int keyboard = insets.getInsets(WindowInsets.Type.ime()).bottom;
                view.setPadding(bars.left, bars.top, bars.right, Math.max(bars.bottom, keyboard));
                return insets;
            });
        }
        LinearLayout page = column();
        page.setFocusableInTouchMode(true);
        int pageEdge = Math.max(dp(16), (getResources().getDisplayMetrics().widthPixels - dp(620)) / 2);
        page.setPadding(pageEdge, dp(7), pageEdge, dp(20));
        root.addView(page, new ScrollView.LayoutParams(-1, -2));

        LinearLayout toolbar = row();
        ImageView mark = image(R.drawable.near_brand);
        toolbar.addView(mark, new LinearLayout.LayoutParams(dp(36), dp(36)));
        TextView brand = label("近传", 20, INK, true);
        LinearLayout.LayoutParams brandParams = new LinearLayout.LayoutParams(0, -2, 1);
        brandParams.leftMargin = dp(9);
        toolbar.addView(brand, brandParams);
        status = label("", 12, MUTED, true);
        status.setPadding(dp(10), dp(6), dp(10), dp(6));
        toolbar.addView(status);
        Button help = button("帮助", MUTED, Color.TRANSPARENT, false);
        help.setOnClickListener(v -> showHelp());
        toolbar.addView(help, new LinearLayout.LayoutParams(-2, dp(46)));
        page.addView(toolbar, new LinearLayout.LayoutParams(-1, dp(53)));

        TextView title = label("与电脑互传", 32, INK, true);
        add(page, title, 15);
        String description = session == null ? "和电脑连上同一个 Wi‑Fi，即可收发文件。" : "选择要发送的文件，或保存电脑发来的文件。";
        add(page, label(description, 14, MUTED, false), 5);
        message = label(hint, 13, 0xff80501f, false);
        message.setPadding(dp(11), dp(9), dp(11), dp(9));
        message.setBackground(surface(0xfffff4e2, 0xfff0d9b6, 10));
        message.setVisibility(hint.isEmpty() ? View.GONE : View.VISIBLE);
        add(page, message, 12);

        if (session == null) buildPairing(page);
        else buildTransfers(page);

        LinearLayout footer = row();
        ImageView wifi = image(R.drawable.near_wifi);
        wifi.setColorFilter(MUTED);
        footer.addView(wifi, new LinearLayout.LayoutParams(dp(16), dp(16)));
        TextView text = label("局域网直传", 12, MUTED, false);
        text.setPadding(dp(5), 0, 0, 0);
        footer.addView(text, new LinearLayout.LayoutParams(0, -2, 1));
        if (session != null) {
            Button disconnect = button("更换连接", MUTED, Color.TRANSPARENT, false);
            disconnect.setOnClickListener(v -> askLeave());
            footer.addView(disconnect, new LinearLayout.LayoutParams(-2, dp(44)));
        }
        add(page, footer, 12);
        setContentView(root);
        page.requestFocus();
        renderStatus();
    }

    private void buildPairing(LinearLayout page) {
        LinearLayout scanner = column();
        GradientDrawable dark = new GradientDrawable(GradientDrawable.Orientation.TL_BR,
                new int[]{0xff25264e, 0xff40377d});
        dark.setCornerRadius(dp(20));
        scanner.setBackground(dark);
        scanner.setPadding(dp(18), dp(17), dp(18), dp(17));
        LinearLayout titleRow = row();
        LinearLayout scannerText = column();
        add(scannerText, label("扫码连接", 21, WHITE, true), 0);
        add(scannerText, label("扫描电脑上近传显示的二维码", 13, 0xffdedbf4, false), 5);
        titleRow.addView(scannerText, new LinearLayout.LayoutParams(0, -2, 1));
        titleRow.addView(tile(R.drawable.near_scan, 0xff504886, 0xffe8e3ff, 44));
        add(scanner, titleRow, 0);
        scan = button("打开相机", BRAND, WHITE, true);
        scan.setOnClickListener(v -> startActivityForResult(new Intent(this, ScanActivity.class), SCAN));
        add(scanner, scan, 16, 48);
        add(page, scanner, 20);

        LinearLayout manual = card();
        add(manual, label("手动连接", 19, INK, true), 0);
        add(manual, label("在电脑端二维码下方查看地址和配对码。", 13, MUTED, false), 5);
        add(manual, label("电脑地址", 13, INK, true), 13);
        address = field("192.168.1.20:47331", false);
        address.setText(addressDraft);
        address.setImeOptions(EditorInfo.IME_ACTION_NEXT);
        address.setId(View.generateViewId());
        add(manual, address, 5);
        add(manual, label("六位配对码", 13, INK, true), 10);
        code = field("输入六位数字", true);
        code.setId(View.generateViewId());
        code.setFilters(new InputFilter[]{new InputFilter.LengthFilter(7)});
        code.setImeOptions(EditorInfo.IME_ACTION_DONE);
        address.setNextFocusForwardId(code.getId());
        code.setOnEditorActionListener((v, action, event) -> {
            if (action != EditorInfo.IME_ACTION_DONE) return false;
            manualPair();
            return true;
        });
        add(manual, code, 5);
        connect = button("连接电脑", WHITE, BRAND, true);
        connect.setOnClickListener(v -> manualPair());
        add(manual, connect, 13, 48);
        add(page, manual, 13);
    }

    private void buildTransfers(LinearLayout page) {
        LinearLayout sender = card();
        LinearLayout sendHeading = row();
        sendHeading.addView(label("发送文件", 19, INK, true), new LinearLayout.LayoutParams(0, -2, 1));
        TextView route = label("手机  →  电脑", 12, 0xff575c85, true);
        route.setBackground(surface(0xfff0f1f8, Color.TRANSPARENT, 8));
        route.setPadding(dp(8), dp(5), dp(8), dp(5));
        sendHeading.addView(route);
        add(sender, sendHeading, 0);

        LinearLayout well = column();
        well.setGravity(Gravity.CENTER_HORIZONTAL);
        well.setPadding(dp(12), dp(15), dp(12), dp(15));
        well.setBackground(surface(0xfff7f6ff, 0xffe6e2fa, 13));
        well.addView(tile(R.drawable.near_upload, 0xffe9e4ff, BRAND, 48));
        TextView subtitle = label("选择要发送的文件", 17, INK, true);
        subtitle.setGravity(Gravity.CENTER);
        add(well, subtitle, 9);
        TextView detail = label("照片、视频和文档都可以多选", 13, MUTED, false);
        detail.setGravity(Gravity.CENTER);
        add(well, detail, 4);
        chooseFiles = button("选择文件", WHITE, BRAND, true);
        chooseFiles.setOnClickListener(v -> pickFiles());
        LinearLayout.LayoutParams chooseParams = new LinearLayout.LayoutParams(dp(143), dp(48));
        chooseParams.topMargin = dp(13);
        well.addView(chooseFiles, chooseParams);
        add(sender, well, 12);

        progressText = label("", 12, MUTED, false);
        progressText.setVisibility(View.GONE);
        add(sender, progressText, 10);
        progress = new ProgressBar(this, null, android.R.attr.progressBarStyleHorizontal);
        progress.setProgressTintList(ColorStateList.valueOf(BRAND));
        progress.setMax(100);
        progress.setVisibility(View.GONE);
        add(sender, progress, 4, 7);
        add(page, sender, 19);

        LinearLayout history = card();
        LinearLayout historyHeading = row();
        ImageView historyIcon = image(R.drawable.near_history);
        historyIcon.setColorFilter(0xff6666a1);
        historyHeading.addView(historyIcon, new LinearLayout.LayoutParams(dp(18), dp(18)));
        LinearLayout.LayoutParams historyTitle = new LinearLayout.LayoutParams(0, -2, 1);
        historyTitle.leftMargin = dp(6);
        historyHeading.addView(label("传输记录", 19, INK, true), historyTitle);
        LinearLayout tabs = row();
        tabs.setBackground(surface(0xffeff1f7, Color.TRANSPARENT, 9));
        tabs.setPadding(dp(2), dp(2), dp(2), dp(2));
        filters = new Button[3];
        String[] names = {"全部", "收到", "发出"};
        for (int i = 0; i < names.length; i++) {
            int selected = i;
            Button tab = button(names[i], MUTED, Color.TRANSPARENT, false);
            tab.setTextSize(12);
            tab.setPadding(dp(8), 0, dp(8), 0);
            tab.setOnClickListener(v -> {
                filter = selected;
                renderFiles();
            });
            filters[i] = tab;
            tabs.addView(tab, new LinearLayout.LayoutParams(-2, dp(40)));
        }
        historyHeading.addView(tabs);
        add(history, historyHeading, 0);
        historyRows = column();
        add(history, historyRows, 9);
        add(page, history, 13);
        renderFiles();
    }

    private void manualPair() {
        if (pairing) return;
        addressDraft = address.getText().toString().trim();
        try {
            pair(TransferApi.manual(addressDraft, code.getText().toString()));
        } catch (IllegalArgumentException invalid) {
            showHint(invalid.getMessage());
        }
    }

    private void pair(TransferApi.Target target) {
        if (pairing) return;
        if (getCurrentFocus() != null) {
            getSystemService(InputMethodManager.class).hideSoftInputFromWindow(getCurrentFocus().getWindowToken(), 0);
        }
        pairing = true;
        showHint("");
        renderStatus();
        connectionWorker.execute(() -> {
            try {
                TransferApi.Session next = TransferApi.pair(target);
                updateUI(() -> {
                    pairing = false;
                    session = next;
                    addressDraft = next.origin.substring("http://".length());
                    prefs.edit().putString("origin", next.origin).putString("cookie", next.cookie)
                            .putString("last-address", addressDraft).apply();
                    showScreen();
                    poll();
                });
            } catch (Exception error) {
                updateUI(() -> {
                    pairing = false;
                    renderStatus();
                    showHint(transferMessage(error, "无法连接电脑。请确认两台设备在同一个 Wi‑Fi，并检查电脑端是否允许连接。"));
                });
            }
        });
    }

    private void poll() {
        TransferApi.Session current = session;
        if (current == null || polling || leaving) return;
        polling = true;
        connectionWorker.execute(() -> {
            try {
                TransferApi.State next = TransferApi.state(current);
                updateUI(() -> {
                    polling = false;
                    if (session != current) return;
                    boolean recovered = !online;
                    state = next;
                    online = true;
                    if (recovered) showHint("");
                    renderStatus();
                    renderFiles();
                });
            } catch (Exception error) {
                updateUI(() -> {
                    polling = false;
                    if (session != current) return;
                    if (error instanceof TransferApi.HttpError && ((TransferApi.HttpError) error).status == 401) {
                        clearSession("连接已结束，请扫描新二维码。");
                        return;
                    }
                    online = false;
                    showHint("正在重连电脑。请保持电脑上的近传程序开启。");
                    renderStatus();
                    renderFiles();
                });
            }
        });
    }

    private void renderStatus() {
        if (session == null) {
            status.setText(pairing ? "正在连接" : "等待配对");
            status.setTextColor(MUTED);
            status.setBackground(surface(0xffeceef7, Color.TRANSPARENT, 99));
            scan.setEnabled(!pairing);
            connect.setEnabled(!pairing);
        } else {
            status.setText(online ? "已连接电脑" : "正在连接");
            status.setTextColor(online ? GREEN : 0xff8a5b29);
            status.setBackground(surface(online ? MINT : 0xffffedd7, Color.TRANSPARENT, 99));
            chooseFiles.setEnabled(online && !transferring && !leaving);
        }
    }

    private void renderFiles() {
        if (session == null) return;
        List<TransferApi.Record> files = state == null ? new ArrayList<>() : state.files;
        StringBuilder key = new StringBuilder().append(filter).append(online).append(transferring).append(leaving);
        for (TransferApi.Record file : files) key.append(file.id);
        if (rendered.equals(key.toString())) return;
        rendered = key.toString();
        for (int i = 0; i < filters.length; i++) {
            filters[i].setSelected(i == filter);
            filters[i].setTextColor(i == filter ? BRAND : MUTED);
            filters[i].setBackground(clickSurface(i == filter ? WHITE : 0xffeff1f7, 7));
        }
        historyRows.removeAllViews();
        for (TransferApi.Record file : files) {
            if (filter == 0 || (filter == 1 && file.incoming) || (filter == 2 && !file.incoming)) {
                addFileRow(file);
            }
        }
        if (historyRows.getChildCount() == 0) {
            LinearLayout empty = column();
            empty.setGravity(Gravity.CENTER_HORIZONTAL);
            empty.setPadding(0, dp(13), 0, dp(12));
            empty.addView(tile(R.drawable.near_inbox, 0xffefeffb, 0xff767baa, 38));
            TextView title = label(filter == 1 ? "还没有收到文件" : filter == 2 ? "还没有发出文件" : "还没有传输记录", 14, 0xff414c65, true);
            title.setGravity(Gravity.CENTER);
            add(empty, title, 6);
            TextView description = label("收发的文件会出现在这里", 12, MUTED, false);
            description.setGravity(Gravity.CENTER);
            add(empty, description, 3);
            add(historyRows, empty, 0);
        }
    }

    private void addFileRow(TransferApi.Record file) {
        View divider = new View(this);
        divider.setBackgroundColor(LINE);
        historyRows.addView(divider, new LinearLayout.LayoutParams(-1, dp(1)));
        LinearLayout item = row();
        item.setPadding(0, dp(5), 0, dp(5));
        item.addView(tile(R.drawable.near_file, file.incoming ? MINT : LILAC, file.incoming ? GREEN : BRAND, 37));
        LinearLayout details = column();
        TextView name = label(file.name, 14, INK, true);
        name.setMaxLines(1);
        name.setEllipsize(TextUtils.TruncateAt.MIDDLE);
        add(details, name, 0);
        add(details, label((file.incoming ? "收到" : "发出") + " · " + fileSize(file.size), 12, MUTED, false), 2);
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(0, -2, 1);
        params.setMargins(dp(8), 0, dp(6), 0);
        item.addView(details, params);
        Button download = button("下载", BRAND, LILAC, true);
        download.setTextSize(13);
        download.setContentDescription("下载 " + file.name);
        download.setEnabled(online && !transferring && !leaving);
        download.setOnClickListener(v -> pickDestination(file));
        item.addView(download, new LinearLayout.LayoutParams(dp(61), dp(45)));
        add(historyRows, item, 0);
    }

    private void pickFiles() {
        Intent intent = new Intent(Intent.ACTION_OPEN_DOCUMENT).setType("*/*");
        intent.addCategory(Intent.CATEGORY_OPENABLE).putExtra(Intent.EXTRA_ALLOW_MULTIPLE, true);
        try {
            startActivityForResult(intent, PICK);
        } catch (RuntimeException unavailable) {
            showHint("此手机上无法打开系统文件选择器。");
        }
    }

    private void pickDestination(TransferApi.Record record) {
        pendingSave = record;
        int dot = record.name.lastIndexOf('.');
        String type = dot < 0 ? null : MimeTypeMap.getSingleton()
                .getMimeTypeFromExtension(record.name.substring(dot + 1).toLowerCase(Locale.ROOT));
        Intent intent = new Intent(Intent.ACTION_CREATE_DOCUMENT).setType(type == null ? "application/octet-stream" : type);
        intent.addCategory(Intent.CATEGORY_OPENABLE).putExtra(Intent.EXTRA_TITLE, record.name);
        try {
            startActivityForResult(intent, SAVE);
        } catch (RuntimeException unavailable) {
            pendingSave = null;
            showHint("此手机上无法打开系统文件选择器。");
        }
    }

    @Override
    protected void onActivityResult(int request, int result, Intent data) {
        super.onActivityResult(request, result, data);
        if (request == SCAN) {
            if (result == RESULT_OK && data != null) {
                try {
                    pair(TransferApi.scan(data.getStringExtra("pairing")));
                } catch (IllegalArgumentException invalid) {
                    showHint("请扫描电脑端近传显示的二维码。");
                }
            } else if (data != null && data.hasExtra("message")) {
                showHint(data.getStringExtra("message"));
            }
        } else if (request == PICK && result == RESULT_OK && data != null) {
            Set<Uri> chosen = new LinkedHashSet<>();
            ClipData clip = data.getClipData();
            if (clip != null) {
                for (int i = 0; i < clip.getItemCount(); i++) {
                    Uri uri = clip.getItemAt(i).getUri();
                    if (uri != null) chosen.add(uri);
                }
            }
            if (data.getData() != null) chosen.add(data.getData());
            if (!chosen.isEmpty()) sendFiles(new ArrayList<>(chosen));
        } else if (request == SAVE) {
            TransferApi.Record record = pendingSave;
            pendingSave = null;
            if (record != null && result == RESULT_OK && data != null && data.getData() != null) download(record, data.getData());
        }
    }

    private void sendFiles(List<Uri> files) {
        TransferApi.Session current = session;
        if (current == null || state == null || transferring) {
            showHint("请连接电脑后重新选择文件。");
            return;
        }
        int room = state.room;
        startProgress("正在准备 " + files.size() + " 个文件");
        fileWorker.execute(() -> {
            int sent = 0;
            for (int i = 0; i < files.size(); i++) {
                String batch = " (" + (i + 1) + "/" + files.size() + ")";
                try (FileAccess.Source source = FileAccess.from(this, files.get(i))) {
                    String caption = "发送 " + source.name + batch;
                    updateUI(() -> { if (session == current) reportProgress(caption, 0); });
                    TransferApi.send(current, room, source, (done, total) -> updateUI(() -> {
                        if (session == current) reportProgress(caption, percent(done, total));
                    }));
                    sent++;
                } catch (Exception error) {
                    String prefix = sent == 0 ? "" : "已发送 " + sent + " 个文件。";
                    updateUI(() -> {
                        if (session == current) finishProgress(prefix + transferMessage(error, "发送中断，请重新选择未发出的文件。"), true);
                    });
                    return;
                }
            }
            updateUI(() -> { if (session == current) finishProgress("已发送 " + files.size() + " 个文件。", false); });
        });
    }

    private void download(TransferApi.Record record, Uri uri) {
        FileAccess.Destination destination = new FileAccess.Destination(getContentResolver(), uri);
        TransferApi.Session current = session;
        if (current == null) {
            fileWorker.execute(destination::discard);
            showHint("连接已结束，请重新连接后下载。");
            return;
        }
        String caption = "下载 " + record.name;
        startProgress(caption);
        fileWorker.execute(() -> {
            try {
                TransferApi.receive(current, record, destination, (done, total) -> updateUI(() -> {
                    if (session == current) reportProgress(caption, percent(done, total));
                }));
                updateUI(() -> { if (session == current) finishProgress("已保存 " + record.name, false); });
            } catch (Exception error) {
                destination.discard();
                updateUI(() -> {
                    if (session == current) finishProgress(transferMessage(error, "保存失败，请检查网络和手机存储空间。"), true);
                });
            }
        });
    }

    private void startProgress(String caption) {
        transferring = true;
        showHint("");
        progress.setVisibility(View.VISIBLE);
        progressText.setVisibility(View.VISIBLE);
        progressText.setTextColor(MUTED);
        reportProgress(caption, 0);
        renderStatus();
        renderFiles();
    }

    private void reportProgress(String caption, int percent) {
        if (session == null || !transferring) return;
        progress.setProgress(percent);
        progressText.setText(caption + " · " + percent + "%");
    }

    private void finishProgress(String caption, boolean failed) {
        transferring = false;
        if (session == null) return;
        progress.setVisibility(View.GONE);
        progressText.setVisibility(View.VISIBLE);
        progressText.setTextColor(failed ? 0xffa0443d : GREEN);
        progressText.setText(caption);
        renderStatus();
        renderFiles();
        poll();
    }

    private void askLeave() {
        if (leaving) return;
        if (transferring) {
            showHint("请等待当前文件传输完成后再更换连接。");
            return;
        }
        new AlertDialog.Builder(this)
                .setTitle("更换连接")
                .setMessage("当前设备会断开，电脑上的中转文件将清空。请先下载要保留的文件。")
                .setNegativeButton("取消", null)
                .setPositiveButton("断开", (dialog, button) -> {
                    TransferApi.Session current = session;
                    if (current == null) return;
                    leaving = true;
                    renderStatus();
                    renderFiles();
                    connectionWorker.execute(() -> {
                        String notice = "";
                        try {
                            TransferApi.leave(current);
                        } catch (Exception error) {
                            notice = "请在原电脑端更换连接，结束原会话。";
                        }
                        String finalNotice = notice;
                        updateUI(() -> {
                            leaving = false;
                            clearSession(finalNotice);
                        });
                    });
                }).show();
    }

    private void clearSession(String notice) {
        session = null;
        state = null;
        online = false;
        transferring = false;
        filter = 0;
        hint = notice;
        prefs.edit().remove("origin").remove("cookie").apply();
        showScreen();
    }

    private void showHelp() {
        new AlertDialog.Builder(this).setTitle("使用近传")
                .setMessage("让手机和电脑连上同一个 Wi‑Fi。在手机上扫码，或填写电脑上显示的地址和配对码。\n\n"
                        + "一端发送，另一端在传输记录中点“下载”。安卓会打开系统文件选择器，让你选择保存位置。\n\n"
                        + "如果无法连接，请检查 Windows 防火墙设置。传输时请保持电脑端程序开启。")
                .setPositiveButton("知道了", null).show();
    }

    private void showHint(String text) {
        hint = text;
        message.setText(text);
        message.setVisibility(text.isEmpty() ? View.GONE : View.VISIBLE);
    }

    private void updateUI(Runnable work) {
        handler.post(() -> { if (!isDestroyed()) work.run(); });
    }

    private static String transferMessage(Exception error, String fallback) {
        return error instanceof TransferApi.HttpError ? error.getMessage() : fallback;
    }

    private static int percent(long done, long total) {
        return total == 0 ? 100 : (int) (100.0 * done / total);
    }

    private static String fileSize(long bytes) {
        String[] units = {"B", "KB", "MB", "GB", "TB"};
        double size = bytes;
        int unit = 0;
        while (size >= 1024 && unit < units.length - 1) {
            unit++;
            size /= 1024;
        }
        return String.format(Locale.ROOT, unit == 0 ? "%.0f %s" : "%.1f %s", size, units[unit]);
    }

    private LinearLayout row() {
        LinearLayout view = new LinearLayout(this);
        view.setGravity(Gravity.CENTER_VERTICAL);
        return view;
    }

    private LinearLayout column() {
        LinearLayout view = new LinearLayout(this);
        view.setOrientation(LinearLayout.VERTICAL);
        return view;
    }

    private LinearLayout card() {
        LinearLayout view = column();
        view.setPadding(dp(15), dp(14), dp(15), dp(13));
        view.setBackground(surface(WHITE, LINE, 18));
        return view;
    }

    private TextView label(String content, float size, int color, boolean strong) {
        TextView view = new TextView(this);
        view.setText(content);
        view.setTextColor(color);
        view.setTextSize(size);
        view.setIncludeFontPadding(false);
        if (strong) view.setTypeface(Typeface.create("sans-serif-medium", Typeface.NORMAL));
        return view;
    }

    private Button button(String content, int text, int fill, boolean bold) {
        Button view = new Button(this);
        view.setText(content);
        view.setAllCaps(false);
        view.setTextSize(14);
        view.setTextColor(new ColorStateList(new int[][]{new int[]{-android.R.attr.state_enabled}, new int[]{}},
                new int[]{(text & 0x00ffffff) | 0x77000000, text}));
        view.setMinHeight(0);
        view.setMinimumHeight(0);
        view.setMinWidth(0);
        view.setMinimumWidth(0);
        view.setPadding(dp(9), dp(2), dp(9), dp(2));
        view.setStateListAnimator(null);
        view.setBackground(clickSurface(fill, 9));
        if (bold) view.setTypeface(Typeface.create("sans-serif-medium", Typeface.NORMAL));
        return view;
    }

    private EditText field(String placeholder, boolean numeric) {
        EditText input = new EditText(this);
        input.setTextSize(15);
        input.setTextColor(INK);
        input.setHintTextColor(0xff6b7690);
        input.setHint(placeholder);
        input.setSingleLine();
        input.setInputType(numeric ? InputType.TYPE_CLASS_NUMBER : InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_URI);
        input.setMinHeight(dp(47));
        input.setPadding(dp(10), dp(8), dp(10), dp(8));
        input.setBackground(surface(0xfff8f9fd, LINE, 9));
        return input;
    }

    private ImageView image(int resource) {
        ImageView tile = new ImageView(this);
        tile.setImageResource(resource);
        tile.setImportantForAccessibility(View.IMPORTANT_FOR_ACCESSIBILITY_NO);
        return tile;
    }

    private FrameLayout tile(int resource, int background, int glyph, int size) {
        FrameLayout tile = new FrameLayout(this);
        tile.setLayoutParams(new LinearLayout.LayoutParams(dp(size), dp(size)));
        tile.setBackground(surface(background, Color.TRANSPARENT, 11));
        ImageView graphic = image(resource);
        graphic.setColorFilter(glyph);
        tile.addView(graphic, new FrameLayout.LayoutParams(dp(size / 2), dp(size / 2), Gravity.CENTER));
        return tile;
    }

    private GradientDrawable surface(int fill, int border, int radius) {
        GradientDrawable shape = new GradientDrawable();
        shape.setColor(fill);
        shape.setCornerRadius(dp(radius));
        if (border != Color.TRANSPARENT) shape.setStroke(dp(1), border);
        return shape;
    }

    private RippleDrawable clickSurface(int fill, int radius) {
        return new RippleDrawable(ColorStateList.valueOf(0x22837adf), surface(fill, Color.TRANSPARENT, radius),
                surface(WHITE, Color.TRANSPARENT, radius));
    }

    private void add(LinearLayout parent, View child, int top) { add(parent, child, top, -2); }

    private void add(LinearLayout parent, View child, int top, int height) {
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(-1, height < 0 ? height : dp(height));
        params.topMargin = dp(top);
        parent.addView(child, params);
    }

    private int dp(float value) { return Math.round(value * getResources().getDisplayMetrics().density); }
}
