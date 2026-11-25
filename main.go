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

func (e endpoint) desktop() bool { return e.subnet == nil }

func main() {
	port := flag.Int("port", 47331, "Preferred HTTP port (0 chooses a free port)")
	noOpen := flag.Bool("no-open", false, "Do not open a browser")
	flag.Parse()

	if err := run(*port, *noOpen); err != nil {
		fmt.Fprintln(os.Stderr, "NearTransfer:", err)
		if !*noOpen {
			fmt.Fprintln(os.Stderr, "Press Enter to close.")
			fmt.Scanln()
		}
		os.Exit(1)
	}
}

func run(port int, noOpen bool) error {
	if port < 0 || port > 65535 {
		return errors.New("port must be 0–65535")
	}
	loopback, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil && port != 0 {
		loopback, err = net.Listen("tcp4", "127.0.0.1:0")
	}
	if err != nil {
		return err
	}
	defer loopback.Close()
	port = loopback.Addr().(*net.TCPAddr).Port

	h, err := newHub(port)
	if err != nil {
		return err
	}
	defer os.RemoveAll(h.tempDir)

	listeners := []net.Listener{loopback}
	for _, e := range lanEndpoints(port) {
		ln, err := net.Listen("tcp4", e.address)
		if err == nil {
			h.endpoints = append(h.endpoints, e)
			listeners = append(listeners, ln)
		}
	}
	servers := make([]*http.Server, 0, len(listeners))
	serveErrors := make(chan error, len(listeners))
	for i, ln := range listeners {
		e := endpoint{address: ln.Addr().String()}
		if i > 0 {
			e = h.endpoints[i-1]
		}
		server := &http.Server{
			Handler:           h.handler(e),
			ReadHeaderTimeout: 8 * time.Second,
			IdleTimeout:       30 * time.Second,
			MaxHeaderBytes:    16 << 10,
		}
		servers = append(servers, server)
		go func() {
			serveErrors <- server.Serve(ln)
		}()
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	pcURL := "http://" + loopback.Addr().String()
	fmt.Println("NearTransfer")
	fmt.Println("Open on this PC:", pcURL)
	fmt.Printf("LAN addresses ready: %d\n", len(h.endpoints))
	fmt.Println("Keep this window open. Press Ctrl+C to end the session.")
	if !noOpen {
		go func() {
			if err := openBrowser(pcURL); err != nil {
				log.Println("Open the address above in your browser:", err)
			}
		}()
	}

	select {
	case <-ctx.Done():
	case <-h.stop:
	case err = <-serveErrors:
	}

	h.beginShutdown()
	for _, server := range servers {
		server.Close()
	}
	h.fileOperations.Wait()
	return err
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
