package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"image/color"
	"io"
	"io/fs"
	"math/big"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/skip2/go-qrcode"
)

//go:embed web
var webFiles embed.FS

var indexPage = template.Must(template.ParseFS(webFiles, "web/index.html"))
var installerAPK, _ = webFiles.ReadFile("web/install/near-transfer-android.apk")

const androidVersion = "1.2.0"
const installerName = "NearTransfer-Android-" + androidVersion + ".apk"
const transferCanceled = "传输已取消。"

type transfer struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	Sender    string    `json:"sender"`
	CreatedAt time.Time `json:"createdAt"`
	path      string
}

type room struct {
	number         int
	dir            string
	pairCode       string
	manualCode     string
	codeWindow     time.Time
	codeFailures   int
	phoneKey       string
	phoneSeen      time.Time
	phoneTransfers int
	received       int
	files          []*transfer
	texts          []*textTransfer
	textByID       map[string]*textTransfer
	receivedTexts  int
	byID           map[string]*transfer
	uploading      map[string]bool
	fileRequests   []*fileRequest
	active         int
	retired        bool
}

type fileRequest struct {
	ctx         context.Context
	cancel      context.CancelFunc
	setDeadline func(time.Time) error
	caption     string
	committed   bool // guarded by hub.mu
}

func newFileRequest(parent context.Context, caption string, setDeadline func(time.Time) error) *fileRequest {
	ctx, cancel := context.WithCancel(parent)
	return &fileRequest{ctx: ctx, cancel: cancel, setDeadline: setDeadline, caption: caption}
}

type hub struct {
	mu             sync.Mutex
	room           *room
	tempDir        string
	dataDir        string
	inbox          string
	bound          binding
	epoch          string
	desktopKey     string
	port           string
	endpoints      []endpoint
	stopping       bool
	stop           chan struct{}
	fileOperations sync.WaitGroup
	reservedBytes  uint64
}

func newHub(port int, dataDir, inbox string) (*hub, error) {
	device, err := loadBinding(dataDir)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "near-transfer-")
	if err != nil {
		return nil, err
	}
	h := &hub{
		tempDir: dir, dataDir: dataDir, inbox: inbox, bound: device, epoch: randomKey(),
		desktopKey: randomKey(), port: strconv.Itoa(port), stop: make(chan struct{}),
	}
	h.room, err = h.makeRoom(1)
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	return h, nil
}

func (h *hub) makeRoom(number int) (*room, error) {
	dir, err := os.MkdirTemp(h.tempDir, "room-")
	if err != nil {
		return nil, err
	}
	pin, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		os.Remove(dir)
		return nil, err
	}
	return &room{
		number: number, dir: dir, pairCode: randomKey(), manualCode: fmt.Sprintf("%06d", pin.Int64()),
		byID: make(map[string]*transfer), uploading: make(map[string]bool),
		textByID: make(map[string]*textTransfer),
	}, nil
}

func randomKey() string {
	key := make([]byte, 24)
	if _, err := rand.Read(key); err != nil {
		panic(err)
	}
	return hex.EncodeToString(key)
}

