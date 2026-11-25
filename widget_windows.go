package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const widgetClass = "NearTransferWidget"
const widgetWidth, widgetHeight = 324, 205
const ballDiameter = 42

const (
	sendAction = iota
	inboxAction
	textAction
	menuAction
	collapseAction
	expandAction
)

type winPoint struct{ x, y int32 }
type winRect struct{ left, top, right, bottom int32 }

type windowClass struct {
	size, style                   uint32
	proc                          uintptr
	classExtra, windowExtra       int32
	instance, icon, cursor, brush uintptr
	menu, name, smallIcon         uintptr
}

type windowMessage struct {
	window         uintptr
	message        uint32
	wParam, lParam uintptr
	time           uint32
	point          winPoint
	private        uint32
}

type windowPaint struct {
	dc                   uintptr
	erase                int32
	update               winRect
	restore, incremental int32
	reserved             [32]byte
}

type monitorInfo struct {
	size          uint32
	monitor, work winRect
	flags         uint32
}

type widgetPrefs struct {
	X, Y       int32
	Moved      bool
	BallAnchor bool
	Pin        bool
}

type widgetState struct {
	paired, bound, online, network bool
	room, received, receivedTexts  int
}

type widget struct {
	h                   *hub
	prefs               widgetPrefs
	hwnd                uintptr
	dpi                 int32
	screenDPI           int32
	expanded            bool
	anchor              winPoint
	anchorOffset        winPoint
	drag                ballDrag
	setupView           *setupPanel
	textView            *textPanel
	focus               int
	openRoom, seen      int
	textRoom, seenTexts int
	view                widgetState

	mu         sync.Mutex
	busy       bool
	notice     string
	noticeTill time.Time
}

var activeWidget *widget

var (
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	dwmapi   = syscall.NewLazyDLL("dwmapi.dll")
	comdlg32 = syscall.NewLazyDLL("comdlg32.dll")

	defWindowProc     = user32.NewProc("DefWindowProcW")
	invalidateWindow  = user32.NewProc("InvalidateRect")
	postWindowMessage = user32.NewProc("PostMessageW")
)

func claimWidget() (func(), error) {
	handle, _, err := kernel32.NewProc("CreateMutexW").Call(0, 0, uintptr(unsafe.Pointer(wide("Local\\NearTransfer.Widget"))))
	if handle == 0 {
		return nil, err
	}
	if err == syscall.Errno(183) {
		syscall.CloseHandle(syscall.Handle(handle))
		window, _, _ := user32.NewProc("FindWindowW").Call(uintptr(unsafe.Pointer(wide(widgetClass))), 0)
		if window != 0 {
			user32.NewProc("ShowWindow").Call(window, 9)
			user32.NewProc("SetForegroundWindow").Call(window)
		}
		return nil, nil
	}
	return func() { syscall.CloseHandle(syscall.Handle(handle)) }, nil
}

func showWidget(h *hub) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if result, _, _ := ole32.NewProc("OleInitialize").Call(0); result <= 1 {
		defer ole32.NewProc("OleUninitialize").Call()
	}
	dpiAwareness := user32.NewProc("SetProcessDpiAwarenessContext")
	if dpiAwareness.Find() == nil {
		dpiAwareness.Call(^uintptr(3))
	} else {
		user32.NewProc("SetProcessDPIAware").Call()
	}
	instance, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	icon, _, _ := user32.NewProc("LoadIconW").Call(instance, 1)
	cursor, _, _ := user32.NewProc("LoadCursorW").Call(0, 32512)
	className := wide(widgetClass)
	class := windowClass{
		size: uint32(unsafe.Sizeof(windowClass{})), style: 0x00020003,
		proc: syscall.NewCallback(widgetProc), instance: instance,
		icon: icon, smallIcon: icon, cursor: cursor,
		name: uintptr(unsafe.Pointer(className)),
	}
	ok, _, err := user32.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&class)))
	runtime.KeepAlive(className)
	if ok == 0 {
		return err
	}

	w := &widget{h: h, dpi: 96, screenDPI: 96, focus: -1}
	w.loadPreferences()
	w.view, _ = h.widgetStatus()
	activeWidget = w
	defer func() { activeWidget = nil }()
	ex := uintptr(0x80)
	if w.prefs.Pin {
		ex |= 0x8
	}
	initial := uintptr(0x80000000)
	window, _, err := user32.NewProc("CreateWindowExW").Call(
		ex, uintptr(unsafe.Pointer(wide(widgetClass))), uintptr(unsafe.Pointer(wide("近传 · 单击展开"))),
		0x80080000, initial, initial, uintptr(ballDiameter), uintptr(ballDiameter), 0, 0, instance, 0)
	if window == 0 {
		return err
	}
	w.hwnd = window
	w.place()
	shell32.NewProc("DragAcceptFiles").Call(window, 1)
	user32.NewProc("SetTimer").Call(window, 1, 500, 0)
	defer user32.NewProc("KillTimer").Call(window, 1)
	user32.NewProc("ShowWindow").Call(window, 4)
	user32.NewProc("UpdateWindow").Call(window)

	var message windowMessage
	for {
		status, _, messageErr := user32.NewProc("GetMessageW").Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(status) == -1 {
			return messageErr
		}
		if status == 0 {
			return nil
		}
		if panel := w.textView; panel != nil && panel.dialogMessage(&message) {
			continue
		}
		user32.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&message)))
		user32.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&message)))
	}
}

