package main

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const textLimit = 256 << 10

type textTransfer struct {
	ID        string    `json:"id"`
	Sender    string    `json:"sender"`
	CreatedAt time.Time `json:"createdAt"`
	Preview   string    `json:"preview"`
	content   string
}

// addTextLocked records a message after the caller has checked its session.
func (h *hub) addTextLocked(id, sender, content string) (*textTransfer, int, string) {
	if h.stopping || !h.paired() {
		return nil, http.StatusConflict, "请连接设备后发送文字。"
	}
	if existing := h.room.textByID[id]; existing != nil {
		return existing, http.StatusOK, ""
	}
	if len(content) > textLimit {
		return nil, http.StatusRequestEntityTooLarge, "单条文字最多 256 KB。"
	}
	if !utf8.ValidString(content) || strings.TrimSpace(content) == "" {
		return nil, http.StatusBadRequest, "请输入要发送的文字。"
	}
	if strings.ContainsRune(content, 0) {
		return nil, http.StatusBadRequest, "文字包含无法复制的空字符。"
	}
	preview := strings.Join(strings.Fields(content), " ")
	short := []rune(preview)
	if len(short) > 72 {
		preview = string(short[:72]) + "…"
	}
	record := &textTransfer{
		ID: id, Sender: sender, CreatedAt: time.Now(), Preview: preview, content: content,
	}
	current := h.room
	current.texts = append(current.texts, record)
	current.textByID[id] = record
	if sender == "phone" {
		current.receivedTexts++
		current.phoneSeen = record.CreatedAt
	}
	return record, http.StatusCreated, ""
}

func (h *hub) postText(w http.ResponseWriter, r *http.Request, desktop bool) {
	h.mu.Lock()
	allowed := h.authorized(r, desktop)
	h.mu.Unlock()
	if !allowed {
		apiError(w, http.StatusUnauthorized, "请连接设备后发送文字。")
		return
	}
	key := r.Header.Get("X-Transfer-Id")
	if !validHex(key, 32) {
		apiError(w, http.StatusBadRequest, "文字传输标识有误，请重试。")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, textLimit))
	if err != nil {
		apiError(w, http.StatusRequestEntityTooLarge, "请确认单条文字在 256 KB 以内，再重试。")
		return
	}
	sender := "phone"
	if desktop {
		sender = "desktop"
	}
	h.mu.Lock()
	if !h.authorized(r, desktop) || r.Header.Get("X-Room") != strconv.Itoa(h.room.number) {
		h.mu.Unlock()
		apiError(w, http.StatusConflict, "连接已更新，请重试。")
		return
	}
	record, status, message := h.addTextLocked(sender[:1]+key, sender, string(body))
	h.mu.Unlock()
	if message != "" {
		apiError(w, status, message)
		return
	}
	writeJSON(w, status, record)
}

func (h *hub) getText(w http.ResponseWriter, r *http.Request, desktop bool) {
	h.mu.Lock()
	if h.stopping || !h.authorized(r, desktop) {
		h.mu.Unlock()
		apiError(w, http.StatusUnauthorized, "请连接设备后查看文字。")
		return
	}
	record := h.room.textByID[r.PathValue("id")]
	if !desktop {
		h.room.phoneSeen = time.Now()
	}
	h.mu.Unlock()
	if record == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, record.content)
}
