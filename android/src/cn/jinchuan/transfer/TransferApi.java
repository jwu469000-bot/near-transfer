package cn.jinchuan.transfer;

import org.json.JSONArray;
import org.json.JSONException;
import org.json.JSONObject;

import android.net.Uri;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.Proxy;
import java.net.URI;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;
import java.util.UUID;

final class TransferApi {
    static final int TEXT_LIMIT = 256 * 1024;
    interface Progress {
        void update(long done, long total);
    }

    static final class Target {
        final String origin;
        final String code;

        Target(String origin, String code) {
            this.origin = origin;
            this.code = code;
        }
    }

    static final class Session {
        final String id;
        final String key;
        volatile Route route;

        Session(String origin, String id, String key) {
            this.id = id;
            this.key = key;
            route = new Route(origin, null);
        }
    }

    static final class Route {
        final String origin;
        final String ticket;

        Route(String origin, String ticket) {
            this.origin = origin;
            this.ticket = ticket;
        }
    }

    static final class Record {
        final String id;
        final String name;
        final long size;
        final boolean incoming;

        Record(JSONObject entry) throws JSONException {
            this(entry.getString("id"), entry.getString("name"), entry.getLong("size"),
                    "desktop".equals(entry.getString("sender")));
        }

        Record(String id, String name, long size, boolean incoming) {
            this.id = id;
            this.name = name;
            this.size = size;
            this.incoming = incoming;
        }
    }

    static final class State {
        final int room;
        final List<Record> files;
        final List<TextRecord> texts;

        State(JSONObject object) throws JSONException {
            room = object.getInt("room");
            JSONArray records = object.getJSONArray("files");
            files = new ArrayList<>(records.length());
            for (int i = records.length() - 1; i >= 0; i--) {
                files.add(new Record(records.getJSONObject(i)));
            }
            texts = new ArrayList<>();
            JSONArray messages = object.optJSONArray("texts");
            if (messages != null) {
                for (int i = 0; i < messages.length(); i++) texts.add(new TextRecord(messages.getJSONObject(i)));
            }
        }
    }

    static final class TextRecord {
        final String id;
        final String preview;
        final boolean incoming;

        TextRecord(JSONObject item) throws JSONException {
            id = item.getString("id");
            preview = item.getString("preview");
            incoming = "desktop".equals(item.getString("sender"));
        }
    }

    static final class HttpError extends IOException {
        final int status;

        HttpError(int status, String message) {
            super(message);
            this.status = status;
        }
    }

    static Target scan(String link) {
        URI address = URI.create(link.trim());
        String fragment = address.getRawFragment();
        if (fragment == null || !fragment.matches("join=[a-f0-9]{48}")) {
            throw new IllegalArgumentException("请扫描电脑端近传显示的二维码。");
        }
        return new Target(localOrigin(address), fragment.substring(5));
    }

    static Target manual(String address, String code) {
        address = address.trim();
        if (address.contains("#")) return scan(address);
        String digits = code.replaceAll("\\s+", "");
        if (!digits.matches("[0-9]{6}")) {
            throw new IllegalArgumentException("请输入电脑上显示的六位配对码。");
        }
        try {
            return new Target(addressOrigin(address), digits);
        } catch (IllegalArgumentException badAddress) {
            throw new IllegalArgumentException("请输入电脑上显示的局域网地址，例如 192.168.1.20:47331。");
        }
    }

    static String addressOrigin(String address) {
        String entered = address.trim();
        URI uri = URI.create(entered.startsWith("http://") ? entered : "http://" + entered);
        return localOrigin(uri);
    }

    static Session restore(String origin, String id, String key) {
        if (origin == null || !DeviceDiscovery.hex(id, 48) || !DeviceDiscovery.hex(key, 48)) return null;
        try {
            if (origin.equals(localOrigin(URI.create(origin)))) return new Session(origin, id, key);
        } catch (IllegalArgumentException ignored) {
            // A saved address can be invalid after an upgrade.
        }
        return null;
    }

    private static String localOrigin(URI uri) {
        String host = uri.getHost();
        int port = uri.getPort();
        String path = uri.getRawPath();
        if (!"http".equals(uri.getScheme()) || !isLanIPv4(host) || uri.getUserInfo() != null
                || uri.getRawQuery() != null || (path != null && !path.isEmpty() && !"/".equals(path))
                || port == 0 || port > 65535) {
            throw new IllegalArgumentException("请使用电脑端近传显示的局域网地址。");
        }
        return "http://" + host + ":" + (port == -1 ? 47331 : port);
    }