func (h *hub) handler(desktop bool) http.Handler {
	assets, _ := fs.Sub(webFiles, "web")
	static := http.FileServer(http.FS(assets))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		side := "phone"
		if desktop {
			side = "desktop"
			h.setCookie(w, true, h.desktopKey)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		indexPage.Execute(w, side)
	})
	mux.Handle("GET /assets/", static)
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) { h.state(w, r, desktop) })
	mux.HandleFunc("POST /api/upload", func(w http.ResponseWriter, r *http.Request) { h.upload(w, r, desktop) })
	mux.HandleFunc("GET /api/files/{id}", func(w http.ResponseWriter, r *http.Request) { h.download(w, r, desktop) })
	mux.HandleFunc("POST /api/text", func(w http.ResponseWriter, r *http.Request) { h.postText(w, r, desktop) })
	mux.HandleFunc("GET /api/text/{id}", func(w http.ResponseWriter, r *http.Request) { h.getText(w, r, desktop) })
	if len(installerAPK) > 0 {
		mux.HandleFunc("GET /android", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/vnd.android.package-archive")
			w.Header().Set("Content-Disposition", "attachment; filename="+installerName)
			http.ServeContent(w, r, installerName, time.Time{}, bytes.NewReader(installerAPK))
		})
	}
	if desktop {
		mux.HandleFunc("GET /api/qr", h.qr)
		mux.HandleFunc("POST /api/reset", func(w http.ResponseWriter, r *http.Request) { h.reset(w, r, true) })
		mux.HandleFunc("POST /api/stop", h.requestStop)
	} else {
		mux.HandleFunc("POST /api/pair", h.pair)
		mux.HandleFunc("GET /api/identity", h.identity)
		mux.HandleFunc("POST /api/leave", func(w http.ResponseWriter, r *http.Request) { h.reset(w, r, false) })
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; object-src 'none'; base-uri 'none'; form-action 'self'")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")

		local, _ := r.Context().Value(http.LocalAddrContextKey).(*net.TCPAddr)
		remote, _, _ := net.SplitHostPort(r.RemoteAddr)
		peer := net.ParseIP(remote)
		if local == nil || r.Host != local.String() ||
			(desktop && !peer.IsLoopback()) || (!desktop && !h.reachable(r.Host, peer)) {
			apiError(w, http.StatusForbidden, "请从同一网络访问此地址。")
			return
		}
		if r.Method == "POST" && r.Header.Get("Origin") != "http://"+r.Host {
			apiError(w, http.StatusForbidden, "请从近传页面发起操作。")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (h *hub) reachable(local string, peer net.IP) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, e := range h.endpoints {
		if e.address == local && e.subnet.Contains(peer) {
			return true
		}
	}
	return false
}

// Paired status is read with mu held.
func (h *hub) paired() bool { return h.bound.ID != "" || h.room.phoneKey != "" }

func (h *hub) cookieName(desktop bool) string {
	if desktop {
		return "near_pc_" + h.port
	}
	return "near_phone_" + h.port
}

func (h *hub) setCookie(w http.ResponseWriter, desktop bool, key string) {
	http.SetCookie(w, &http.Cookie{
		Name: h.cookieName(desktop), Value: key, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
}

// authorized is called with mu held.
func (h *hub) authorized(r *http.Request, desktop bool) bool {
	key := h.room.phoneKey
	if desktop {
		key = h.desktopKey
	} else if h.bound.ID != "" {
		ticket := "Bearer " + signature(h.bound.Key, "ticket", "http://"+r.Host, h.epoch)
		return subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(ticket)) == 1
	}
	cookie, err := r.Cookie(h.cookieName(desktop))
	return err == nil && key != "" && subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(key)) == 1
}

