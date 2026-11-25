package main

import (
	"fmt"
	"unsafe"
)

const (
	cardWhite = 0xffffff
	cardInk   = 0x19223b
	cardMuted = 0x59677e
	cardLine  = 0xe5e8f3
	indigo    = 0x4d45d5
)

func (w *widget) paintBuffered(dc uintptr) {
	if dc == 0 {
		return
	}
	mem, _, _ := gdi32.NewProc("CreateCompatibleDC").Call(dc)
	bitmap, _, _ := gdi32.NewProc("CreateCompatibleBitmap").Call(dc, uintptr(w.px(widgetWidth)), uintptr(w.px(widgetHeight)))
	if mem == 0 || bitmap == 0 {
		if mem != 0 {
			gdi32.NewProc("DeleteDC").Call(mem)
		}
		removeObject(bitmap)
		w.paint(dc)
		return
	}
	saved := selectObject(mem, bitmap)
	w.paint(mem)
	gdi32.NewProc("BitBlt").Call(dc, 0, 0, uintptr(w.px(widgetWidth)), uintptr(w.px(widgetHeight)), mem, 0, 0, 0x00cc0020)
	selectObject(mem, saved)
	removeObject(bitmap)
	gdi32.NewProc("DeleteDC").Call(mem)
}

func (w *widget) paint(dc uintptr) {
	if dc == 0 {
		return
	}
	c := painter{dc, w}
	bounds := winRect{0, 0, w.px(widgetWidth), w.px(widgetHeight)}
	back := brush(cardWhite)
	user32.NewProc("FillRect").Call(dc, uintptr(unsafe.Pointer(&bounds)), back)
	removeObject(back)
	c.round(0, 0, widgetWidth, widgetHeight, 19, cardWhite, cardLine)
	c.round(15, 13, 48, 46, 10, indigo, indigo)
	c.line(0xffffff, 2, 24, 26, 38, 26)
	c.line(0xffffff, 2, 34, 22, 38, 26, 34, 30)
	c.line(0xdcd9ff, 2, 39, 33, 25, 33)
	c.line(0xdcd9ff, 2, 29, 29, 25, 33, 29, 37)
	c.label("近传", 59, 14, 240, 44, 18, cardInk, true)

	menuBorder := uint32(0xf1f2f8)
	if w.focus == 2 {
		menuBorder = 0x9b92ee
	}
	c.round(276, 13, 309, 46, 9, 0xf3f4f9, menuBorder)
	for _, x := range []int32{284, 291, 298} {
		c.round(x, 28, x+4, 32, 4, cardMuted, cardMuted)
	}

	status := "等待首次配对"
	dot, statusText := uint32(0x969fba), uint32(cardMuted)
	switch {
	case !w.view.network:
		status = "等待局域网"
	case w.view.online:
		status, dot, statusText = "手机在线", 0x23a38a, 0x16775f
	case w.view.bound:
		status = "手机离线 · 会自动重连"
	case w.view.paired:
		status = "手机暂离"
	}
	w.mu.Lock()
	notice := w.notice
	w.mu.Unlock()
	if notice != "" {
		status, dot, statusText = notice, indigo, indigo
	}
	c.round(17, 59, 24, 66, 7, dot, dot)
	c.label(status, 32, 50, 308, 75, 12, statusText, false)

	border := uint32(0xe7e3fc)
	if w.focus == 0 {
		border = 0x9b92ee
	}
	c.round(14, 80, 310, 150, 13, 0xf6f4ff, border)
	c.round(29, 95, 70, 136, 11, 0xeae5ff, 0xeae5ff)
	title, description := "拖入文件发送", "点击这里也可选择文件"
	if w.view.paired {
		c.line(indigo, 2, 50, 119, 50, 103)
		c.line(indigo, 2, 44, 109, 50, 103, 56, 109)
		c.line(indigo, 2, 40, 118, 40, 125, 59, 125, 59, 118)
	} else {
		title, description = "扫码配对手机", "配对一次，以后自动连接"
		c.line(indigo, 2, 45, 106, 40, 106, 40, 111)
		c.line(indigo, 2, 54, 106, 59, 106, 59, 111)
		c.line(indigo, 2, 40, 120, 40, 125, 45, 125)
		c.line(indigo, 2, 59, 120, 59, 125, 54, 125)
		c.line(indigo, 2, 44, 115, 55, 115)
	}
	c.label(title, 83, 92, 301, 119, 14, cardInk, true)
	c.label(description, 83, 118, 301, 138, 11, cardMuted, false)

	c.line(cardLine, 1, 16, 159, 309, 159)
	if w.focus == 1 {
		c.round(14, 164, 309, 196, 8, cardWhite, 0x9b92ee)
	}
	c.line(0x676c9c, 1, 18, 184, 18, 173, 24, 173, 27, 176, 35, 176, 35, 184, 18, 184)
	c.label("收件箱", 43, 165, 114, 193, 13, cardInk, true)
	seen := 0
	if w.openRoom == w.view.room {
		seen = w.seen
	}
	if count := w.view.received - seen; count > 0 {
		c.round(99, 169, 184, 189, 8, 0xe7f6ef, 0xe7f6ef)
		c.label(fmt.Sprintf("%d 个新文件", count), 106, 169, 180, 189, 11, 0x16775f, false)
	}
	c.label("打开", 264, 166, 294, 191, 12, indigo, true)
	c.line(indigo, 1, 298, 175, 302, 179, 298, 183)
}

