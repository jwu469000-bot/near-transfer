package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const discoveryPort = 47330

type binding struct {
	ID  string `json:"id"`
	Key string `json:"key"`
}

func loadBinding(dir string) (binding, error) {
	var stored binding
	ciphertext, err := os.ReadFile(filepath.Join(dir, "binding.dat"))
	if errors.Is(err, os.ErrNotExist) {
		return stored, nil
	}
	if err != nil {
		return stored, err
	}
	data, err := protectData(ciphertext, false)
	if err != nil {
		return stored, err
	}
	if err := json.Unmarshal(data, &stored); err != nil || !validHex(stored.ID, 48) || !validHex(stored.Key, 48) {
		return binding{}, errors.New("无法读取已保存的设备绑定")
	}
	return stored, nil
}

func saveBinding(dir string, next binding) error {
	filename := filepath.Join(dir, "binding.dat")
	if next.ID == "" {
		err := os.Remove(filename)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	data, err := protectData(raw, true)
	if err != nil {
		return err
	}
	return writeAtomic(filename, data)
}

func writeAtomic(filename string, data []byte) error {
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, "near-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, err = file.Write(data)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(file.Name(), filename)
}

func validHex(value string, length int) bool {
	if len(value) != length || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func signature(key, purpose string, values ...string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte("near-transfer/1\n" + purpose))
	for _, value := range values {
		mac.Write([]byte("\n" + value))
	}
	return hex.EncodeToString(mac.Sum(nil))
}

func (h *hub) identity(w http.ResponseWriter, r *http.Request) {
	nonce := r.URL.Query().Get("nonce")
	id := r.URL.Query().Get("id")
	if !validHex(nonce, 32) || !validHex(id, 48) {
		http.NotFound(w, r)
		return
	}
	h.mu.Lock()
	device, epoch := h.bound, h.epoch
	h.mu.Unlock()
	if device.ID == "" || device.ID != id {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"epoch": epoch,
		"proof": signature(device.Key, "host", "http://"+r.Host, nonce, epoch),
	})
}

func (h *hub) startDiscovery() func() {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: discoveryPort})
	if err != nil {
		return func() {}
	}
	go func() {
		buf := make([]byte, 256)
		for {
			n, remote, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if reply := h.discoveryReply(remote.IP, string(buf[:n])); reply != "" {
				conn.WriteToUDP([]byte(reply), remote)
			}
		}
	}()
	return func() { conn.Close() }
}

func (h *hub) discoveryReply(remote net.IP, query string) string {
	fields := strings.Split(query, " ")
	if len(fields) != 4 || fields[0] != "NT1" || !validHex(fields[1], 48) || !validHex(fields[2], 32) ||
		!validHex(fields[3], 64) {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopping || h.bound.ID != fields[1] {
		return ""
	}
	if !hmac.Equal([]byte(fields[3]), []byte(signature(h.bound.Key, "find", fields[1], fields[2]))) {
		return ""
	}
	for _, e := range h.endpoints {
		if e.subnet.Contains(remote) {
			proof := signature(h.bound.Key, "found", e.address, fields[2], h.epoch)
			return fmt.Sprintf("NT1 %s %s %s", e.address, h.epoch, proof)
		}
	}
	return ""
}
