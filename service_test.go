package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTransferTestHub(t *testing.T) *hub {
	t.Helper()
	dir := t.TempDir()
	h, err := newHub(0, dir, filepath.Join(dir, "inbox"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(h.tempDir) })
	h.room.phoneKey = "test-phone"
	return h
}

func waitForTransfer(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("file operation did not reach the expected state")
}

func activeRequest(h *hub) *fileRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.room.fileRequests) == 0 {
		return nil
	}
	return h.room.fileRequests[0]
}

func openTestConnection(t *testing.T, server *httptest.Server) (*net.TCPConn, string) {
	t.Helper()
	host := strings.TrimPrefix(server.URL, "http://")
	conn, err := net.DialTimeout("tcp", host, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(12 * time.Second)); err != nil {
		t.Fatal(err)
	}
	return conn.(*net.TCPConn), host
}

func TestCanceledUploadDiscardsPartialFile(t *testing.T) {
	for _, source := range []string{"desktop cancel", "peer disconnect"} {
		t.Run(source, func(t *testing.T) {
			h := newTransferTestHub(t)
			if err := os.Mkdir(h.inbox, 0700); err != nil {
				t.Fatal(err)
			}
			oldPath := filepath.Join(h.inbox, "test.bin")
			if err := os.WriteFile(oldPath, []byte("previous download"), 0600); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				h.upload(w, r, false)
			}))
			t.Cleanup(server.Close)
			conn, host := openTestConnection(t, server)
			const id = "1234567890abcdef1234567890abcdef"
			cookie := h.cookieName(false) + "=" + h.room.phoneKey
			_, err := fmt.Fprintf(conn, "POST /api/upload HTTP/1.1\r\nHost: %s\r\nCookie: %s\r\n"+
				"X-Room: 1\r\nX-File-Name: test.bin\r\nX-Transfer-Id: %s\r\nContent-Length: 1024\r\n\r\nfirst block",
				host, cookie, id)
			if err != nil {
				t.Fatal(err)
			}
			waitForTransfer(t, func() bool {
				files, _ := filepath.Glob(filepath.Join(h.inbox, ".near-*.part"))
				if len(files) != 1 {
					return false
				}
				info, err := os.Stat(files[0])
				return err == nil && info.Size() > 0
			})
			operation := activeRequest(h)
			if source == "desktop cancel" {
				if operation == nil || !h.cancelFileRequest(operation) {
					t.Fatal("the upload could not be canceled")
				}
				response, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodPost})
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(response.Body)
				response.Body.Close()
				if err != nil || response.StatusCode != http.StatusConflict || !bytes.Contains(body, []byte(transferCanceled)) {
					t.Fatalf("upload cancel response: status=%d, body=%q, err=%v", response.StatusCode, body, err)
				}
			} else {
				conn.Close()
			}
			waitForTransfer(t, func() bool { return activeRequest(h) == nil })
			if h.cancelFileRequest(operation) {
				t.Fatal("an already finished upload could be canceled again")
			}
			entries, err := os.ReadDir(h.inbox)
			if err != nil || len(entries) != 1 || entries[0].Name() != "test.bin" {
				t.Fatalf("unexpected inbox after cancel: %v, %v", entries, err)
			}
			if data, err := os.ReadFile(oldPath); err != nil || string(data) != "previous download" {
				t.Fatalf("existing file changed: %q, %v", data, err)
			}
			retried, status, message := h.stage(context.Background(), h.room, "p"+id, "test.bin",
				"phone", 3, strings.NewReader("new"), nil)
			if status != http.StatusCreated || retried.Name != "test (2).bin" {
				t.Fatalf("retry did not save a separate file: %v, %d, %s", retried, status, message)
			}
		})
	}
}

func TestCanceledDownloadCanBeRetried(t *testing.T) {
	h := newTransferTestHub(t)
	h.bound = binding{ID: strings.Repeat("a", 48), Key: strings.Repeat("b", 48)}
	data := bytes.Repeat([]byte("a staged file"), 1<<20)
	file, status, message := h.stage(context.Background(), h.room, "file", "test.bin", "desktop",
		int64(len(data)), bytes.NewReader(data), nil)
	if status != http.StatusCreated {
		t.Fatal(message)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.SetPathValue("id", file.ID)
		h.download(w, r, false)
	}))
	t.Cleanup(server.Close)
	conn, host := openTestConnection(t, server)
	ticket := "Bearer " + signature(h.bound.Key, "ticket", server.URL, h.epoch)
	if err := conn.SetReadBuffer(4096); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(conn, "GET /api/files/file HTTP/1.1\r\nHost: %s\r\nAuthorization: %s\r\nX-Download-Cancel: 1\r\n\r\n", host, ticket); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(response.Body, make([]byte, 4096)); err != nil {
		t.Fatal(err)
	}
	if request := activeRequest(h); request == nil || !h.cancelFileRequest(request) {
		t.Fatal("the download could not be canceled during streaming")
	}
	waitForTransfer(t, func() bool { return activeRequest(h) == nil })
	read, err := io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if err == nil || read+4096 >= file.Size {
		t.Fatalf("download did not stop early: copied %d bytes of %d, err=%v", read+4096, file.Size, err)
	}

	retry, err := http.NewRequest(http.MethodGet, server.URL+"/api/files/file", nil)
	if err != nil {
		t.Fatal(err)
	}
	retry.Header.Set("Authorization", ticket)
	response, err = server.Client().Do(retry)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	got, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusOK || !bytes.Equal(got, data) {
		t.Fatalf("download retry changed file data: status=%d, size=%d, err=%v", response.StatusCode, len(got), err)
	}
}

func TestInboxLocationUsesSavedDirectory(t *testing.T) {
	data := t.TempDir()
	inbox := filepath.Join(t.TempDir(), "近传")
	if err := os.WriteFile(filepath.Join(data, "inbox.txt"), []byte("\ufeff"+inbox+"\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := inboxLocation(data, ""); err != nil || got != inbox {
		t.Fatalf("configured inbox: got %q, err=%v", got, err)
	}
	override := t.TempDir()
	if got, err := inboxLocation(data, override); err != nil || got != override {
		t.Fatalf("inbox override: got %q, err=%v", got, err)
	}
}

func TestCancelDesktopPreparationKeepsCompletedFile(t *testing.T) {
	h := newTransferTestHub(t)
	if _, status, message := h.stage(context.Background(), h.room, "complete", "complete.txt", "desktop",
		2, strings.NewReader("ok"), nil); status != http.StatusCreated {
		t.Fatal(message)
	}
	ctx, cancel := context.WithCancel(context.Background())
	input, writer := io.Pipe()
	defer writer.Close()
	defer input.Close()
	defer cancel()
	result := make(chan string, 1)
	go func() {
		_, _, message := h.stage(ctx, h.room, "partial", "partial.txt", "desktop", 16, input, nil)
		result <- message
	}()
	if _, err := writer.Write([]byte("first block")); err != nil {
		t.Fatal(err)
	}
	cancel()
	writer.Close()
	select {
	case message := <-result:
		if message != transferCanceled || len(h.room.files) != 1 || h.room.files[0].ID != "complete" {
			t.Fatalf("cancellation changed the staged file list: %q, %v", message, h.room.files)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("desktop staging did not stop")
	}
	entries, err := os.ReadDir(h.room.dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("partial file was not removed: %v, %v", entries, err)
	}
}