type painter struct {
	dc uintptr
	w  *widget
}

func colorRef(rgb uint32) uintptr {
	return uintptr((rgb&0xff)<<16 | rgb&0xff00 | (rgb>>16)&0xff)
}

func brush(color uint32) uintptr {
	result, _, _ := gdi32.NewProc("CreateSolidBrush").Call(colorRef(color))
	return result
}

func selectObject(dc, object uintptr) uintptr {
	old, _, _ := gdi32.NewProc("SelectObject").Call(dc, object)
	return old
}

func removeObject(object uintptr) {
	if object != 0 {
		gdi32.NewProc("DeleteObject").Call(object)
	}
}

func (c painter) round(left, top, right, bottom, radius int32, fill, border uint32) {
	body := brush(fill)
	stroke, _, _ := gdi32.NewProc("CreatePen").Call(0, 1, colorRef(border))
	b, p := selectObject(c.dc, body), selectObject(c.dc, stroke)
	px := c.w.px
	gdi32.NewProc("RoundRect").Call(c.dc, uintptr(px(left)), uintptr(px(top)), uintptr(px(right)),
		uintptr(px(bottom)), uintptr(px(radius)), uintptr(px(radius)))
	selectObject(c.dc, p)
	selectObject(c.dc, b)
	removeObject(stroke)
	removeObject(body)
}

func (c painter) line(color uint32, width int32, coords ...int32) {
	px := c.w.px
	stroke, _, _ := gdi32.NewProc("CreatePen").Call(0, uintptr(px(width)), colorRef(color))
	old := selectObject(c.dc, stroke)
	gdi32.NewProc("MoveToEx").Call(c.dc, uintptr(px(coords[0])), uintptr(px(coords[1])), 0)
	for i := 2; i < len(coords); i += 2 {
		gdi32.NewProc("LineTo").Call(c.dc, uintptr(px(coords[i])), uintptr(px(coords[i+1])))
	}
	selectObject(c.dc, old)
	removeObject(stroke)
}

func (c painter) label(text string, left, top, right, bottom, size int32, color uint32, bold bool) {
	weight := uintptr(400)
	if bold {
		weight = 700
	}
	px := c.w.px
	font, _, _ := gdi32.NewProc("CreateFontW").Call(uintptr(uint32(-px(size))), 0, 0, 0, weight, 0, 0, 0, 1, 0, 0,
		5, 0, uintptr(unsafe.Pointer(wide("Microsoft YaHei UI"))))
	old := selectObject(c.dc, font)
	gdi32.NewProc("SetBkMode").Call(c.dc, 1)
	gdi32.NewProc("SetTextColor").Call(c.dc, colorRef(color))
	area := winRect{px(left), px(top), px(right), px(bottom)}
	user32.NewProc("DrawTextW").Call(c.dc, uintptr(unsafe.Pointer(wide(text))), 0xffffffff,
		uintptr(unsafe.Pointer(&area)), 0x8824)
	selectObject(c.dc, old)
	removeObject(font)
}