func (h *hub) pair(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Code string `json:"code"`
		Mode string `json:"mode"`
		binding
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 384)).Decode(&input); err != nil {
		apiError(w, http.StatusBadRequest, "连接链接有误，请重新扫码。")
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	current := h.room
	if h.stopping {
		apiError(w, http.StatusServiceUnavailable, "电脑已结束本次传输。")
		return
	}
	native := input.Mode == "android"
	if native && (!validHex(input.ID, 48) || !validHex(input.Key, 48)) {
		apiError(w, http.StatusBadRequest, "设备绑定信息有误，请更新安卓端。")
		return
	}
	if native && input.ID == h.bound.ID && subtle.ConstantTimeCompare([]byte(input.Key), []byte(h.bound.Key)) == 1 {
		current.phoneSeen = time.Now()
		writeJSON(w, http.StatusOK, map[string]string{"epoch": h.epoch})
		return
	}
	if !h.authorized(r, false) {
		if h.paired() {
			apiError(w, http.StatusConflict, "连接已失效。请在电脑端更换连接，再扫描新二维码。")
			return
		}
		if len(input.Code) == 6 {
			now := time.Now()
			if now.After(current.codeWindow) {
				current.codeWindow = now.Add(time.Minute)
				current.codeFailures = 0
			}
			if current.codeFailures >= 8 {
				apiError(w, http.StatusTooManyRequests, "配对码尝试次数过多，请扫码连接或稍后重试。")
				return
			}
			if subtle.ConstantTimeCompare([]byte(current.manualCode), []byte(input.Code)) != 1 {
				current.codeFailures++
				apiError(w, http.StatusForbidden, "配对码有误，请查看电脑上显示的六位数字。")
				return
			}
		} else if subtle.ConstantTimeCompare([]byte(current.pairCode), []byte(input.Code)) != 1 {
			apiError(w, http.StatusConflict, "二维码已失效，请扫描电脑端的新二维码。")
			return
		}
		if native {
			if err := saveBinding(h.dataDir, input.binding); err != nil {
				apiError(w, http.StatusInternalServerError, "电脑暂时无法保存绑定，请检查近传的数据目录。")
				return
			}
			h.bound = input.binding
		} else {
			current.phoneKey = randomKey()
		}
	} else if native {
		apiError(w, http.StatusConflict, "请在电脑端解除当前连接，再用安卓端配对。")
		return
	}
	current.phoneSeen = time.Now()
	if native {
		writeJSON(w, http.StatusOK, map[string]string{"epoch": h.epoch})
		return
	}
	h.setCookie(w, false, current.phoneKey)
	w.WriteHeader(http.StatusNoContent)
}

type link struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	URL     string `json:"url"`
}

type roomState struct {
	Room       int             `json:"room"`
	Paired     bool            `json:"paired"`
	Bound      bool            `json:"bound"`
	Online     bool            `json:"online"`
	Links      []link          `json:"links"`
	ManualCode string          `json:"manualCode,omitempty"`
	Installer  bool            `json:"installer"`
	LANCount   int             `json:"lanCount"`
	Files      []*transfer     `json:"files"`
	Texts      []*textTransfer `json:"texts"`
}

func pairURL(address, code string) string { return "http://" + address + "/#join=" + code }

func (h *hub) state(w http.ResponseWriter, r *http.Request, desktop bool) {
	h.mu.Lock()
	if !h.authorized(r, desktop) {
		h.mu.Unlock()
		apiError(w, http.StatusUnauthorized, "请扫描电脑上显示的二维码。")
		return
	}
	current := h.room
	if !desktop {
		current.phoneSeen = time.Now()
	}
	state := roomState{
		Room:      current.number,
		Paired:    h.paired(),
		Bound:     h.bound.ID != "",
		Online:    current.phoneTransfers > 0 || time.Since(current.phoneSeen) < 18*time.Second,
		Links:     make([]link, 0),
		Files:     append([]*transfer{}, current.files...),
		Texts:     append([]*textTransfer{}, current.texts...),
		Installer: desktop && len(installerAPK) > 0,
		LANCount:  len(h.endpoints),
	}
	if desktop && !state.Paired {
		state.ManualCode = current.manualCode
		for _, e := range h.endpoints {
			state.Links = append(state.Links, link{e.name, e.address, pairURL(e.address, current.pairCode)})
		}
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, state)
}

