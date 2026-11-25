package cn.jinchuan.transfer;

import android.os.CancellationSignal;

import java.io.InterruptedIOException;
import java.net.HttpURLConnection;

final class FileTransfer {
    final CancellationSignal storage = new CancellationSignal();
    private volatile boolean canceled;
    private boolean finished;
    private HttpURLConnection connection;

    synchronized void attach(HttpURLConnection request) throws InterruptedIOException {
        check();
        connection = request;
    }

    synchronized void detach() {
        connection = null;
    }

    synchronized boolean cancel() {
        if (canceled || finished) return false;
        canceled = true;
        HttpURLConnection active = connection;
        new Thread(() -> {
            try {
                if (active != null) active.disconnect();
            } finally {
                storage.cancel();
            }
        }, "near-cancel-transfer").start();
        return true;
    }

    boolean isCanceled() {
        return canceled;
    }

    void check() throws InterruptedIOException {
        if (canceled) throw new InterruptedIOException("传输已取消。");
    }

    synchronized boolean finish() {
        finished = true;
        return !canceled;
    }
}