func (w *widget) prefsFile() string { return filepath.Join(w.h.dataDir, "widget.json") }

func (w *widget) px(dip int32) int32 { return scaleDip(dip, w.dpi) }

func (h *hub) widgetStatus() (widgetState, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	current := h.room
	return widgetState{
		paired: h.paired(), bound: h.bound.ID != "", network: len(h.endpoints) != 0,
		online: current.phoneTransfers > 0 || time.Since(current.phoneSeen) < 14*time.Second,
		room:   current.number, received: current.received, receivedTexts: current.receivedTexts,
	}, h.stopping
}

func (w *widget) refresh() {
	view, stopped := w.h.widgetStatus()
	if stopped {
		w.close()
		return
	}
	if w.setupView != nil {
		w.setupView.refresh()
	}
	if w.textView != nil {
		w.textView.refresh()
	}
	w.mu.Lock()
	expired := w.notice != "" && !w.busy && time.Now().After(w.noticeTill)
	if expired {
		w.notice = ""
	}
	w.mu.Unlock()
	if w.view != view || expired {
		w.view = view
		w.redraw()
	}
}

func (w *widget) redraw() { invalidateWindow.Call(w.hwnd, 0, 0) }

func (w *widget) action(x, y int32) int {
	if !w.expanded {
		if x >= 0 && y >= 0 && x < w.px(ballDiameter) && y < w.px(ballDiameter) {
			return expandAction
		}
		return -1
	}
	switch {
	case x >= w.px(276) && x < w.px(309) && y >= w.px(13) && y < w.px(46):
		return menuAction
	case x >= w.px(235) && x < w.px(268) && y >= w.px(13) && y < w.px(46):
		return collapseAction
	case x >= w.px(14) && x < w.px(310) && y >= w.px(80) && y < w.px(150):
		return sendAction
	case y >= w.px(160) && x >= w.px(13) && x < w.px(311):
		if x < w.px(164) {
			return inboxAction
		}
		return textAction
	default:
		return -1
	}
}