func (h *hub) qr(w http.ResponseWriter, r *http.Request) {
	index, err := strconv.Atoi(r.URL.Query().Get("index"))
	install := r.URL.Query().Get("install") == "1"
	h.mu.Lock()
	switch {
	case !h.authorized(r, true):
		h.mu.Unlock()
		apiError(w, http.StatusUnauthorized, "请重新打开电脑端。")
		return
	case err != nil || index < 0 || index >= len(h.endpoints),
		install && len(installerAPK) == 0,
		!install && (r.URL.Query().Get("room") != strconv.Itoa(h.room.number) || h.paired()):
		h.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	host := h.endpoints[index].address
	address := "http://" + host + "/android"
	if !install {
		address = pairURL(host, h.room.pairCode)
	}
	h.mu.Unlock()

	code, err := qrcode.New(address, qrcode.Medium)
	if err == nil {
		code.ForegroundColor = color.RGBA{20, 28, 49, 255}
		var img []byte
		img, err = code.PNG(294)
		if err == nil {
			w.Header().Set("Content-Type", "image/png")
			w.Write(img)
			return
		}
	}
	apiError(w, http.StatusInternalServerError, "二维码生成失败，请使用连接链接。")
}

func (h *hub) reset(w http.ResponseWriter, r *http.Request, desktop bool) {
	h.mu.Lock()
	if !h.authorized(r, desktop) || h.stopping {
		h.mu.Unlock()
		apiError(w, http.StatusUnauthorized, "当前连接已结束。")
		return
	}
	err := h.resetLocked()
	h.mu.Unlock()
	if err != nil {
		apiError(w, http.StatusInternalServerError, "无法解除绑定，请检查电脑的存储空间。")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *hub) resetLocked() error {
	old := h.room
	next, err := h.makeRoom(old.number + 1)
	if err != nil {
		return err
	}
	if err := saveBinding(h.dataDir, binding{}); err != nil {
		os.RemoveAll(next.dir)
		return err
	}
	h.bound = binding{}
	h.room = next
	old.retired = true
	if old.active == 0 {
		os.RemoveAll(old.dir)
	}
	return nil
}

func (h *hub) requestStop(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	authorized := h.authorized(r, true)
	h.mu.Unlock()
	if !authorized {
		apiError(w, http.StatusUnauthorized, "请重新打开电脑端。")
		return
	}
	w.WriteHeader(http.StatusNoContent)
	http.NewResponseController(w).Flush()
	h.beginShutdown()
}

func (h *hub) beginShutdown() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.stopping {
		h.stopping = true
		close(h.stop)
	}
}

// startFileOperation is called with mu held before opening a staged file.
func (h *hub) startFileOperation(current *room, desktop bool, request *fileRequest) {
	h.fileOperations.Add(1)
	current.active++
	if request != nil {
		current.fileRequests = append(current.fileRequests, request)
	}
	if !desktop {
		current.phoneTransfers++
		current.phoneSeen = time.Now()
	}
}

func (h *hub) finishFileOperation(current *room, desktop bool, request *fileRequest) {
	defer h.fileOperations.Done()
	h.mu.Lock()
	current.active--
	if request != nil {
		at := slices.Index(current.fileRequests, request)
		current.fileRequests = slices.Delete(current.fileRequests, at, at+1)
	}
	if !desktop {
		current.phoneTransfers--
		current.phoneSeen = time.Now()
	}
	remove := current.retired && current.active == 0
	h.mu.Unlock()
	if remove {
		os.RemoveAll(current.dir)
	}
}

func (h *hub) cancelFileRequest(request *fileRequest) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if request.committed || request.ctx.Err() != nil || !slices.Contains(h.room.fileRequests, request) {
		return false
	}
	request.cancel()
	// Interrupt the blocked read or write before the handler can release its connection.
	_ = request.setDeadline(time.Now())
	return true
}