    private static boolean isLanIPv4(String host) {
        if (host == null || !host.matches("[0-9]{1,3}(\\.[0-9]{1,3}){3}")) return false;
        String[] parts = host.split("\\.");
        int[] octets = new int[4];
        for (int i = 0; i < octets.length; i++) {
            octets[i] = Integer.parseInt(parts[i]);
            if (octets[i] > 255 || !String.valueOf(octets[i]).equals(parts[i])) return false;
        }
        int a = octets[0], b = octets[1];
        return a == 10 || (a == 192 && b == 168) || (a == 172 && b >= 16 && b <= 31)
                || (a == 100 && b >= 64 && b <= 127);
    }

    static Session pair(Target target) throws IOException, JSONException {
        Session session = new Session(target.origin, DeviceDiscovery.randomHex(24), DeviceDiscovery.randomHex(24));
        HttpURLConnection request = open(target.origin, "/api/pair", "POST", null);
        try {
            byte[] body = new JSONObject().put("code", target.code).put("mode", "android")
                    .put("id", session.id).put("key", session.key).toString().getBytes(StandardCharsets.UTF_8);
            request.setRequestProperty("Content-Type", "application/json");
            request.setFixedLengthStreamingMode(body.length);
            request.setDoOutput(true);
            try (OutputStream out = request.getOutputStream()) {
                out.write(body);
            }
            requireSuccess(request);
            try (InputStream in = request.getInputStream()) {
                String epoch = new JSONObject(readText(in, 4096)).getString("epoch");
                session.route = DeviceDiscovery.route(session, target.origin, epoch);
            }
            return session;
        } catch (HttpError rejected) {
            throw rejected;
        } catch (IOException | JSONException interrupted) {
            try {
                session.route = probe(session, target.origin);
                return session;
            } catch (IOException | JSONException unavailable) {
                throw interrupted;
            }
        } finally {
            request.disconnect();
        }
    }

    static State state(Session session) throws IOException, JSONException {
        Route cached = session.route;
        if (cached.ticket != null) {
            try {
                return readState(cached);
            } catch (IOException | JSONException unavailable) {
                // A desktop restart rotates its ticket. Probe the remembered address first.
            }
        }
        Route found = findRoute(session, cached);
        State state = readState(found);
        if (session.route == cached) session.route = found;
        return state;
    }

    private static State readState(Route route) throws IOException, JSONException {
        HttpURLConnection request = open(route.origin, "/api/state", "GET", route.ticket);
        request.setConnectTimeout(1500);
        request.setReadTimeout(2500);
        try {
            requireSuccess(request);
            try (InputStream input = request.getInputStream()) {
                return new State(new JSONObject(readText(input, 16 * 1024 * 1024)));
            }
        } finally {
            request.disconnect();
        }
    }

    private static Route findRoute(Session session, Route cached) throws IOException, JSONException {
        try {
            return probe(session, cached.origin);
        } catch (IOException | JSONException unavailable) {
            return DeviceDiscovery.find(session, cached.origin);
        }
    }

    private static Route probe(Session session, String origin) throws IOException, JSONException {
        String nonce = DeviceDiscovery.randomHex(16);
        HttpURLConnection request = open(origin, "/api/identity?id=" + session.id + "&nonce=" + nonce, "GET", null);
        request.setConnectTimeout(950);
        request.setReadTimeout(950);
        try {
            requireSuccess(request);
            try (InputStream in = request.getInputStream()) {
                JSONObject response = new JSONObject(readText(in, 4096));
                String epoch = response.getString("epoch");
                if (!DeviceDiscovery.check(session.key, response.getString("proof"), "host", origin, nonce, epoch)) {
                    throw new IOException("电脑身份验证失败。");
                }
                return DeviceDiscovery.route(session, origin, epoch);
            }
        } finally {
            request.disconnect();
        }
    }

    static void leave(Session session) throws IOException, JSONException {
        Route route = session.route;
        if (route.ticket == null) route = findRoute(session, route);
        try {
            postLeave(route);
        } catch (HttpError error) {
            if (error.status != 401) throw error;
            postLeave(findRoute(session, route));
        }
    }

    private static void postLeave(Route route) throws IOException {
        HttpURLConnection request = open(route.origin, "/api/leave", "POST", route.ticket);
        try {
            request.setConnectTimeout(1800);
            request.setReadTimeout(2500);
            requireSuccess(request);
        } finally {
            request.disconnect();
        }
    }

