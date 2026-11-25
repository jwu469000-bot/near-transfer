package main

import (
	"fmt"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"github.com/skip2/go-qrcode"
)

const setupClass = "NearTransferSetup"
const setupStyle = 0x00c80000 // WS_CAPTION | WS_SYSMENU
const setupWidth, setupHeight = 468, 340

const (
	pairTab = iota
	installTab
	setupPrimary
	setupNetwork
)

var setupButtons = [...]winRect{
	{20, 16, 233, 49},
	{235, 16, 448, 49},
	{221, 263, 328, 299},
	{339, 263, 448, 299},
}

type setupState struct {
	address, code, payload string
	choices                int
	paired, online         bool
}

type setupPanel struct {
	owner   *widget
	hwnd    uintptr
	dpi     int32
	tab     int
	focus   int
	network string
	state   setupState
	qr      [][]bool
	notice  string
}

var setupClassRegistered bool

func openSetup(w *widget) error {
	if existing := w.setupView; existing != nil {
		showPanelWindow(existing.hwnd)
		return nil
	}
	area := windowsArea(w.anchor)
	panel := &setupPanel{owner: w, dpi: w.screenDPI, focus: -1}
	window, err := createPanelWindow(w.hwnd, area.center(), setupClass, "近传 · 配对与安装", &setupClassRegistered, setupProc)
	if err != nil {
		return err
	}
	panel.hwnd = window
	w.setupView = panel
	panel.dpi = positionPanel(window, area, windowDPI(window), winPoint{setupWidth, setupHeight}, nil)
	panel.refresh()
	showPanelWindow(window)
	return nil
}

func createPanelWindow(owner uintptr, center winPoint, className, title string, registered *bool,
	callback func(uintptr, uint32, uintptr, uintptr) uintptr) (uintptr, error) {
	instance, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	if !*registered {
		name := wide(className)
		icon, _, _ := user32.NewProc("LoadIconW").Call(instance, 1)
		cursor, _, _ := user32.NewProc("LoadCursorW").Call(0, 32512)
		class := windowClass{
			size: uint32(unsafe.Sizeof(windowClass{})), proc: syscall.NewCallback(callback),
			instance: instance, icon: icon, smallIcon: icon, cursor: cursor,
			name: uintptr(unsafe.Pointer(name)),
		}
		atom, _, err := user32.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&class)))
		runtime.KeepAlive(name)
		if atom == 0 {
			return 0, err
		}
		*registered = true
	}

	window, _, err := user32.NewProc("CreateWindowExW").Call(0,
		uintptr(unsafe.Pointer(wide(className))), uintptr(unsafe.Pointer(wide(title))),
		setupStyle, uintptr(center.x), uintptr(center.y), 1, 1, owner, 0, instance, 0)
	if window == 0 {
		return 0, err
	}
	return window, nil
}

func showPanelWindow(window uintptr) {
	user32.NewProc("ShowWindow").Call(window, 9)
	user32.NewProc("SetForegroundWindow").Call(window)
}

func positionPanel(window uintptr, area winRect, dpi int32, dimensions winPoint, suggested *winPoint) int32 {
	layoutDPI := fitDPI(dpi, dimensions.add(winPoint{20, 48}), area)
	frame := winRect{0, 0, scaleDip(dimensions.x, layoutDPI), scaleDip(dimensions.y, layoutDPI)}
	adjust := user32.NewProc("AdjustWindowRectExForDpi")
	if adjust.Find() == nil {
		adjust.Call(uintptr(unsafe.Pointer(&frame)), setupStyle, 0, 0, uintptr(dpi))
	} else {
		user32.NewProc("AdjustWindowRectEx").Call(uintptr(unsafe.Pointer(&frame)), setupStyle, 0, 0)
	}
	size := frame.size()
	origin := area.center().sub(winPoint{size.x / 2, size.y / 2})
	if suggested != nil {
		origin = *suggested
	}
	origin = fitPoint(origin, size, area)
	user32.NewProc("SetWindowPos").Call(window, 0, uintptr(origin.x), uintptr(origin.y), uintptr(size.x), uintptr(size.y), 0x14)
	return layoutDPI
}