func (h *hub) upload(w http.ResponseWriter, r *http.Request, desktop bool) {
	sender := "phone"
	if desktop {
		sender = "desktop"
	}
	opID, err := hex.DecodeString(r.Header.Get("X-Transfer-Id"))
	providedName, nameErr := url.PathUnescape(r.Header.Get("X-File-Name"))
	if err != nil || len(opID) != 16 || nameErr != nil || providedName == "" {
		apiError(w, http.StatusBadRequest, "文件信息不完整，请重新选择文件。")
		return
	}
	if r.ContentLength < 0 {
		apiError(w, http.StatusLengthRequired, "浏览器没有提供文件大小，请换个浏览器重试。")
		return
	}
	id := sender[:1] + hex.EncodeToString(opID)
	h.mu.Lock()
	current := h.room
	if !h.authorized(r, desktop) {
		h.mu.Unlock()
		apiError(w, http.StatusUnauthorized, "请重新扫码连接。")
		return
	}
	h.mu.Unlock()
	if r.Header.Get("X-Room") != strconv.Itoa(current.number) {
		apiError(w, http.StatusConflict, "会话已经切换，请重新选择文件。")
		return
	}
	name := cleanFilename(providedName)
	caption := "正在接收 " + name
	if desktop {
		caption = "正在准备 " + name
	}
	request := newFileRequest(r.Context(), caption, http.NewResponseController(w).SetReadDeadline)
	defer request.cancel()
	record, status, message := h.stage(request.ctx, current, id, name, sender, r.ContentLength, r.Body, request)
	if message != "" {
		if request.ctx.Err() != nil {
			w.Header().Set("Connection", "close")
		}
		apiError(w, status, message)
		return
	}
	writeJSON(w, status, record)
}

func (h *hub) stage(ctx context.Context, current *room, id, name, sender string, length int64, body io.Reader, request *fileRequest) (*transfer, int, string) {
	const diskReserve = 512 << 20
	desktop := sender == "desktop"
	size := uint64(length)
	h.mu.Lock()
	if ctx.Err() != nil {
		h.mu.Unlock()
		return nil, http.StatusConflict, transferCanceled
	}
	if h.stopping || h.room != current {
		h.mu.Unlock()
		return nil, http.StatusConflict, "会话已经切换，请重新选择文件。"
	}
	if existing := current.byID[id]; existing != nil {
		h.mu.Unlock()
		return existing, http.StatusOK, ""
	}
	if current.uploading[id] {
		h.mu.Unlock()
		return nil, http.StatusConflict, "此文件正在传输，请稍后重试。"
	}
	dir := current.dir
	if !desktop {
		dir = h.inbox
		if err := os.MkdirAll(dir, 0700); err != nil {
			h.mu.Unlock()
			return nil, http.StatusInsufficientStorage, "电脑无法创建收件箱。"
		}
	}
	available, diskErr := availableDiskBytes(dir)
	if diskErr == nil && (available < diskReserve+h.reservedBytes || size > available-diskReserve-h.reservedBytes) {
		h.mu.Unlock()
		return nil, http.StatusInsufficientStorage, "电脑的存储空间不足。"
	}
	current.uploading[id] = true
	h.reservedBytes += size
	h.startFileOperation(current, desktop, request)
	h.mu.Unlock()
	defer h.finishFileOperation(current, desktop, request)
	defer func() {
		h.mu.Lock()
		delete(current.uploading, id)
		h.reservedBytes -= size
		h.mu.Unlock()
	}()

	temp, err := os.CreateTemp(dir, ".near-*.part")
	if err != nil {
		return nil, http.StatusInsufficientStorage, "电脑无法创建临时文件，请检查磁盘空间。"
	}
	keep := false
	defer func() {
		if !keep {
			os.Remove(temp.Name())
		}
	}()
	input := transferReader{ctx: ctx, stopped: h.stop, input: body}
	written, copyErr := io.Copy(temp, io.LimitReader(input, length))
	closeErr := temp.Close()
	if ctx.Err() != nil {
		return nil, http.StatusConflict, transferCanceled
	}
	if copyErr != nil || closeErr != nil || written != length {
		return nil, http.StatusBadRequest, "文件未完整写入，请检查连接和电脑存储空间后重试。"
	}

	record := &transfer{ID: id, Name: name, Size: written, Sender: sender, CreatedAt: time.Now(), path: temp.Name()}
	h.mu.Lock()
	if ctx.Err() != nil {
		h.mu.Unlock()
		return nil, http.StatusConflict, transferCanceled
	}
	if h.room != current || h.stopping {
		h.mu.Unlock()
		return nil, http.StatusConflict, "会话已经结束，请重新连接。"
	}
	if !desktop {
		record.path, err = commitInbox(temp.Name(), dir, name)
		if err != nil {
			h.mu.Unlock()
			return nil, http.StatusInternalServerError, "文件无法保存到电脑收件箱。"
		}
		record.Name = filepath.Base(record.path)
		current.received++
	}
	current.byID[id] = record
	current.files = append(current.files, record)
	if request != nil {
		request.committed = true
	}
	keep = true
	h.mu.Unlock()
	return record, http.StatusCreated, ""
}

