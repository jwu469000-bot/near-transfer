package cn.jinchuan.transfer;

import java.io.IOException;
import java.net.DatagramPacket;
import java.net.DatagramSocket;
import java.net.InetAddress;
import java.net.InterfaceAddress;
import java.net.NetworkInterface;
import java.net.SocketException;
import java.net.SocketTimeoutException;
import java.net.URI;
import java.nio.charset.StandardCharsets;
import java.security.GeneralSecurityException;
import java.security.MessageDigest;
import java.security.SecureRandom;
import java.util.Enumeration;
import java.util.LinkedHashSet;
import java.util.Set;
import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;

final class DeviceDiscovery {
    private static final int PORT = 47330;
    private static final SecureRandom RANDOM = new SecureRandom();
    private static final char[] HEX = "0123456789abcdef".toCharArray();

    static TransferApi.Route route(TransferApi.Session session, String origin, String epoch) throws IOException {
        if (!hex(epoch, 48)) throw new IOException("电脑返回的会话信息有误。");
        return new TransferApi.Route(origin, signature(session.key, "ticket", origin, epoch));
    }

    static TransferApi.Route find(TransferApi.Session session, String cachedOrigin) throws IOException {
        String nonce = randomHex(16);
        String query = "NT1 " + session.id + " " + nonce + " " + signature(session.key, "find", session.id, nonce);
        byte[] payload = query.getBytes(StandardCharsets.US_ASCII);
        try (DatagramSocket socket = new DatagramSocket()) {
            socket.setBroadcast(true);
            for (InetAddress destination : targets(cachedOrigin)) {
                try {
                    socket.send(new DatagramPacket(payload, payload.length, destination, PORT));
                } catch (IOException ignored) {
                    // Interfaces can disappear while Android changes Wi-Fi.
                }
            }
            byte[] buffer = new byte[256];
            long deadline = System.nanoTime() + 1_450_000_000L;
            while (!Thread.currentThread().isInterrupted()) {
                long remaining = (deadline - System.nanoTime()) / 1_000_000L;
                if (remaining <= 0) break;
                socket.setSoTimeout((int) remaining);
                DatagramPacket packet = new DatagramPacket(buffer, buffer.length);
                try {
                    socket.receive(packet);
                } catch (SocketTimeoutException expired) {
                    break;
                }
                String reply = new String(packet.getData(), 0, packet.getLength(), StandardCharsets.US_ASCII);
                String[] fields = reply.split(" ");
                if (fields.length != 4 || !"NT1".equals(fields[0]) || !hex(fields[2], 48)
                        || !check(session.key, fields[3], "found", fields[1], nonce, fields[2])) continue;
                try {
                    return route(session, TransferApi.addressOrigin(fields[1]), fields[2]);
                } catch (IllegalArgumentException invalid) {
                    continue;
                }
            }
        }
        throw new IOException("暂时找不到已绑定的电脑。");
    }

    private static Set<InetAddress> targets(String cachedOrigin) throws IOException {
        Set<InetAddress> result = new LinkedHashSet<>();
        result.add(InetAddress.getByName(URI.create(cachedOrigin).getHost()));
        result.add(InetAddress.getByName("255.255.255.255"));
        try {
            Enumeration<NetworkInterface> interfaces = NetworkInterface.getNetworkInterfaces();
            while (interfaces != null && interfaces.hasMoreElements()) {
                NetworkInterface iface = interfaces.nextElement();
                if (!iface.isUp() || iface.isLoopback()) continue;
                for (InterfaceAddress address : iface.getInterfaceAddresses()) {
                    if (address.getBroadcast() != null) result.add(address.getBroadcast());
                }
            }
        } catch (SocketException unavailable) {
            // The remembered address and default broadcast remain usable.
        }
        return result;
    }

    static boolean check(String key, String proof, String purpose, String... values) {
        if (!hex(proof, 64)) return false;
        return MessageDigest.isEqual(proof.getBytes(StandardCharsets.US_ASCII),
                signature(key, purpose, values).getBytes(StandardCharsets.US_ASCII));
    }

    private static String signature(String key, String purpose, String... values) {
        StringBuilder message = new StringBuilder("near-transfer/1\n").append(purpose);
        for (String value : values) message.append('\n').append(value);
        try {
            Mac mac = Mac.getInstance("HmacSHA256");
            mac.init(new SecretKeySpec(key.getBytes(StandardCharsets.US_ASCII), "HmacSHA256"));
            return encoded(mac.doFinal(message.toString().getBytes(StandardCharsets.US_ASCII)));
        } catch (GeneralSecurityException impossible) {
            throw new IllegalStateException(impossible);
        }
    }

    static boolean hex(String value, int length) {
        return value != null && value.length() == length && value.matches("[a-f0-9]+");
    }

    static String randomHex(int length) {
        byte[] buffer = new byte[length];
        RANDOM.nextBytes(buffer);
        return encoded(buffer);
    }

    static String encoded(byte[] bytes) {
        char[] value = new char[bytes.length * 2];
        for (int i = 0; i < bytes.length; i++) {
            int part = bytes[i] & 255;
            value[i * 2] = HEX[part >>> 4];
            value[i * 2 + 1] = HEX[part & 15];
        }
        return new String(value);
    }
}
