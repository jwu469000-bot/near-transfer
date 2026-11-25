package cn.jinchuan.transfer;

import java.io.IOException;
import java.security.GeneralSecurityException;
import java.security.MessageDigest;
import java.security.cert.CertificateException;
import java.security.cert.X509Certificate;

import javax.net.ssl.SSLContext;
import javax.net.ssl.SSLSocketFactory;
import javax.net.ssl.TrustManager;
import javax.net.ssl.X509TrustManager;
import javax.security.auth.x500.X500Principal;

final class RemoteTls {
    private RemoteTls() {}

    static SSLSocketFactory forBinding(String key) throws IOException {
        try {
            SSLContext context = SSLContext.getInstance("TLS");
            context.init(null, new TrustManager[]{new X509TrustManager() {
                @Override
                public void checkServerTrusted(X509Certificate[] chain, String authType) throws CertificateException {
                    if (chain == null || chain.length != 1) throw new CertificateException("电脑证书不完整。");
                    X509Certificate certificate = chain[0];
                    certificate.checkValidity();
                    String subject = certificate.getSubjectX500Principal().getName(X500Principal.RFC2253);
                    try {
                        byte[] digest = MessageDigest.getInstance("SHA-256").digest(certificate.getPublicKey().getEncoded());
                        if (subject.startsWith("CN=") && DeviceDiscovery.check(
                                key, subject.substring(3), "remote-certificate", DeviceDiscovery.encoded(digest))) return;
                    } catch (GeneralSecurityException error) {
                        throw new CertificateException("无法验证电脑证书。", error);
                    }
                    throw new CertificateException("异地地址与原配对不匹配。");
                }

                @Override
                public void checkClientTrusted(X509Certificate[] chain, String authType) throws CertificateException {
                    throw new CertificateException("无法验证客户端证书。");
                }

                @Override
                public X509Certificate[] getAcceptedIssuers() {
                    return new X509Certificate[0];
                }
            }}, null);
            return context.getSocketFactory();
        } catch (GeneralSecurityException unavailable) {
            throw new IOException("无法建立异地加密连接。", unavailable);
        }
    }
}
