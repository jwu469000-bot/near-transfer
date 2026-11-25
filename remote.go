package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const remotePort = 47332

type remoteConfig struct {
	ip     string
	device binding
}

func (h *hub) startTailscale() <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		var server *http.Server
		var serving <-chan struct{}
		var config remoteConfig
		defer func() {
			if server != nil {
				server.Close()
			}
		}()
		for {
			h.mu.Lock()
			next := remoteConfig{device: h.bound}
			h.mu.Unlock()
			if next.device.ID != "" {
				next.ip = tailscaleIP()
			}
			if next != config || (server == nil && next.ip != "") {
				if server != nil {
					server.Close()
					server, serving = nil, nil
				}
				config = next
				address, failed := "", false
				if next.ip != "" {
					endpoint := net.JoinHostPort(next.ip, strconv.Itoa(remotePort))
					var err error
					server, serving, err = h.serveRemote(endpoint, next.device)
					failed = err != nil
					if !failed {
						address = endpoint
					}
				}
				h.setRemoteStatus(address, failed)
			}
			select {
			case <-h.stop:
				return
			case <-ticker.C:
			case <-serving:
				server.Close()
				server, serving = nil, nil
				h.setRemoteStatus("", true)
			}
		}
	}()
	return done
}

func (h *hub) setRemoteStatus(address string, failed bool) {
	h.mu.Lock()
	h.remoteAddress, h.remoteFailed = address, failed
	h.mu.Unlock()
}

func tailscaleIP() string {
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || !strings.EqualFold(iface.Name, "Tailscale") {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if subnet, ok := addr.(*net.IPNet); ok && carrierSubnet.Contains(subnet.IP) {
				return subnet.IP.String()
			}
		}
	}
	return ""
}

func (h *hub) serveRemote(address string, device binding) (*http.Server, <-chan struct{}, error) {
	host, _, _ := net.SplitHostPort(address)
	cert, err := remoteCertificate(net.ParseIP(host), device)
	if err != nil {
		return nil, nil, err
	}
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		return nil, nil, err
	}
	config := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	server := &http.Server{
		Handler: h.handler(remoteAccess), ReadHeaderTimeout: 8 * time.Second,
		IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10,
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.Serve(tls.NewListener(listener, config))
	}()
	return server, done, nil
}

func remoteCertificate(ip net.IP, device binding) (tls.Certificate, error) {
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	public, err := x509.MarshalPKIXPublicKey(&private.PublicKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	digest := sha256.Sum256(public)
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial.Add(serial, big.NewInt(1)),
		// Android checks the binding's signature over the TLS public key.
		Subject:     pkix.Name{CommonName: signature(device.Key, "remote-certificate", hex.EncodeToString(digest[:]))},
		NotBefore:   now.Add(-24 * time.Hour),
		NotAfter:    now.AddDate(2, 0, 0),
		IPAddresses: []net.IP{ip},
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &private.PublicKey, private)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: private}, nil
}