func widgetProc(window uintptr, message uint32, wParam, lParam uintptr) uintptr {
	w := activeWidget
	if w == nil || w.hwnd == 0 {
		result, _, _ := defWindowProc.Call(window, uintptr(message), wParam, lParam)
		return result
	}
	point := winPoint{int32(int16(lParam)), int32(int16(lParam >> 16))}
	switch message {
	case 0x0014: // WM_ERASEBKGND
		return 1
	case 0x000f: // WM_PAINT
		var paint windowPaint
		user32.NewProc("BeginPaint").Call(window, uintptr(unsafe.Pointer(&paint)))
		w.paintBuffered(paint.dc)
		user32.NewProc("EndPaint").Call(window, uintptr(unsafe.Pointer(&paint)))
		return 0
	case 0x0317, 0x0318: // WM_PRINT, WM_PRINTCLIENT
		w.paint(wParam)
		return 0
	case 0x0084: // WM_NCHITTEST
		user32.NewProc("ScreenToClient").Call(window, uintptr(unsafe.Pointer(&point)))
		if w.expanded && point.y >= 0 && point.y < w.px(46) && point.x >= 0 && point.x < w.px(235) {
			return 2 // HTCAPTION
		}
		return 1 // HTCLIENT
	case 0x0020: // WM_SETCURSOR
		user32.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&point)))
		user32.NewProc("ScreenToClient").Call(window, uintptr(unsafe.Pointer(&point)))
		id := uintptr(32512)
		if w.action(point.x, point.y) >= 0 {
			id = 32649
		}
		cursor, _, _ := user32.NewProc("LoadCursorW").Call(0, id)
		user32.NewProc("SetCursor").Call(cursor)
		return 1
	case 0x0201: // WM_LBUTTONDOWN
		if !w.expanded && w.action(point.x, point.y) == expandAction {
			w.beginBallDrag(point)
			return 0
		}
	case 0x0200: // WM_MOUSEMOVE
		if w.drag.active {
			w.moveBall(point)
			return 0
		}
	case 0x0202: // WM_LBUTTONUP
		if w.expanded {
			w.activate(w.action(point.x, point.y))
		} else {
			w.endBallDrag(point)
		}
		return 0
	case 0x0215: // WM_CAPTURECHANGED
		if w.drag.active {
			w.cancelBallDrag()
		}
		return 0
	case 0x007b: // WM_CONTEXTMENU
		w.menu()
		return 0
	case 0x0100: // WM_KEYDOWN
		switch wParam {
		case 9:
			if !w.expanded {
				return 0
			}
			shift, _, _ := user32.NewProc("GetKeyState").Call(0x10)
			if int16(shift) < 0 {
				if w.focus < 0 {
					w.focus = collapseAction
				} else {
					w.focus = (w.focus + collapseAction) % (collapseAction + 1)
				}
			} else {
				w.focus = (w.focus + 1) % (collapseAction + 1)
			}
			w.redraw()
		case 13, 32:
			if w.expanded {
				w.activate(max(0, w.focus))
			} else {
				w.setExpanded(true)
			}
		case 27:
			w.setExpanded(false)
		case 0x79:
			w.menu()
		}
		return 0
	case 0x0008: // WM_KILLFOCUS
		w.focus = -1
		w.redraw()
		return 0
	case 0x0233: // WM_DROPFILES
		w.sendFiles(droppedFiles(wParam))
		return 0
	case 0x0232: // WM_EXITSIZEMOVE
		w.savePosition()
		return 0
	case 0x0046: // WM_WINDOWPOSCHANGING
		w.constrainPosition(lParam)
		return 0
	case 0x02e0: // WM_DPICHANGED
		w.changeDPI(int32(wParam&0xffff), lParam)
		return 0
	case 0x007e: // WM_DISPLAYCHANGE
		w.keepOnScreen()
		return 0
	case 0x001a: // WM_SETTINGCHANGE
		if wParam == 0x2f { // SPI_SETWORKAREA
			w.keepOnScreen()
		}
	case 0x0113: // WM_TIMER
		w.refresh()
		return 0
	case 0x8000: // A file operation updated the widget.
		w.refresh()
		w.redraw()
		return 0
	case 0x0010: // WM_CLOSE
		w.close()
		return 0
	case 0x0002: // WM_DESTROY
		w.h.beginShutdown()
		user32.NewProc("PostQuitMessage").Call(0)
		return 0
	}
	result, _, _ := defWindowProc.Call(window, uintptr(message), wParam, lParam)
	return result
}

func (w *widget) activate(action int) {
	switch action {
	case sendAction:
		w.refresh()
		if !w.view.paired {
			w.setup()
		} else {
			w.sendFiles(pickFiles(w.hwnd))
		}
	case inboxAction:
		w.openInbox()
	case textAction:
		w.refresh()
		if !w.view.paired {
			w.setup()
		} else if err := openTextPanel(w); err != nil {
			messageBox(w.hwnd, "近传 · 传文字", "暂时无法打开文字互传窗口。", 0x10)
		}
	case menuAction:
		w.menu()
	case collapseAction:
		w.setExpanded(false)
	}
}

