package cn.jinchuan.transfer;

import android.app.Activity;
import android.app.AlertDialog;
import android.content.ClipData;
import android.content.ClipboardManager;
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

import java.io.File;
import java.io.FileOutputStream;
import java.io.OutputStream;
import java.util.ArrayList;
import java.util.Collections;
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
    private FileTransfer fileTransfer;
    private PendingDownload pendingDownload;
    private boolean choosingDestination;
    private boolean visible;
    private boolean online;
    private boolean polling;
    private boolean pairing;
    private boolean leaving;
    private int filter;
    private String rendered = "";
    private String hint = "";
    private String addressDraft = "";
    private TextView status;
    private TextView transport;
    private TextView message;
    private LinearLayout progressPanel;
    private TextView progressText;
    private ProgressBar progress;
    private Button cancelButton;
    private final Runnable hideTransferResult = () -> {
        if (fileTransfer == null && progressPanel != null) progressPanel.setVisibility(View.GONE);
    };
    private Button scan;
    private Button connect;
    private Button chooseFiles;
    private Button textButton;
    private TextDialog textDialog;
    private String seenText = "";
    private EditText address;
    private EditText code;
    private LinearLayout historyRows;
    private Button[] filters;

    private static final class PendingDownload {
        final String name;
        final File staged;

        PendingDownload(String name, File staged) {
            this.name = name;
            this.staged = staged;
        }
    }

    @Override
    protected void onCreate(Bundle saved) {
        super.onCreate(saved);
        prefs = getSharedPreferences("near-transfer", MODE_PRIVATE);
        session = TransferApi.restore(prefs.getString("origin", null), prefs.getString("device-id", null),
                prefs.getString("device-key", null), prefs.getString("remote-origin", ""));
        addressDraft = prefs.getString("last-address", "");
        if (session != null && saved != null) {
            String cached = saved.getString("download-file", "");
            if (cached.startsWith("near-in-") && cached.endsWith(".part") && cached.equals(new File(cached).getName())) {
                File staged = new File(getCacheDir(), cached);
                if (staged.isFile()) {
                    pendingDownload = new PendingDownload(saved.getString("download-name", "文件"), staged);
                    choosingDestination = saved.getBoolean("download-picker");
                    fileTransfer = new FileTransfer();
                    fileTransfer.finish();
                }
            }
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
        if (pendingDownload != null && !choosingDestination) pickDestination();
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
        if (pendingDownload != null) {
            out.putString("download-file", pendingDownload.staged.getName());
            out.putString("download-name", pendingDownload.name);
            out.putBoolean("download-picker", choosingDestination);
        }
    }

    @Override
    protected void onDestroy() {
        if (textDialog != null) textDialog.dialog.dismiss();
        if (fileTransfer != null) fileTransfer.cancel();
        if (isFinishing() && pendingDownload != null) pendingDownload.staged.delete();
        // Already posted download results still need to release their cache files.
        handler.removeCallbacks(pollTask);
        handler.removeCallbacks(hideTransferResult);
        connectionWorker.shutdownNow();
        fileWorker.shutdown();
        super.onDestroy();
    }

    private void showScreen() {
        rendered = "";
        LinearLayout screen = column();
        screen.setBackground(new GradientDrawable(GradientDrawable.Orientation.TOP_BOTTOM, new int[]{0xfff0f0fc, CANVAS, CANVAS}));
        ScrollView root = new ScrollView(this);
        root.setFillViewport(true);
        if (Build.VERSION.SDK_INT >= 30) {
            screen.setOnApplyWindowInsetsListener((view, insets) -> {
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
        screen.addView(root, new LinearLayout.LayoutParams(-1, 0, 1));

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
        String description = session == null ? "在同一局域网和电脑完成配对。" : "选择要发送的文件，或保存电脑发来的文件。";
        add(page, label(description, 14, MUTED, false), 5);
        message = label(hint, 13, 0xff80501f, false);
        message.setPadding(dp(11), dp(9), dp(11), dp(9));
        message.setBackground(surface(0xfffff4e2, 0xfff0d9b6, 10));
        message.setVisibility(hint.isEmpty() ? View.GONE : View.VISIBLE);
        add(page, message, 12);

        if (session == null) {
            buildPairing(page);
        } else {
            buildTransfers(page);
            buildProgress(screen, pageEdge);
            if (pendingDownload != null) showSaveProgress("请选择保存位置");
        }

        LinearLayout footer = row();
        ImageView wifi = image(R.drawable.near_wifi);
        wifi.setColorFilter(MUTED);
        footer.addView(wifi, new LinearLayout.LayoutParams(dp(16), dp(16)));
        transport = label("局域网直传", 12, MUTED, false);
        transport.setPadding(dp(5), 0, 0, 0);
        footer.addView(transport, new LinearLayout.LayoutParams(0, -2, 1));
        if (session != null) {
            Button settings = button("连接设置", MUTED, Color.TRANSPARENT, false);
            settings.setOnClickListener(v -> showConnectionSettings());
            footer.addView(settings, new LinearLayout.LayoutParams(-2, dp(44)));
        }
        add(page, footer, 12);
        setContentView(screen);
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
        add(scannerText, label("配对一次，以后打开近传会自动连接", 13, 0xffdedbf4, false), 5);
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
        textButton = button("传文字", BRAND, LILAC, true);
        textButton.setTextSize(13);
        textButton.setOnClickListener(v -> openTexts());
        LinearLayout.LayoutParams textButtonParams = new LinearLayout.LayoutParams(-2, dp(37));
        textButtonParams.leftMargin = dp(7);
        sendHeading.addView(textButton, textButtonParams);
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
                    addressDraft = next.route.origin.substring("http://".length());
                    prefs.edit().putString("origin", next.route.origin).putString("device-id", next.id)
                            .putString("device-key", next.key).putString("last-address", addressDraft).remove("cookie").apply();
                    showScreen();
                    poll();
                });
            } catch (Exception error) {
                updateUI(() -> {
                    pairing = false;
                    renderStatus();
                    showHint(transferMessage(error, "无法连接电脑。请确认两台设备在同一局域网，并检查电脑端是否允许连接。"));
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
                    if (session != current || current.route.ticket == null) return;
                    boolean recovered = !online;
                    state = next;
                    online = true;
                    String origin = current.lanOrigin;
                    if (!origin.equals(prefs.getString("origin", ""))) {
                        addressDraft = origin.substring("http://".length());
                        prefs.edit().putString("origin", origin).putString("last-address", addressDraft).apply();
                    }
                    if (recovered) showHint("");
                    renderStatus();
                    renderFiles();
                    if (textDialog != null) textDialog.refresh();
                });
            } catch (Exception error) {
                updateUI(() -> {
                    polling = false;
                    if (session != current) return;
                    online = false;
                    showHint(current.remoteOrigin.isEmpty()
                            ? "电脑暂时离线。在同一局域网打开两端近传后会自动重连。"
                            : "电脑暂时离线。异地使用时，请确认电脑已唤醒，且两端 Tailscale 已连接。");
                    renderStatus();
                    renderFiles();
                    if (textDialog != null) textDialog.refresh();
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
            String connected = session.route.remote() ? "已异地连接" : "已连接电脑";
            status.setText(online ? connected : "自动重连中");
            transport.setText(session.route.remote() ? "Tailscale 加密连接" : "局域网直传");
            status.setTextColor(online ? GREEN : 0xff8a5b29);
            status.setBackground(surface(online ? MINT : 0xffffedd7, Color.TRANSPARENT, 99));
            chooseFiles.setEnabled(online && fileTransfer == null && !leaving);
            renderTextButton();
        }
    }

    private void renderTextButton() {
        int unread = 0;
        if (state != null) {
            for (int i = state.texts.size() - 1; i >= 0; i--) {
                TransferApi.TextRecord item = state.texts.get(i);
                if (item.id.equals(seenText)) break;
                if (item.incoming) unread++;
            }
        }
        textButton.setText(unread == 0 ? "传文字" : "传文字 · " + Math.min(unread, 99));
    }

    private void openTexts() {
        if (textDialog != null || session == null) return;
        textDialog = new TextDialog(session);
        textDialog.dialog.show();
        textDialog.refresh();
    }

    private final class TextDialog {
        final TransferApi.Session owner;
        final AlertDialog dialog;
        final EditText draft;
        final TextView position;
        final TextView detail;
        final TextView note;
        final Button send;
        final Button previous;
        final Button next;
        final Button copy;

        List<TransferApi.TextRecord> records = Collections.emptyList();
        int selected = -1;
        String requested = "";
        String fullText;
        boolean sending;
        String lastDraft = "";
        String transferId = "";
        int lastRoom = -1;

        TextDialog(TransferApi.Session owner) {
            this.owner = owner;
            ScrollView scroll = new ScrollView(MainActivity.this);
            LinearLayout body = column();
            body.setPadding(dp(22), dp(8), dp(22), 0);
            scroll.addView(body);
            add(body, label("发送到电脑", 14, INK, true), 0);
            draft = new EditText(MainActivity.this);
            draft.setTextSize(14);
            draft.setHint("输入或粘贴文字");
            draft.setGravity(Gravity.TOP | Gravity.START);
            draft.setMinLines(3);
            draft.setMaxLines(6);
            draft.setTextColor(INK);
            draft.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_FLAG_MULTI_LINE);
            draft.setBackground(surface(0xfff8f9fd, LINE, 9));
            draft.setPadding(dp(9), dp(8), dp(9), dp(8));
            add(body, draft, 6);
            send = button("发送文字", WHITE, BRAND, true);
            send.setOnClickListener(v -> sendDraft());
            add(body, send, 9, 43);
            note = label("", 12, MUTED, false);
            note.setVisibility(View.GONE);
            add(body, note, 7);

            LinearLayout history = row();
            history.addView(label("本次文字记录", 15, INK, true), new LinearLayout.LayoutParams(0, -2, 1));
            position = label("", 12, MUTED, false);
            history.addView(position);
            add(body, history, 15);

            detail = label("", 14, INK, false);
            detail.setMinLines(3);
            detail.setMaxLines(6);
            detail.setEllipsize(TextUtils.TruncateAt.END);
            detail.setTextIsSelectable(true);
            detail.setBackground(surface(0xfff8f9fd, LINE, 9));
            detail.setPadding(dp(10), dp(8), dp(10), dp(8));
            add(body, detail, 7);

            LinearLayout actions = row();
            previous = button("上一条", MUTED, 0xffeef0f7, false);
            next = button("下一条", MUTED, 0xffeef0f7, false);
            copy = button("复制文字", BRAND, LILAC, true);
            previous.setOnClickListener(v -> select(selected - 1));
            next.setOnClickListener(v -> select(selected + 1));
            copy.setOnClickListener(v -> {
                if (fullText == null) return;
                getSystemService(ClipboardManager.class).setPrimaryClip(ClipData.newPlainText("近传", fullText));
                showNote("已复制到剪贴板");
            });
            actions.addView(previous, new LinearLayout.LayoutParams(-2, dp(42)));
            LinearLayout.LayoutParams nextParams = new LinearLayout.LayoutParams(-2, dp(42));
            nextParams.leftMargin = dp(5);
            actions.addView(next, nextParams);
            View spacer = new View(MainActivity.this);
            actions.addView(spacer, new LinearLayout.LayoutParams(0, 1, 1));
            actions.addView(copy, new LinearLayout.LayoutParams(-2, dp(42)));
            add(body, actions, 8);

            dialog = new AlertDialog.Builder(MainActivity.this).setTitle("传文字").setView(scroll)
                    .setNegativeButton("关闭", null).create();
            dialog.setOnDismissListener(ignored -> {
                if (textDialog == this) textDialog = null;
            });
            if (dialog.getWindow() != null) {
                dialog.getWindow().setSoftInputMode(WindowManager.LayoutParams.SOFT_INPUT_STATE_ALWAYS_HIDDEN);
            }
        }

        void refresh() {
            send.setEnabled(online && !sending && state != null);
            List<TransferApi.TextRecord> updated = state == null ? Collections.emptyList() : state.texts;
            if (!updated.isEmpty()) seenText = updated.get(updated.size() - 1).id;
            renderTextButton();
            String oldId = selected >= 0 ? records.get(selected).id : "";
            boolean followLatest = selected < 0 || selected == records.size() - 1;
            records = updated;
            int target = records.size() - 1;
            if (!followLatest) {
                for (int i = 0; i < records.size(); i++) {
                    if (records.get(i).id.equals(oldId)) {
                        target = i;
                        break;
                    }
                }
            }
            select(target);
        }

        void select(int index) {
            selected = index;
            previous.setEnabled(index > 0);
            next.setEnabled(index + 1 < records.size());
            if (index < 0) {
                requested = "";
                fullText = null;
                detail.setText("收到或发出的文字会显示在这里");
                position.setText("");
                copy.setEnabled(false);
                return;
            }
            TransferApi.TextRecord item = records.get(index);
            position.setText((item.incoming ? "收到" : "发出") + " · " + (index + 1) + "/" + records.size());
            if (requested.equals(item.id)) return;
            fullText = null;
            copy.setEnabled(false);
            detail.setText(item.preview);
            if (!online) return;
            requested = item.id;
            connectionWorker.execute(() -> {
                try {
                    String content = TransferApi.getText(owner, item);
                    updateUI(() -> {
                        if (textDialog != this || !requested.equals(item.id)) return;
                        fullText = content;
                        detail.setText(content);
                        copy.setEnabled(true);
                    });
                } catch (Exception error) {
                    updateUI(() -> {
                        if (textDialog == this && requested.equals(item.id)) requested = "";
                    });
                }
            });
        }

        void sendDraft() {
            if (sending || session != owner || state == null || !online) return;
            String content = draft.getText().toString();
            if (content.trim().isEmpty()) {
                showNote("请输入要发送的文字。");
                return;
            }
            int room = state.room;
            if (!content.equals(lastDraft) || room != lastRoom) {
                transferId = DeviceDiscovery.randomHex(16);
                lastDraft = content;
                lastRoom = room;
            }
            String attempted = transferId;
            sending = true;
            send.setEnabled(false);
            showNote("正在发送…");
            connectionWorker.execute(() -> {
                try {
                    TransferApi.sendText(owner, room, attempted, content);
                    updateUI(() -> {
                        if (textDialog != this) return;
                        sending = false;
                        lastDraft = "";
                        if (draft.getText().toString().equals(content)) draft.setText("");
                        selected = records.size() - 1;
                        showNote("文字已发送");
                        refresh();
                        poll();
                    });
                } catch (Exception error) {
                    updateUI(() -> {
                        if (textDialog != this) return;
                        sending = false;
                        send.setEnabled(online);
                        showNote(transferMessage(error, "发送失败，请重试。"));
                    });
                }
            });
        }

        void showNote(String value) {
            note.setText(value);
            note.setVisibility(View.VISIBLE);
        }
    }

    private void renderFiles() {
        if (session == null) return;
        List<TransferApi.Record> files = state == null ? new ArrayList<>() : state.files;
        StringBuilder key = new StringBuilder().append(filter).append(online).append(fileTransfer != null).append(leaving);
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
        download.setEnabled(online && fileTransfer == null && !leaving);
        download.setOnClickListener(v -> download(file));
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

    private void pickDestination() {
        String name = pendingDownload.name;
        int dot = name.lastIndexOf('.');
        String type = dot < 0 ? null : MimeTypeMap.getSingleton()
                .getMimeTypeFromExtension(name.substring(dot + 1).toLowerCase(Locale.ROOT));
        Intent intent = new Intent(Intent.ACTION_CREATE_DOCUMENT).setType(type == null ? "application/octet-stream" : type);
        intent.addCategory(Intent.CATEGORY_OPENABLE).putExtra(Intent.EXTRA_TITLE, name);
        choosingDestination = true;
        try {
            startActivityForResult(intent, SAVE);
        } catch (RuntimeException unavailable) {
            pendingDownload.staged.delete();
            pendingDownload = null;
            choosingDestination = false;
            finishProgress(fileTransfer, "此手机上无法打开系统文件选择器。", true);
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
            PendingDownload downloaded = pendingDownload;
            pendingDownload = null;
            choosingDestination = false;
            if (downloaded == null) return;
            if (result == RESULT_OK && data != null && data.getData() != null) {
                saveDownload(downloaded, data.getData());
            } else {
                downloaded.staged.delete();
                finishProgress(fileTransfer, "已取消保存。", false);
            }
        }
    }

    private void sendFiles(List<Uri> files) {
        TransferApi.Session current = session;
        if (current == null || state == null || fileTransfer != null || leaving) {
            showHint("请连接电脑后重新选择文件。");
            return;
        }
        int room = state.room;
        FileTransfer transfer = startProgress("正在准备 " + files.size() + " 个文件");
        fileWorker.execute(() -> {
            int sent = 0;
            for (int i = 0; i < files.size(); i++) {
                String batch = " (" + (i + 1) + "/" + files.size() + ")";
                try (FileAccess.Source source = FileAccess.from(this, files.get(i), transfer)) {
                    String caption = "发送 " + source.name + batch;
                    updateUI(() -> reportProgress(transfer, caption, 0));
                    TransferApi.send(current, room, source, transfer,
                            (done, total) -> updateUI(() -> reportProgress(transfer, caption, percent(done, total))));
                    sent++;
                } catch (Exception error) {
                    boolean failed = transfer.finish();
                    String prefix = sent == 0 ? "" : "已发送 " + sent + " 个文件。";
                    String detail = failed ? transferMessage(error, "发送中断，请重新选择未发出的文件。") : "传输已取消。";
                    updateUI(() -> finishProgress(transfer, prefix + detail, failed));
                    return;
                }
            }
            transfer.finish();
            updateUI(() -> finishProgress(transfer, "电脑收件箱已收到 " + files.size() + " 个文件。", false));
        });
    }

    private void download(TransferApi.Record record) {
        TransferApi.Session current = session;
        if (current == null || fileTransfer != null || leaving) return;
        String caption = "下载 " + record.name;
        FileTransfer transfer = startProgress(caption);
        fileWorker.execute(() -> {
            File staged = null;
            try {
                transfer.check();
                File cache = getCacheDir();
                if (record.size > cache.getUsableSpace()) {
                    boolean failed = transfer.finish();
                    updateUI(() -> finishProgress(transfer,
                            failed ? "手机可用空间不足，无法暂存文件。" : "传输已取消。", failed));
                    return;
                }
                staged = File.createTempFile("near-in-", ".part", cache);
                try (OutputStream out = new FileOutputStream(staged)) {
                    TransferApi.receive(current, record, out, transfer,
                            (done, total) -> updateUI(() -> reportProgress(transfer, caption, percent(done, total))));
                }
                transfer.finish();
                transfer.check();
                File downloaded = staged;
                boolean posted = handler.post(() -> {
                    if (isDestroyed() || fileTransfer != transfer) {
                        downloaded.delete();
                        return;
                    }
                    pendingDownload = new PendingDownload(record.name, downloaded);
                    showSaveProgress("请选择保存位置");
                    if (visible) pickDestination();
                });
                if (!posted) downloaded.delete();
            } catch (Exception error) {
                if (staged != null) staged.delete();
                boolean failed = transfer.finish();
                String result = failed ? transferMessage(error, "下载中断，请检查连接和手机存储空间后重试。") : "传输已取消。";
                updateUI(() -> finishProgress(transfer, result, failed));
            }
        });
    }

    private void saveDownload(PendingDownload downloaded, Uri uri) {
        FileTransfer transfer = fileTransfer;
        showSaveProgress("正在保存 " + downloaded.name);
        fileWorker.execute(() -> {
            try {
                FileAccess.saveDownload(getContentResolver(), uri, downloaded.staged);
                updateUI(() -> finishProgress(transfer, "已保存 " + downloaded.name, false));
            } catch (Exception error) {
                updateUI(() -> finishProgress(transfer, "未能完整保存，请检查所选位置后重试。", true));
            } finally {
                downloaded.staged.delete();
            }
        });
    }

    private void buildProgress(LinearLayout screen, int pageEdge) {
        progressPanel = card();
        progressPanel.setPadding(dp(13), dp(6), dp(7), dp(9));
        LinearLayout header = row();
        progressText = label("", 13, MUTED, false);
        progressText.setSingleLine(true);
        progressText.setEllipsize(TextUtils.TruncateAt.MIDDLE);
        LinearLayout.LayoutParams textParams = new LinearLayout.LayoutParams(0, -2, 1);
        textParams.rightMargin = dp(8);
        header.addView(progressText, textParams);
        cancelButton = button("取消", 0xffa0443d, 0xfffff0ee, true);
        cancelButton.setOnClickListener(view -> {
            if (fileTransfer != null && fileTransfer.cancel()) {
                cancelButton.setEnabled(false);
                progressText.setText("正在取消传输…");
            }
        });
        header.addView(cancelButton, new LinearLayout.LayoutParams(dp(64), dp(48)));
        add(progressPanel, header, 0, 48);
        progress = new ProgressBar(this, null, android.R.attr.progressBarStyleHorizontal);
        progress.setProgressTintList(ColorStateList.valueOf(BRAND));
        progress.setMax(100);
        add(progressPanel, progress, 2, 7);
        progressPanel.setVisibility(View.GONE);
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(-1, -2);
        params.setMargins(pageEdge, dp(5), pageEdge, dp(8));
        screen.addView(progressPanel, params);
    }

    private FileTransfer startProgress(String caption) {
        fileTransfer = new FileTransfer();
        handler.removeCallbacks(hideTransferResult);
        showHint("");
        progressPanel.setVisibility(View.VISIBLE);
        progress.setVisibility(View.VISIBLE);
        cancelButton.setVisibility(View.VISIBLE);
        cancelButton.setEnabled(true);
        progressText.setTextColor(MUTED);
        reportProgress(fileTransfer, caption, 0);
        renderStatus();
        renderFiles();
        return fileTransfer;
    }

    private void reportProgress(FileTransfer transfer, String caption, int percent) {
        if (fileTransfer != transfer || transfer.isCanceled()) return;
        progress.setProgress(percent);
        progressText.setText(caption + " · " + percent + "%");
    }

    private void showSaveProgress(String caption) {
        progressPanel.setVisibility(View.VISIBLE);
        progress.setVisibility(View.GONE);
        cancelButton.setVisibility(View.GONE);
        progressText.setTextColor(MUTED);
        progressText.setText(caption);
    }

    private void finishProgress(FileTransfer transfer, String caption, boolean failed) {
        if (transfer == null || fileTransfer != transfer || session == null) return;
        fileTransfer = null;
        showSaveProgress(caption);
        progressText.setTextColor(failed ? 0xffa0443d : transfer.isCanceled() ? MUTED : GREEN);
        handler.postDelayed(hideTransferResult, 7000);
        renderStatus();
        renderFiles();
        poll();
    }

    private void showConnectionSettings() {
        if (leaving || session == null) return;
        new AlertDialog.Builder(this).setTitle("连接设置")
                .setItems(new String[]{"更新电脑地址", "异地连接", "解除绑定"}, (dialog, item) -> {
                    if (item == 0) updateAddress(false);
                    else if (item == 1) updateAddress(true);
                    else askLeave();
                }).show();
    }

    private void updateAddress(boolean remote) {
        TransferApi.Session current = session;
        if (current == null || fileTransfer != null) {
            showHint("请等待当前文件传输完成。");
            return;
        }
        EditText input = field(remote ? "100.x.x.x:47332" : "192.168.1.20:47331", false);
        String origin = remote ? current.remoteOrigin : current.lanOrigin;
        if (!origin.isEmpty()) input.setText(origin.substring(origin.indexOf("://") + 3));
        input.selectAll();
        LinearLayout content = column();
        content.setPadding(dp(22), dp(8), dp(22), 0);
        String help = remote
                ? "两端连接同一个 Tailscale 私有网络，填写电脑端「异地连接」页的地址。留空可关闭异地连接。"
                : "填写电脑端显示的局域网地址，继续使用当前绑定。";
        add(content, label(help, 13, MUTED, false), 0);
        add(content, input, 8);
        AlertDialog dialog = new AlertDialog.Builder(this).setTitle(remote ? "异地连接" : "更新电脑地址")
                .setView(content).setNegativeButton("取消", null)
                .setPositiveButton(remote ? "保存" : "重连", null).create();
        dialog.setOnShowListener(owner -> dialog.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener(view -> {
            try {
                String entered = input.getText().toString().trim();
                if (remote) {
                    String address = entered.isEmpty() ? "" : TransferApi.remoteOrigin(entered);
                    current.setRemoteOrigin(address);
                    prefs.edit().putString("remote-origin", address).apply();
                } else {
                    current.setLanOrigin(TransferApi.addressOrigin(entered));
                }
                dialog.dismiss();
                online = false;
                showHint("");
                renderStatus();
                renderFiles();
                poll();
            } catch (IllegalArgumentException invalid) {
                input.setError(remote ? "请填写电脑端显示的 Tailscale 异地地址。" : "请填写电脑上显示的局域网地址。");
            }
        }));
        dialog.show();
    }

    private void askLeave() {
        if (leaving) return;
        if (fileTransfer != null) {
            showHint("请等待当前文件传输完成后再更换连接。");
            return;
        }
        new AlertDialog.Builder(this)
                .setTitle("解除绑定")
                .setMessage("解除后，下次使用时需要重新扫码。请先下载要保留的待收文件。")
                .setNegativeButton("取消", null)
                .setPositiveButton("解除", (dialog, button) -> {
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
                            notice = "请在原电脑端也解除设备绑定。";
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
        if (textDialog != null) textDialog.dialog.dismiss();
        session = null;
        state = null;
        seenText = "";
        online = false;
        if (fileTransfer != null) fileTransfer.cancel();
        fileTransfer = null;
        filter = 0;
        hint = notice;
        prefs.edit().remove("origin").remove("remote-origin").remove("device-id").remove("device-key").remove("cookie").apply();
        showScreen();
    }

    private void showHelp() {
        new AlertDialog.Builder(this).setTitle("使用近传")
                .setMessage("在同一局域网内扫码或输入配对码，之后打开两端近传会自动重连。异地使用前，在「连接设置 → 异地连接」中填写电脑的 Tailscale 地址。\n\n"
                        + "手机发来的文件会自动进入电脑收件箱。点“下载”接收电脑发来的文件，下载完成后选择保存位置。"
                        + "收发过程中可点进度条旁的“取消”。\n\n"
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
