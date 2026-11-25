package main

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

const textWindowClass = "NearTransferText"
const textWidth, textHeight = 468, 411

const (
	textInput = 100 + iota
	textSend
	textPreview
	textPrevious
	textNext
	textCopy
)

type textControl struct {
	id    int
	class string
	title string
	style uintptr
	frame winRect
}

const editMultiline = 0x200000 | 0x0004 | 0x0040 | 0x1000

var textControls = [...]textControl{
	{textInput, "EDIT", "", editMultiline | 0x10000, winRect{20, 77, 448, 173}},
	{textSend, "BUTTON", "发送文字", 0x10000, winRect{344, 184, 448, 217}},
	{textPreview, "EDIT", "", editMultiline | 0x0800 | 0x10000, winRect{20, 275, 448, 347}},
	{textPrevious, "BUTTON", "上一条", 0x10000, winRect{20, 360, 91, 394}},
	{textNext, "BUTTON", "下一条", 0x10000, winRect{100, 360, 171, 394}},
	{textCopy, "BUTTON", "复制文字", 0x10000, winRect{344, 360, 448, 394}},
}

type textPanel struct {
	owner    *widget
	hwnd     uintptr
	dpi      int32
	font     uintptr
	children [len(textControls)]uintptr
	room     int
	records  []*textTransfer
	selected int
	notice   string
}

var textClassRegistered bool

func openTextPanel(w *widget) error {
	if existing := w.textView; existing != nil {
		showPanelWindow(existing.hwnd)
		return nil
	}
	area := windowsArea(w.anchor)
	window, err := createPanelWindow(w.hwnd, area.center(), textWindowClass, "近传 · 传文字", &textClassRegistered, textProc)
	if err != nil {
		return err
	}
	p := &textPanel{owner: w, hwnd: window, selected: -1}
	w.textView = p
	p.dpi = positionPanel(window, area, windowDPI(window), winPoint{textWidth, textHeight}, nil)
	instance, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	for i, control := range textControls {
		ex := uintptr(0)
		if control.class == "EDIT" {
			ex = 0x200 // WS_EX_CLIENTEDGE
		}
		child, _, err := user32.NewProc("CreateWindowExW").Call(
			ex, uintptr(unsafe.Pointer(wide(control.class))), uintptr(unsafe.Pointer(wide(control.title))),
			control.style|0x50000000, 0, 0, 1, 1, window, uintptr(control.id), instance, 0)
		if child == 0 {
			user32.NewProc("DestroyWindow").Call(window)
			return err
		}
		p.children[i] = child
	}
	send := user32.NewProc("SendMessageW")
	send.Call(p.child(textInput), 0x00c5, textLimit, 0) // EM_SETLIMITTEXT
	send.Call(p.child(textPreview), 0x00c5, textLimit, 0)
	p.positionControls()
	p.refresh()
	showPanelWindow(window)
	user32.NewProc("SetFocus").Call(p.child(textInput))
	return nil
}

func (p *textPanel) child(id int) uintptr { return p.children[id-textInput] }

func (p *textPanel) positionControls() {
	font := fontFor(p.dpi, 14, false)
	for i, control := range textControls {
		rect := control.frame
		child := p.children[i]
		user32.NewProc("MoveWindow").Call(child,
			uintptr(scaleDip(rect.left, p.dpi)), uintptr(scaleDip(rect.top, p.dpi)),
			uintptr(scaleDip(rect.size().x, p.dpi)), uintptr(scaleDip(rect.size().y, p.dpi)), 1)
		user32.NewProc("SendMessageW").Call(child, 0x0030, font, 1) // WM_SETFONT
	}
	removeObject(p.font)
	p.font = font
}

func (p *textPanel) dialogMessage(message *windowMessage) bool {
	if message.window != p.hwnd {
		child, _, _ := user32.NewProc("IsChild").Call(p.hwnd, message.window)
		if child == 0 {
			return false
		}
	}
	handled, _, _ := user32.NewProc("IsDialogMessageW").Call(p.hwnd, uintptr(unsafe.Pointer(message)))
	return handled != 0
}

func (p *textPanel) refresh() {
	h := p.owner.h
	h.mu.Lock()
	current := h.room
	changed := current.number != p.room || len(current.texts) != len(p.records)
	if changed {
		followLatest := current.number != p.room || p.selected < 0 || p.selected == len(p.records)-1
		p.room = current.number
		p.records = append(p.records[:0], current.texts...)
		if followLatest {
			p.selected = len(p.records) - 1
		}
	}
	received := current.receivedTexts
	h.mu.Unlock()
	if p.owner.textRoom != p.room || p.owner.seenTexts != received {
		p.owner.textRoom, p.owner.seenTexts = p.room, received
		p.owner.redraw()
	}
	if changed {
		p.showSelected()
	}
}

func (p *textPanel) showSelected() {
	display := ""
	if p.selected >= 0 {
		// The Windows edit control uses CRLF, while copied data retains the message as sent.
		display = strings.ReplaceAll(strings.ReplaceAll(p.records[p.selected].content, "\r\n", "\n"), "\n", "\r\n")
	}
	setWindowText(p.child(textPreview), display)
	enableWindow(p.child(textCopy), p.selected >= 0)
	enableWindow(p.child(textPrevious), p.selected > 0)
	enableWindow(p.child(textNext), p.selected+1 < len(p.records))
	invalidateWindow.Call(p.hwnd, 0, 0)
}