func (w *widget) setup() {
	if err := openSetup(w); err != nil {
		messageBox(w.hwnd, "近传 · 配对与安装", "暂时无法打开配对窗口。", 0x10)
	}
}

func (w *widget) openInbox() {
	if err := os.MkdirAll(w.h.inbox, 0700); err != nil {
		w.setNotice("无法打开收件箱")
		return
	}
	if err := openPath(w.h.inbox); err != nil {
		w.setNotice("无法打开收件箱")
		return
	}
	w.openRoom, w.seen = w.view.room, w.view.received
	w.redraw()
}

func (w *widget) menu() {
	w.refresh()
	handle, _, _ := user32.NewProc("CreatePopupMenu").Call()
	if handle == 0 {
		return
	}
	defer user32.NewProc("DestroyMenu").Call(handle)
	add := func(id uintptr, label string, flags uintptr) {
		user32.NewProc("AppendMenuW").Call(handle, flags, id, uintptr(unsafe.Pointer(wide(label))))
	}
	check := func(on bool) uintptr {
		if on {
			return 8
		}
		return 0
	}
	add(1, "配对与安装…", 0)
	add(2, "打开收件箱", 0)
	add(0, "", 0x800)
	add(3, "窗口置顶", check(w.prefs.Pin))
	auto := startupEnabled()
	add(4, "登录 Windows 时打开", check(auto))
	flags := uintptr(0)
	if !w.view.paired {
		flags = 1
	}
	add(5, "解除设备绑定…", flags)
	add(0, "", 0x800)
	add(6, "退出近传", 0)
	var pos winPoint
	user32.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&pos)))
	// A fixed anchor keeps the menu near the widget when opened from the keyboard.
	user32.NewProc("ScreenToClient").Call(w.hwnd, uintptr(unsafe.Pointer(&pos)))
	size := w.pixelSize()
	if pos.x < 0 || pos.y < 0 || pos.x >= size.x || pos.y >= size.y {
		pos = winPoint{size.x - w.px(14), min(w.px(43), size.y)}
	}
	user32.NewProc("ClientToScreen").Call(w.hwnd, uintptr(unsafe.Pointer(&pos)))
	user32.NewProc("SetForegroundWindow").Call(w.hwnd)
	id, _, _ := user32.NewProc("TrackPopupMenu").Call(handle, 0x182, uintptr(pos.x), uintptr(pos.y), 0, w.hwnd, 0)
	switch id {
	case 1:
		w.setup()
	case 2:
		w.openInbox()
	case 3:
		w.prefs.Pin = !w.prefs.Pin
		top := ^uintptr(1)
		if w.prefs.Pin {
			top = ^uintptr(0)
		}
		user32.NewProc("SetWindowPos").Call(w.hwnd, top, 0, 0, 0, 0, 0x13)
		w.savePosition()
	case 4:
		if err := setStartup(!auto); err != nil {
			w.setNotice("无法修改登录启动设置")
		}
	case 5:
		if messageBox(w.hwnd, "解除设备绑定", "解除后，下次连接需要重新配对。现在解除绑定吗？", 0x124) == 6 {
			w.h.mu.Lock()
			err := w.h.resetLocked()
			w.h.mu.Unlock()
			if err != nil {
				w.setNotice("无法解除当前绑定")
			}
			w.refresh()
		}
	case 6:
		w.close()
	}
}

func messageBox(window uintptr, title, detail string, flags uintptr) uintptr {
	result, _, _ := user32.NewProc("MessageBoxW").Call(window,
		uintptr(unsafe.Pointer(wide(detail))), uintptr(unsafe.Pointer(wide(title))), flags)
	return result
}

func widgetError(title, detail string) { messageBox(0, title, detail, 0x10) }

func (w *widget) setNotice(message string) {
	w.mu.Lock()
	w.notice = message
	w.noticeTill = time.Now().Add(7 * time.Second)
	w.mu.Unlock()
	postWindowMessage.Call(w.hwnd, 0x8000, 0, 0)
}