func (p *setupPanel) snapshot() setupState {
	h := p.owner.h
	h.mu.Lock()
	defer h.mu.Unlock()
	current := h.room
	state := setupState{
		paired: h.paired(), choices: len(h.endpoints),
		online: current.phoneTransfers > 0 || time.Since(current.phoneSeen) < 14*time.Second,
	}
	if state.choices == 0 {
		return state
	}
	state.address = h.endpoints[0].address
	for _, e := range h.endpoints {
		if e.address == p.network {
			state.address = e.address
			break
		}
	}
	p.network = state.address
	switch {
	case p.tab == installTab && len(installerAPK) > 0:
		state.payload = "http://" + state.address + "/android"
	case p.tab == pairTab && !state.paired:
		state.code = current.manualCode
		state.payload = pairURL(state.address, current.pairCode)
	}
	return state
}

func (p *setupPanel) refresh() {
	next := p.snapshot()
	if next == p.state {
		return
	}
	if next.payload != p.state.payload {
		p.qr = nil
		if next.payload != "" {
			if code, err := qrcode.New(next.payload, qrcode.Medium); err == nil {
				p.qr = code.Bitmap()
			}
		}
	}
	p.state = next
	p.redraw()
}

func (p *setupPanel) redraw() { invalidateWindow.Call(p.hwnd, 0, 0) }

func (p *setupPanel) enabled(action int) bool {
	switch action {
	case pairTab, installTab:
		return true
	case setupPrimary:
		if p.tab == installTab {
			return len(installerAPK) > 0
		}
		return p.state.address != ""
	case setupNetwork:
		return p.state.choices > 1
	}
	return false
}

func (p *setupPanel) target(x, y int32) int {
	for action, rect := range setupButtons {
		pixelRect := winRect{
			scaleDip(rect.left, p.dpi), scaleDip(rect.top, p.dpi),
			scaleDip(rect.right, p.dpi), scaleDip(rect.bottom, p.dpi),
		}
		if pixelRect.contains(winPoint{x, y}) && p.enabled(action) {
			return action
		}
	}
	return -1
}

func (p *setupPanel) choose(action int) {
	switch action {
	case pairTab, installTab:
		p.tab = action
		p.notice = ""
		p.refresh()
		p.redraw()
	case setupPrimary:
		if !p.enabled(action) {
			return
		}
		if p.tab == installTab {
			p.saveInstaller()
		} else if err := copyText(p.hwnd, p.state.address); err != nil {
			p.notice = "复制失败，可手动填写上方地址。"
		} else {
			p.notice = "电脑地址已复制。"
		}
		p.redraw()
	case setupNetwork:
		h := p.owner.h
		h.mu.Lock()
		for i, e := range h.endpoints {
			if e.address == p.state.address {
				p.network = h.endpoints[(i+1)%len(h.endpoints)].address
				break
			}
		}
		h.mu.Unlock()
		p.notice = ""
		p.refresh()
	}
}

