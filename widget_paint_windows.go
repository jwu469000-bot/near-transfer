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
	paintBuffered(dc, w.pixelSize(), w.paint)
}

func paintBuffered(dc uintptr, size winPoint, paint func(uintptr)) {
	if dc == 0 {
		return
	}
	mem, _, _ := gdi32.NewProc("CreateCompatibleDC").Call(dc)
	bitmap, _, _ := gdi32.NewProc("CreateCompatibleBitmap").Call(dc, uintptr(size.x), uintptr(size.y))
	if mem == 0 || bitmap == 0 {
		if mem != 0 {
			gdi32.NewProc("DeleteDC").Call(mem)
		}
		removeObject(bitmap)
		paint(dc)
		return
	}
	saved := selectObject(mem, bitmap)
	paint(mem)
	gdi32.NewProc("BitBlt").Call(dc, 0, 0, uintptr(size.x), uintptr(size.y), mem, 0, 0, 0x00cc0020)
	selectObject(mem, saved)
	removeObject(bitmap)
	gdi32.NewProc("DeleteDC").Call(mem)
}

func (w *widget) paint(dc uintptr) {
	if dc == 0 {
		return
	}
	c := painter{dc, w.dpi}
	if !w.expanded {
		w.paintBall(c)
		return
	}
	c.fill(winRect{0, 0, widgetWidth, widgetHeight}, cardWhite)
	c.round(0, 0, widgetWidth, widgetHeight, 19, cardWhite, cardLine)
	c.round(15, 13, 48, 46, 10, indigo, indigo)
	c.line(0xffffff, 2, 24, 26, 38, 26)
	c.line(0xffffff, 2, 34, 22, 38, 26, 34, 30)
	c.line(0xdcd9ff, 2, 39, 33, 25, 33)
	c.line(0xdcd9ff, 2, 29, 29, 25, 33, 29, 37)
	c.label("近传", 59, 14, 240, 44, 18, cardInk, true)

	menuBorder := uint32(0xf1f2f8)
	if w.focus == menuAction {
		menuBorder = 0x9b92ee
	}
	c.round(276, 13, 309, 46, 9, 0xf3f4f9, menuBorder)
	for _, x := range []int32{284, 291, 298} {
		c.round(x, 28, x+4, 32, 4, cardMuted, cardMuted)
	}
	collapseBorder := uint32(0xf1f2f8)
	if w.focus == collapseAction {
		collapseBorder = 0x9b92ee
	}
	c.round(235, 13, 268, 46, 9, 0xf3f4f9, collapseBorder)
	c.line(cardMuted, 2, 245, 30, 258, 30)

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
	switch {
	case w.view.batch != nil && w.view.batch.ctx.Err() != nil:
		notice = "正在取消传输…"
	case w.view.batch == nil && w.view.request != nil:
		notice = w.view.request.caption
	}
	if notice != "" {
		status, dot, statusText = notice, indigo, indigo
	}
	statusRight := int32(308)
	if w.canCancel() {
		statusRight = cancelBounds.left - 7
		border := uint32(0xf2d9d6)
		if w.focus == cancelAction {
			border = 0xc16c65
		}
		c.round(cancelBounds.left, cancelBounds.top, cancelBounds.right, cancelBounds.bottom, 8, 0xfff0ee, border)
		c.drawText("取消", cancelBounds, 12, 0xa0443d, true, 1)
	}
	c.round(17, 59, 24, 66, 7, dot, dot)
	c.label(status, 32, 50, statusRight, 75, 12, statusText, false)

	border := uint32(0xe7e3fc)
	if w.focus == sendAction {
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
	c.line(cardLine, 1, 164, 169, 164, 190)
	if w.focus == inboxAction {
		c.round(14, 164, 157, 196, 8, cardWhite, 0x9b92ee)
	}
	c.line(0x676c9c, 1, 18, 184, 18, 173, 24, 173, 27, 176, 35, 176, 35, 184, 18, 184)
	c.label("收件箱", 43, 165, 109, 193, 13, cardInk, true)
	c.badge(109, w.unreadFiles())
	if w.focus == textAction {
		c.round(171, 164, 310, 196, 8, cardWhite, 0x9b92ee)
	}
	c.line(0x676c9c, 1, 180, 173, 196, 173, 196, 183, 188, 183, 184, 187, 184, 183, 180, 183, 180, 173)
	c.line(0x676c9c, 1, 184, 177, 192, 177)
	c.label("传文字", 204, 165, 264, 193, 13, indigo, true)
	c.badge(264, w.unreadTexts())
}

func (w *widget) unreadFiles() int {
	if w.openRoom == w.view.room {
		return w.view.received - w.seen
	}
	return w.view.received
}

func (w *widget) unreadTexts() int {
	if w.textRoom == w.view.room {
		return w.view.receivedTexts - w.seenTexts
	}
	return w.view.receivedTexts
}

func (w *widget) paintBall(c painter) {
	c.fill(winRect{0, 0, ballDiameter, ballDiameter}, cardWhite)
	c.round(0, 0, 42, 42, 42, 0x4d40cb, 0x4336be)
	c.round(2, 2, 40, 40, 38, 0x6050e3, 0x7868f1)
	c.line(0xffffff, 2, 12, 16, 29, 16)
	c.line(0xffffff, 2, 25, 12, 29, 16, 25, 20)
	c.line(0xe9e4ff, 2, 30, 25, 13, 25)
	c.line(0xe9e4ff, 2, 17, 21, 13, 25, 17, 29)
	if w.unreadFiles()+w.unreadTexts() > 0 {
		c.round(29, 28, 37, 36, 8, 0xa2efd1, cardWhite)
	}
}

type painter struct {
	dc  uintptr
	dpi int32
}

func (c painter) badge(x int32, count int) {
	if count <= 0 {
		return
	}
	text := fmt.Sprint(count)
	if count > 99 {
		text = "99+"
	}
	bounds := winRect{x, 169, x + 32, 190}
	c.round(bounds.left, bounds.top, bounds.right, bounds.bottom, 9, 0xe7f6ef, 0xe7f6ef)
	c.drawText(text, bounds, 11, 0x16775f, true, 1)
}

func (c painter) px(dip int32) int32 { return scaleDip(dip, c.dpi) }

func (c painter) fill(area winRect, color uint32) {
	rect := winRect{c.px(area.left), c.px(area.top), c.px(area.right), c.px(area.bottom)}
	back := brush(color)
	user32.NewProc("FillRect").Call(c.dc, uintptr(unsafe.Pointer(&rect)), back)
	removeObject(back)
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
	px := c.px
	gdi32.NewProc("RoundRect").Call(c.dc, uintptr(px(left)), uintptr(px(top)), uintptr(px(right)),
		uintptr(px(bottom)), uintptr(px(radius)), uintptr(px(radius)))
	selectObject(c.dc, p)
	selectObject(c.dc, b)
	removeObject(stroke)
	removeObject(body)
}

func (c painter) line(color uint32, width int32, coords ...int32) {
	px := c.px
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
	c.drawText(text, winRect{left, top, right, bottom}, size, color, bold, 0)
}

func (c painter) drawText(text string, bounds winRect, size int32, color uint32, bold bool, align uintptr) {
	px := c.px
	font := fontFor(c.dpi, size, bold)
	old := selectObject(c.dc, font)
	gdi32.NewProc("SetBkMode").Call(c.dc, 1)
	gdi32.NewProc("SetTextColor").Call(c.dc, colorRef(color))
	area := winRect{px(bounds.left), px(bounds.top), px(bounds.right), px(bounds.bottom)}
	user32.NewProc("DrawTextW").Call(c.dc, uintptr(unsafe.Pointer(wide(text))), 0xffffffff,
		uintptr(unsafe.Pointer(&area)), 0x8824|align)
	selectObject(c.dc, old)
	removeObject(font)
}

func fontFor(dpi, size int32, bold bool) uintptr {
	weight := uintptr(400)
	if bold {
		weight = 700
	}
	font, _, _ := gdi32.NewProc("CreateFontW").Call(uintptr(uint32(-scaleDip(size, dpi))), 0, 0, 0, weight, 0, 0, 0, 1, 0, 0,
		5, 0, uintptr(unsafe.Pointer(wide("Microsoft YaHei UI"))))
	return font
}