func setWindowText(window uintptr, text string) {
	user32.NewProc("SetWindowTextW").Call(window, uintptr(unsafe.Pointer(wide(text))))
}

func enableWindow(window uintptr, enabled bool) {
	flag := uintptr(0)
	if enabled {
		flag = 1
	}
	user32.NewProc("EnableWindow").Call(window, flag)
}

func editText(window uintptr) string {
	size, _, _ := user32.NewProc("GetWindowTextLengthW").Call(window)
	buffer := make([]uint16, size+1)
	user32.NewProc("GetWindowTextW").Call(window, uintptr(unsafe.Pointer(&buffer[0])), size+1)
	return syscall.UTF16ToString(buffer)
}

func (p *textPanel) command(id int) {
	switch id {
	case textSend:
		content := editText(p.child(textInput))
		h := p.owner.h
		h.mu.Lock()
		if h.room.number != p.room {
			p.notice = "连接已更新，请重试。"
		} else {
			_, _, message := h.addTextLocked("d"+randomKey()[:32], "desktop", content)
			p.notice = message
			if message == "" {
				p.notice = "文字已发送"
			}
		}
		h.mu.Unlock()
		if p.notice == "文字已发送" {
			setWindowText(p.child(textInput), "")
			p.selected = len(p.records) - 1
		}
		p.refresh()
		user32.NewProc("SetFocus").Call(p.child(textInput))
	case textPrevious, textNext:
		if id == textPrevious && p.selected > 0 {
			p.selected--
		}
		if id == textNext && p.selected+1 < len(p.records) {
			p.selected++
		}
		p.showSelected()
	case textCopy:
		if p.selected >= 0 {
			if err := copyText(p.hwnd, p.records[p.selected].content); err != nil {
				p.notice = "暂时无法复制，请重试。"
			} else {
				p.notice = "已复制到剪贴板"
			}
		}
	}
	invalidateWindow.Call(p.hwnd, 0, 0)
}

func (p *textPanel) paint(dc uintptr) {
	c := painter{dc, p.dpi}
	c.fill(winRect{0, 0, textWidth, textHeight}, 0xf8f9fc)
	c.label("发送文字", 20, 17, 448, 51, 21, cardInk, true)
	c.label("输入或粘贴内容，发送到已绑定的手机。", 20, 49, 448, 73, 12, cardMuted, false)
	if p.notice != "" {
		c.label(p.notice, 20, 184, 330, 217, 12, cardMuted, false)
	}
	c.line(cardLine, 1, 20, 233, 448, 233)
	c.label("本次文字记录", 20, 242, 253, 268, 16, cardInk, true)
	status := "暂无记录"
	if p.selected >= 0 {
		direction := "发出"
		if p.records[p.selected].Sender == "phone" {
			direction = "收到"
		}
		status = fmt.Sprintf("%s  ·  %d / %d", direction, p.selected+1, len(p.records))
	}
	c.drawText(status, winRect{260, 242, 448, 268}, 12, cardMuted, false, 2)
}

func textProc(window uintptr, message uint32, wParam, lParam uintptr) uintptr {
	if activeWidget == nil || activeWidget.textView == nil || activeWidget.textView.hwnd != window {
		result, _, _ := defWindowProc.Call(window, uintptr(message), wParam, lParam)
		return result
	}
	p := activeWidget.textView
	switch message {
	case 0x0014: // WM_ERASEBKGND
		return 1
	case 0x000f: // WM_PAINT
		var paint windowPaint
		user32.NewProc("BeginPaint").Call(window, uintptr(unsafe.Pointer(&paint)))
		paintBuffered(paint.dc, winPoint{scaleDip(textWidth, p.dpi), scaleDip(textHeight, p.dpi)}, p.paint)
		user32.NewProc("EndPaint").Call(window, uintptr(unsafe.Pointer(&paint)))
		return 0
	case 0x0138: // WM_CTLCOLORSTATIC, including the read-only edit
		gdi32.NewProc("SetBkColor").Call(wParam, colorRef(cardWhite))
		white, _, _ := gdi32.NewProc("GetStockObject").Call(0)
		return white
	case 0x0111: // WM_COMMAND
		id := int(wParam & 0xffff)
		if id == 2 && lParam == 0 { // IDCANCEL
			user32.NewProc("DestroyWindow").Call(window)
		} else if (wParam>>16)&0xffff == 0 {
			p.command(id)
		}
		return 0
	case 0x02e0: // WM_DPICHANGED
		var suggested winRect
		copySystemMemory.Call(uintptr(unsafe.Pointer(&suggested)), lParam, unsafe.Sizeof(suggested))
		origin := suggested.origin()
		p.dpi = positionPanel(window, windowsArea(suggested.center()), int32(wParam&0xffff), winPoint{textWidth, textHeight}, &origin)
		p.positionControls()
		invalidateWindow.Call(window, 0, 0)
		return 0
	case 0x0002: // WM_DESTROY
		removeObject(p.font)
		p.owner.textView = nil
		return 0
	}
	result, _, _ := defWindowProc.Call(window, uintptr(message), wParam, lParam)
	return result
}