func setupProc(window uintptr, message uint32, wParam, lParam uintptr) uintptr {
	if activeWidget == nil || activeWidget.setupView == nil || activeWidget.setupView.hwnd != window {
		result, _, _ := defWindowProc.Call(window, uintptr(message), wParam, lParam)
		return result
	}
	p := activeWidget.setupView
	switch message {
	case 0x0014: // WM_ERASEBKGND
		return 1
	case 0x000f: // WM_PAINT
		var paint windowPaint
		user32.NewProc("BeginPaint").Call(window, uintptr(unsafe.Pointer(&paint)))
		paintBuffered(paint.dc, winPoint{scaleDip(setupWidth, p.dpi), scaleDip(setupHeight, p.dpi)}, p.paint)
		user32.NewProc("EndPaint").Call(window, uintptr(unsafe.Pointer(&paint)))
		return 0
	case 0x0317, 0x0318: // WM_PRINT, WM_PRINTCLIENT
		p.paint(wParam)
		return 0
	case 0x0202: // WM_LBUTTONUP
		p.choose(p.target(int32(int16(lParam)), int32(int16(lParam>>16))))
		return 0
	case 0x0020: // WM_SETCURSOR
		if uint16(lParam) == 1 { // HTCLIENT
			var point winPoint
			user32.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&point)))
			user32.NewProc("ScreenToClient").Call(window, uintptr(unsafe.Pointer(&point)))
			id := uintptr(32512)
			if p.target(point.x, point.y) >= 0 {
				id = 32649
			}
			cursor, _, _ := user32.NewProc("LoadCursorW").Call(0, id)
			user32.NewProc("SetCursor").Call(cursor)
			return 1
		}
	case 0x0100: // WM_KEYDOWN
		switch wParam {
		case 9:
			shift, _, _ := user32.NewProc("GetKeyState").Call(0x10)
			step := 1
			if int16(shift) < 0 {
				step = -1
				if p.focus < 0 {
					p.focus = 0
				}
			}
			for {
				p.focus = (p.focus + step + len(setupButtons)) % len(setupButtons)
				if p.enabled(p.focus) {
					break
				}
			}
			p.redraw()
		case 13, 32:
			if p.focus >= 0 {
				p.choose(p.focus)
			}
		case 27:
			user32.NewProc("DestroyWindow").Call(window)
		}
		return 0
	case 0x0008: // WM_KILLFOCUS
		p.focus = -1
		p.redraw()
		return 0
	case 0x02e0: // WM_DPICHANGED
		var suggested winRect
		copySystemMemory.Call(uintptr(unsafe.Pointer(&suggested)), lParam, unsafe.Sizeof(suggested))
		origin := suggested.origin()
		p.dpi = positionPanel(window, windowsArea(suggested.center()), int32(wParam&0xffff), winPoint{setupWidth, setupHeight}, &origin)
		p.redraw()
		return 0
	case 0x0002: // WM_DESTROY
		p.owner.setupView = nil
		return 0
	}
	result, _, _ := defWindowProc.Call(window, uintptr(message), wParam, lParam)
	return result
}

func copyText(window uintptr, text string) error {
	value, err := syscall.UTF16FromString(text)
	if err != nil {
		return err
	}
	size := uintptr(len(value) * 2)
	handle, _, err := kernel32.NewProc("GlobalAlloc").Call(2, size)
	if handle == 0 {
		return err
	}
	defer func() {
		if handle != 0 {
			kernel32.NewProc("GlobalFree").Call(handle)
		}
	}()
	pointer, _, err := kernel32.NewProc("GlobalLock").Call(handle)
	if pointer == 0 {
		return err
	}
	copySystemMemory.Call(pointer, uintptr(unsafe.Pointer(&value[0])), size)
	kernel32.NewProc("GlobalUnlock").Call(handle)
	if ok, _, err := user32.NewProc("OpenClipboard").Call(window); ok == 0 {
		return err
	}
	defer user32.NewProc("CloseClipboard").Call()
	if ok, _, err := user32.NewProc("EmptyClipboard").Call(); ok == 0 {
		return err
	}
	if ok, _, err := user32.NewProc("SetClipboardData").Call(13, handle); ok == 0 { // CF_UNICODETEXT
		return err
	}
	handle = 0 // Windows now owns the memory.
	return nil
}

func (p *setupPanel) saveInstaller() {
	buffer := make([]uint16, 32768)
	copy(buffer, syscall.StringToUTF16(installerName))
	title := wide("保存近传安卓安装包")
	initialDir, _ := downloadsFolder()
	folder, extension := wide(initialDir), wide("apk")
	filter := syscall.StringToUTF16("Android 安装包")
	filter = append(filter, '*', '.', 'a', 'p', 'k', 0, 0)
	dialog := openFileName{
		size: uint32(unsafe.Sizeof(openFileName{})), owner: p.hwnd,
		title: title, initialDir: folder, defaultExt: extension, filter: &filter[0],
		file: &buffer[0], maxFile: uint32(len(buffer)), filterIndex: 1,
		flags: 0x80000 | 0x800 | 0x8 | 0x2,
	}
	if !showFileDialog(&dialog, "GetSaveFileNameW") {
		return
	}
	filename := syscall.UTF16ToString(buffer)
	if err := writeAtomic(filename, installerAPK); err != nil {
		messageBox(p.hwnd, "保存安卓安装包", "保存失败，请选择可写入的文件夹。", 0x10)
		return
	}
	p.notice = fmt.Sprintf("已保存 %s", filepath.Base(filename))
}