type transferReader struct {
	ctx     context.Context
	stopped <-chan struct{}
	input   io.Reader
}

func (r transferReader) Read(p []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	case <-r.stopped:
		return 0, context.Canceled
	default:
		return r.input.Read(p)
	}
}

func commitInbox(temp, dir, name string) (string, error) {
	extension := filepath.Ext(name)
	base := strings.TrimSuffix(name, extension)
	device := strings.ToUpper(strings.TrimSpace(strings.SplitN(base, ".", 2)[0]))
	if device == "CON" || device == "PRN" || device == "AUX" || device == "NUL" ||
		(len([]rune(device)) == 4 && (strings.HasPrefix(device, "COM") || strings.HasPrefix(device, "LPT"))) {
		base = "_" + base
	}
	shorten := func(value string, limit int) string {
		runes := []rune(value)
		if len(runes) > limit {
			runes = runes[:limit]
		}
		return string(runes)
	}
	base = shorten(base, 105)
	extension = shorten(extension, 16)
	for i := 1; i <= 10000; i++ {
		suffix := ""
		if i > 1 {
			suffix = fmt.Sprintf(" (%d)", i)
		}
		filename := filepath.Join(dir, base+suffix+extension)
		err := moveFileExclusive(temp, filename)
		if err == nil {
			return filename, nil
		}
		if !os.IsExist(err) {
			return "", err
		}
	}
	return "", os.ErrExist
}

func (h *hub) download(w http.ResponseWriter, r *http.Request, desktop bool) {
	h.mu.Lock()
	if !h.authorized(r, desktop) || h.stopping {
		h.mu.Unlock()
		apiError(w, http.StatusUnauthorized, "会话已结束，请重新连接。")
		return
	}
	current := h.room
	record := current.byID[r.PathValue("id")]
	if record == nil {
		h.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	var request *fileRequest
	// The older Android app writes directly to the chosen document. Let it finish
	// unless the client confirms that it can safely discard a partial download.
	if desktop || h.bound.ID == "" || r.Header.Get("X-Download-Cancel") == "1" {
		request = newFileRequest(r.Context(), "正在发送 "+record.Name, http.NewResponseController(w).SetWriteDeadline)
		defer request.cancel()
	}
	h.startFileOperation(current, desktop, request)
	h.mu.Unlock()
	defer h.finishFileOperation(current, desktop, request)

	file, err := os.Open(record.path)
	if err != nil {
		apiError(w, http.StatusGone, "文件已不可用，请重新发送。")
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": record.Name}))
	http.ServeContent(w, r, record.Name, record.CreatedAt, file)
	if request != nil && request.ctx.Err() != nil {
		panic(http.ErrAbortHandler)
	}
}

func cleanFilename(name string) string {
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(ch rune) rune {
		if unicode.IsControl(ch) || strings.ContainsRune(`<>:"|?*`, ch) {
			return '_'
		}
		return ch
	}, name)
	name = strings.Trim(name, " .")
	if name == "" {
		return "文件"
	}
	return name
}

func apiError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