    static void send(Session session, int room, FileAccess.Source source, Progress progress) throws IOException {
        Route route = session.route;
        HttpURLConnection request = open(route.origin, "/api/upload", "POST", route.ticket);
        try {
            request.setDoOutput(true);
            request.setFixedLengthStreamingMode(source.size);
            request.setRequestProperty("Content-Type", "application/octet-stream");
            request.setRequestProperty("X-File-Name", Uri.encode(source.name));
            request.setRequestProperty("X-Room", String.valueOf(room));
            request.setRequestProperty("X-Transfer-Id", UUID.randomUUID().toString().replace("-", ""));
            try (InputStream in = source.open(); OutputStream out = request.getOutputStream()) {
                copy(in, out, source.size, progress);
            }
            requireSuccess(request);
        } finally {
            request.disconnect();
        }
    }

    static void receive(Session session, Record record, FileAccess.Destination destination, Progress progress) throws IOException {
        if (!record.id.matches("[dp][a-f0-9]{32}")) throw new IOException("文件记录无效。");
        Route route = session.route;
        HttpURLConnection request = open(route.origin, "/api/files/" + record.id, "GET", route.ticket);
        try {
            requireSuccess(request);
            try (InputStream in = request.getInputStream(); OutputStream out = destination.open()) {
                copy(in, out, record.size, progress);
            }
        } finally {
            request.disconnect();
        }
    }

    static void sendText(Session session, int room, String id, String value) throws IOException {
        byte[] body = value.getBytes(StandardCharsets.UTF_8);
        if (body.length > TEXT_LIMIT) throw new HttpError(413, "单条文字最多 256 KB。");
        Route route = session.route;
        HttpURLConnection request = open(route.origin, "/api/text", "POST", route.ticket);
        try {
            request.setConnectTimeout(2200);
            request.setReadTimeout(5500);
            request.setRequestProperty("Content-Type", "text/plain; charset=utf-8");
            request.setRequestProperty("X-Transfer-Id", id);
            request.setRequestProperty("X-Room", String.valueOf(room));
            request.setDoOutput(true);
            request.setFixedLengthStreamingMode(body.length);
            try (OutputStream output = request.getOutputStream()) {
                output.write(body);
            }
            requireSuccess(request);
        } finally {
            request.disconnect();
        }
    }

    static String getText(Session session, TextRecord record) throws IOException {
        if (!record.id.matches("[dp][a-f0-9]{32}")) throw new IOException("文字记录无效。");
        Route route = session.route;
        HttpURLConnection request = open(route.origin, "/api/text/" + record.id, "GET", route.ticket);
        try {
            request.setConnectTimeout(2200);
            request.setReadTimeout(5500);
            requireSuccess(request);
            try (InputStream input = request.getInputStream()) {
                return readText(input, TEXT_LIMIT);
            }
        } finally {
            request.disconnect();
        }
    }

    private static HttpURLConnection open(String origin, String path, String method, String ticket) throws IOException {
        HttpURLConnection request = (HttpURLConnection) new URL(origin + path).openConnection(Proxy.NO_PROXY);
        request.setConnectTimeout(5500);
        request.setReadTimeout(20000);
        request.setInstanceFollowRedirects(false);
        request.setUseCaches(false);
        request.setRequestMethod(method);
        request.setRequestProperty("Accept-Encoding", "identity");
        if (ticket != null) request.setRequestProperty("Authorization", "Bearer " + ticket);
        if ("POST".equals(method)) request.setRequestProperty("Origin", origin);
        return request;
    }

    private static void requireSuccess(HttpURLConnection request) throws IOException {
        int status = request.getResponseCode();
        if (status >= 200 && status < 300) return;
        String message = "电脑返回错误，请重新操作。";
        try (InputStream error = request.getErrorStream()) {
            if (error != null) message = new JSONObject(readText(error, 4096)).optString("error", message);
        } catch (IOException | JSONException ignored) {
            // Preserve the status code when the response body is unavailable.
        }
        throw new HttpError(status, message);
    }

    private static String readText(InputStream in, int limit) throws IOException {
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        byte[] buffer = new byte[4096];
        int count;
        while ((count = in.read(buffer)) != -1) {
            if (out.size() > limit - count) throw new IOException("响应超过可读取大小。");
            out.write(buffer, 0, count);
        }
        return out.toString("UTF-8");
    }

    private static void copy(InputStream in, OutputStream out, long total, Progress progress) throws IOException {
        byte[] buffer = new byte[128 * 1024];
        long done = 0;
        long lastUpdate = 0;
        int count;
        while ((count = in.read(buffer)) != -1) {
            if (done > total - count) throw new IOException("文件大小在传输时发生了变化。");
            out.write(buffer, 0, count);
            done += count;
            long now = System.nanoTime();
            if (done == total || now - lastUpdate > 120_000_000L) {
                progress.update(done, total);
                lastUpdate = now;
            }
        }
        if (done != total) throw new IOException("文件传输不完整。");
    }

}