func (w *widget) sendFiles(files []string) {
	if len(files) == 0 {
		return
	}
	w.h.mu.Lock()
	current, paired := w.h.room, w.h.paired()
	w.h.mu.Unlock()
	if !paired {
		w.setup()
		return
	}
	w.mu.Lock()
	if w.busy {
		w.mu.Unlock()
		w.setNotice("请等当前文件准备完成")
		return
	}
	w.busy = true
	w.mu.Unlock()
	w.setNotice("正在准备发送…")
	go func() {
		sent := 0
		var lastError string
		for index, filename := range files {
			w.setNotice(fmt.Sprintf("正在准备第 %d / %d 个文件", index+1, len(files)))
			file, err := os.Open(filename)
			if err != nil {
				lastError = "无法读取所选文件"
				continue
			}
			stat, err := file.Stat()
			if err != nil || !stat.Mode().IsRegular() {
				file.Close()
				lastError = "可发送文件，文件夹可先压缩"
				continue
			}
			_, _, lastError = w.h.stage(current, "d"+randomKey()[:32], cleanFilename(stat.Name()), "desktop", stat.Size(), file)
			file.Close()
			if lastError == "" {
				sent++
			}
		}
		w.mu.Lock()
		w.busy = false
		w.mu.Unlock()
		switch {
		case sent == len(files):
			w.setNotice(fmt.Sprintf("已发送 %d 个文件 · 在手机上下载", sent))
		case sent > 0:
			w.setNotice(fmt.Sprintf("已发送 %d 个，另有 %d 个需重试", sent, len(files)-sent))
		default:
			w.setNotice(lastError)
		}
	}()
}

type openFileName struct {
	size                                 uint32
	owner, instance                      uintptr
	filter, customFilter                 *uint16
	maxCustom, filterIndex               uint32
	file                                 *uint16
	maxFile                              uint32
	fileTitle                            *uint16
	maxFileTitle                         uint32
	initialDir, title                    *uint16
	flags                                uint32
	fileOffset, extensionOffset          uint16
	defaultExt                           *uint16
	customData, hook, template, reserved uintptr
	reservedInt, flagsEx                 uint32
}

func showFileDialog(dialog *openFileName, method string) bool {
	// Windows can reenter the Go window procedure while this dialog is open.
	var pinned runtime.Pinner
	pinned.Pin(dialog)
	defer pinned.Unpin()
	for _, buffer := range []*uint16{
		dialog.filter, dialog.customFilter, dialog.file, dialog.fileTitle,
		dialog.initialDir, dialog.title, dialog.defaultExt,
	} {
		if buffer != nil {
			pinned.Pin(buffer)
		}
	}
	ok, _, _ := comdlg32.NewProc(method).Call(uintptr(unsafe.Pointer(dialog)))
	return ok != 0
}

func pickFiles(window uintptr) []string {
	buffer := make([]uint16, 32768)
	title := wide("选择要发送的文件")
	filter := []uint16{'*', '.', '*', 0, '*', '.', '*', 0, 0}
	dialog := openFileName{
		size: uint32(unsafe.Sizeof(openFileName{})), owner: window,
		title: title, filter: &filter[0],
		file: &buffer[0], maxFile: uint32(len(buffer)), filterIndex: 1,
		flags: 0x80000 | 0x1000 | 0x800 | 0x200 | 0x8 | 0x100000,
	}
	if !showFileDialog(&dialog, "GetOpenFileNameW") {
		return nil
	}
	var parts []string
	start := 0
	for end, code := range buffer {
		if code != 0 {
			continue
		}
		if end == start {
			break
		}
		parts = append(parts, syscall.UTF16ToString(buffer[start:end]))
		start = end + 1
	}
	if len(parts) < 2 {
		return parts
	}
	files := parts[1:]
	for i := range files {
		files[i] = filepath.Join(parts[0], files[i])
	}
	return files
}

func droppedFiles(drop uintptr) []string {
	query := shell32.NewProc("DragQueryFileW")
	defer shell32.NewProc("DragFinish").Call(drop)
	number, _, _ := query.Call(drop, 0xffffffff, 0, 0)
	files := make([]string, 0, number)
	for i := uintptr(0); i < number; i++ {
		size, _, _ := query.Call(drop, i, 0, 0)
		if size > 0 {
			buffer := make([]uint16, size+1)
			query.Call(drop, i, uintptr(unsafe.Pointer(&buffer[0])), size+1)
			files = append(files, syscall.UTF16ToString(buffer))
		}
	}
	return files
}
