package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type endpoint struct {
	address string
	name    string
	subnet  *net.IPNet
}

func main() {
	port := flag.Int("port", 47331, "Preferred HTTP port (0 chooses a free port)")
	noOpen := flag.Bool("no-open", false, "Run without the desktop widget")
	dataDir := flag.String("data-dir", "", "Override the application data directory")
	inbox := flag.String("inbox-dir", "", "Override the received files directory")
	flag.Parse()

	if *dataDir == "" {
		*dataDir = filepath.Join(os.Getenv("LOCALAPPDATA"), "NearTransfer")
	}
	if err := run(*port, *noOpen, *dataDir, *inbox); err != nil {
		fmt.Fprintln(os.Stderr, "NearTransfer:", err)
		if !*noOpen {
			widgetError("近传无法启动", err.Error())
		}
		os.Exit(1)
	}
}

func inboxLocation(dataDir, override string) (string, error) {
	if override != "" {
		return override, nil
	}
	config := filepath.Join(dataDir, "inbox.txt")
	raw, err := os.ReadFile(config)
	if err == nil {
		dir := strings.TrimSpace(strings.TrimPrefix(string(raw), "\ufeff"))
		if !filepath.IsAbs(dir) || strings.ContainsRune(dir, 0) {
			return "", fmt.Errorf("收件箱设置需要完整路径，请检查 %s", config)
		}
		return filepath.Clean(dir), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("无法读取收件箱设置：%w", err)
	}
	dir, err := downloadsFolder()
	if err != nil {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, "Downloads")
	}
	return filepath.Join(dir, "近传"), nil
}

func run(port int, noOpen bool, dataDir, inbox string) error {
	if port < 0 || port > 65535 {
		return errors.New("port must be 0–65535")
	}
	if !noOpen {
		release, err := claimWidget()
		if err != nil || release == nil {
			return err
		}
		defer release()
	}
	inbox, err := inboxLocation(dataDir, inbox)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp4", net.JoinHostPort("0.0.0.0", strconv.Itoa(port)))
	if err != nil && port != 0 {
		listener, err = net.Listen("tcp4", "0.0.0.0:0")
	}
	if err != nil {
		return err
	}
	defer listener.Close()
	port = listener.Addr().(*net.TCPAddr).Port

	h, err := newHub(port, dataDir, inbox)
	if err != nil {
		return err
	}
	defer os.RemoveAll(h.tempDir)

	h.endpoints = lanEndpoints(port)
	desktop := h.handler(desktopAccess)
	phone := h.handler(lanAccess)
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			addr, _ := r.Context().Value(http.LocalAddrContextKey).(*net.TCPAddr)
			if addr != nil && addr.IP.IsLoopback() {
				desktop.ServeHTTP(w, r)
			} else {
				phone.ServeHTTP(w, r)
			}
		}),
		ReadHeaderTimeout: 8 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	pcURL := "http://127.0.0.1:" + strconv.Itoa(port)
	serveErrors := make(chan error, 1)
	go func() {
		if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
			log.Println("NearTransfer:", err)
			serveErrors <- err
			h.beginShutdown()
		}
	}()
	go h.watchNetwork(port)
	remoteDone := h.startTailscale()
	stopDiscovery := h.startDiscovery()
	defer stopDiscovery()
	go func() {
		select {
		case <-ctx.Done():
			h.beginShutdown()
		case <-h.stop:
		}
	}()

	fmt.Println("NearTransfer:", pcURL)
	if noOpen {
		<-h.stop
	} else {
		err = showWidget(h)
	}
	h.beginShutdown()
	server.Close()
	<-remoteDone
	h.fileOperations.Wait()
	select {
	case serveErr := <-serveErrors:
		if err == nil {
			err = serveErr
		}
	default:
	}
	return err
}

func (h *hub) watchNetwork(port int) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-h.stop:
			return
		case <-ticker.C:
			endpoints := lanEndpoints(port)
			h.mu.Lock()
			h.endpoints = endpoints
			h.mu.Unlock()
		}
	}
}

var carrierSubnet = &net.IPNet{
	IP: net.IPv4(100, 64, 0, 0).To4(), Mask: net.CIDRMask(10, 32),
}

func lanEndpoints(port int) []endpoint {
	interfaces, _ := net.Interfaces()
	var endpoints []endpoint
	used := map[string]bool{}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || isVirtualAdapter(iface.Name) {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			subnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := subnet.IP.To4()
			bits, width := subnet.Mask.Size()
			if ip == nil || width != 32 || bits == 0 || (!ip.IsPrivate() && !carrierSubnet.Contains(ip)) {
				continue
			}
			address := net.JoinHostPort(ip.String(), strconv.Itoa(port))
			if !used[address] {
				used[address] = true
				endpoints = append(endpoints, endpoint{address, iface.Name, &net.IPNet{IP: ip.Mask(subnet.Mask), Mask: subnet.Mask}})
			}
		}
	}
	sort.SliceStable(endpoints, func(i, j int) bool {
		return isWiFi(endpoints[i].name) && !isWiFi(endpoints[j].name)
	})
	return endpoints
}

func isWiFi(name string) bool {
	name = strings.ToLower(name)
	return strings.Contains(name, "wi-fi") || strings.Contains(name, "wifi") ||
		strings.Contains(name, "wlan") || strings.Contains(name, "无线")
}

func isVirtualAdapter(name string) bool {
	name = strings.ToLower(name)
	for _, marker := range []string{"vethernet", "virtualbox", "vmware", "mihomo", "clash", "wintun", "tailscale"} {
		if strings.Contains(name, marker) {
			return true
		}
	}
	return false
}
