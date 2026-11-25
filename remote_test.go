package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteConnectionRequiresNativeBinding(t *testing.T) {
	h := newTransferTestHub(t)
	device := binding{ID: strings.Repeat("a", 48), Key: strings.Repeat("b", 48)}
	h.bound = device
	ip := net.ParseIP("100.100.12.34")
	host := net.JoinHostPort(ip.String(), "47332")
	origin := "https://" + host
	certificate, err := remoteCertificate(ip, device)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(leaf.RawSubjectPublicKeyInfo)
	if leaf.Subject.CommonName != signature(device.Key, "remote-certificate", hex.EncodeToString(digest[:])) {
		t.Fatal("certificate is not authenticated by the device binding")
	}
	if err := leaf.VerifyHostname(ip.String()); err != nil {
		t.Fatal(err)
	}
	if leaf.VerifyHostname("100.100.12.35") == nil {
		t.Fatal("certificate was valid for a different node address")
	}
	roots := x509.NewCertPool()
	roots.AddCert(leaf)

	remote := h.handler(remoteAccess)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The TCP connection stays on loopback. Supply the two addresses a Tailscale adapter would report.
		r.RemoteAddr = "100.101.20.30:50102"
		if r.Header.Get("X-Test-Non-Tailscale") == "1" {
			r.RemoteAddr = "192.168.1.10:50102"
		}
		r = r.WithContext(context.WithValue(r.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: ip, Port: 47332}))
		remote.ServeHTTP(w, r)
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}}
	server.StartTLS()
	t.Cleanup(server.Close)
	client := server.Client()
	transport := client.Transport.(*http.Transport)
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	dialer := new(net.Dialer)
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, server.Listener.Addr().String())
	}

	call := func(method, path string, headers map[string]string, data io.Reader, status int) []byte {
		t.Helper()
		req, err := http.NewRequest(method, origin+path, data)
		if err != nil {
			t.Fatal(err)
		}
		if method == http.MethodPost {
			req.Header.Set("Origin", origin)
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil || response.StatusCode != status {
			t.Fatalf("%s %s: status %d, body %q, err %v", method, path, response.StatusCode, body, err)
		}
		return body
	}

	nonce := strings.Repeat("c", 32)
	body := call(http.MethodGet, "/api/identity?id="+device.ID+"&nonce="+nonce, nil, nil, http.StatusOK)
	var identity map[string]string
	if err := json.Unmarshal(body, &identity); err != nil ||
		identity["proof"] != signature(device.Key, "host", origin, nonce, h.epoch) {
		t.Fatalf("HTTPS identity proof: %s, %v", body, err)
	}
	call(http.MethodGet, "/api/state", nil, nil, http.StatusUnauthorized)
	call(http.MethodGet, "/api/state", map[string]string{
		"Authorization": "Bearer " + signature(device.Key, "ticket", "http://"+host, h.epoch),
	}, nil, http.StatusUnauthorized)
	call(http.MethodGet, "/api/state", map[string]string{"X-Test-Non-Tailscale": "1"}, nil, http.StatusForbidden)
	call(http.MethodPost, "/api/pair", nil, nil, http.StatusNotFound)
	call(http.MethodGet, "/android", nil, nil, http.StatusNotFound)

	headers := map[string]string{"Authorization": "Bearer " + signature(device.Key, "ticket", origin, h.epoch)}
	call(http.MethodGet, "/api/state", headers, nil, http.StatusOK)
	headers["X-Room"] = "1"
	headers["X-Transfer-Id"] = strings.Repeat("d", 32)
	headers["X-File-Name"] = "remote.txt"
	call(http.MethodPost, "/api/upload", headers, strings.NewReader("HTTPS transfer"), http.StatusCreated)
	if data, err := os.ReadFile(filepath.Join(h.inbox, "remote.txt")); err != nil || string(data) != "HTTPS transfer" {
		t.Fatalf("remote inbox result %q, err %v", data, err)
	}
}
