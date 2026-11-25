package cn.jinchuan.transfer;

import android.content.ContentResolver;
import android.content.Context;
import android.content.res.AssetFileDescriptor;
import android.database.Cursor;
import android.net.Uri;
import android.os.CancellationSignal;
import android.provider.OpenableColumns;

import java.io.File;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;

final class FileAccess {
    static final class Source implements AutoCloseable {
        final String name;
        final long size;
        private final ContentResolver resolver;
        private final Uri uri;
        private final File staged;

        Source(ContentResolver resolver, Uri uri, String name, long size, File staged) {
            this.resolver = resolver;
            this.uri = uri;
            this.name = name;
            this.size = size;
            this.staged = staged;
        }

        InputStream open(CancellationSignal signal) throws IOException {
            if (staged != null) return new FileInputStream(staged);
            return openInput(resolver, uri, signal);
        }

        @Override
        public void close() {
            if (staged != null) staged.delete();
        }
    }

    static Source from(Context context, Uri uri, FileTransfer transfer) throws IOException {
        transfer.check();
        ContentResolver resolver = context.getContentResolver();
        String name = "文件";
        long size = -1;
        try (Cursor cursor = resolver.query(uri, new String[]{OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE},
                null, null, null, transfer.storage)) {
            if (cursor != null && cursor.moveToFirst()) {
                int nameColumn = cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME);
                int sizeColumn = cursor.getColumnIndex(OpenableColumns.SIZE);
                if (nameColumn >= 0 && !cursor.isNull(nameColumn)) name = cursor.getString(nameColumn);
                if (sizeColumn >= 0 && !cursor.isNull(sizeColumn)) size = cursor.getLong(sizeColumn);
            }
        }
        if (size < 0) {
            try (AssetFileDescriptor descriptor = resolver.openAssetFileDescriptor(uri, "r", transfer.storage)) {
                if (descriptor != null) size = descriptor.getLength();
            }
        }
        transfer.check();
        if (size >= 0) return new Source(resolver, uri, name, size, null);

        File staged = File.createTempFile("near-out-", ".part", context.getCacheDir());
        boolean keep = false;
        try {
            try (InputStream in = openInput(resolver, uri, transfer.storage); OutputStream out = new FileOutputStream(staged)) {
                byte[] buffer = new byte[128 * 1024];
                while (true) {
                    transfer.check();
                    int length = in.read(buffer);
                    if (length == -1) break;
                    transfer.check();
                    out.write(buffer, 0, length);
                }
            }
            transfer.check();
            keep = true;
            return new Source(resolver, uri, name, staged.length(), staged);
        } finally {
            if (!keep) staged.delete();
        }
    }

    static void saveDownload(ContentResolver resolver, Uri uri, File staged) throws IOException {
        try (InputStream in = new FileInputStream(staged); OutputStream out = resolver.openOutputStream(uri, "wt")) {
            if (out == null) throw new IOException("无法写入选定的位置。");
            byte[] buffer = new byte[128 * 1024];
            int length;
            while ((length = in.read(buffer)) != -1) out.write(buffer, 0, length);
        }
    }

    private static InputStream openInput(ContentResolver resolver, Uri uri, CancellationSignal signal) throws IOException {
        AssetFileDescriptor descriptor = resolver.openAssetFileDescriptor(uri, "r", signal);
        if (descriptor == null) throw new IOException("无法读取选中的文件。");
        try {
            return descriptor.createInputStream();
        } catch (IOException | RuntimeException error) {
            descriptor.close();
            throw error;
        }
    }
}
