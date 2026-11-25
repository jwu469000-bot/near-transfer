package main

import (
	"encoding/json"
	"os"
	"syscall"
	"unsafe"
)

var copySystemMemory = syscall.NewLazyDLL("ntdll.dll").NewProc("RtlMoveMemory")

type windowPosition struct {
	window, after uintptr
	x, y          int32
	width, height int32
	flags         uint32
}

type ballDrag struct {
	active, moved bool
	start, last   winPoint
	grab          winPoint
	dpi           int32
}

func (r winRect) origin() winPoint { return winPoint{r.left, r.top} }
func (r winRect) size() winPoint   { return winPoint{r.right - r.left, r.bottom - r.top} }
func (r winRect) center() winPoint {
	size := r.size()
	return winPoint{r.left + size.x/2, r.top + size.y/2}
}

func (r winRect) contains(point winPoint) bool {
	return point.x >= r.left && point.x < r.right && point.y >= r.top && point.y < r.bottom
}

func (p winPoint) add(other winPoint) winPoint { return winPoint{p.x + other.x, p.y + other.y} }
func (p winPoint) sub(other winPoint) winPoint { return winPoint{p.x - other.x, p.y - other.y} }

func scaleDip(dip, dpi int32) int32 { return (dip*dpi + 48) / 96 }

func fitDPI(dpi int32, dimensions winPoint, area winRect) int32 {
	size := area.size()
	return max(1, min(dpi, size.x*96/dimensions.x, size.y*96/dimensions.y))
}

func fitPoint(point, windowSize winPoint, area winRect) winPoint {
	return winPoint{
		max(area.left, min(point.x, area.right-windowSize.x)),
		max(area.top, min(point.y, area.bottom-windowSize.y)),
	}
}

func windowsArea(anchor winPoint) winRect {
	var area winRect
	packed := uintptr(uint64(uint32(anchor.x)) | uint64(uint32(anchor.y))<<32)
	monitor, _, _ := user32.NewProc("MonitorFromPoint").Call(packed, 2)
	info := monitorInfo{size: uint32(unsafe.Sizeof(monitorInfo{}))}
	if ok, _, _ := user32.NewProc("GetMonitorInfoW").Call(monitor, uintptr(unsafe.Pointer(&info))); ok != 0 {
		return info.work
	}
	user32.NewProc("SystemParametersInfoW").Call(0x0030, 0, uintptr(unsafe.Pointer(&area)), 0)
	return area
}

func windowBounds(hwnd uintptr) (winRect, bool) {
	var rect winRect
	ok, _, _ := user32.NewProc("GetWindowRect").Call(hwnd, uintptr(unsafe.Pointer(&rect)))
	return rect, ok != 0
}

func windowDPI(hwnd uintptr) int32 {
	getDPI := user32.NewProc("GetDpiForWindow")
	if getDPI.Find() == nil {
		if dpi, _, _ := getDPI.Call(hwnd); dpi > 0 {
			return int32(dpi)
		}
	}
	return 96
}

func clientToScreen(hwnd uintptr, point winPoint) winPoint {
	user32.NewProc("ClientToScreen").Call(hwnd, uintptr(unsafe.Pointer(&point)))
	return point
}

func (w *widget) dimensions() winPoint {
	if w.expanded {
		return winPoint{widgetWidth, widgetHeight}
	}
	return winPoint{ballDiameter, ballDiameter}
}

func (w *widget) pixelSize() winPoint {
	size := w.dimensions()
	return winPoint{w.px(size.x), w.px(size.y)}
}

func (w *widget) ballSize(area winRect) winPoint {
	dpi := fitDPI(w.screenDPI, winPoint{ballDiameter, ballDiameter}, area)
	diameter := scaleDip(ballDiameter, dpi)
	return winPoint{diameter, diameter}
}

func (w *widget) loadPreferences() {
	if raw, err := os.ReadFile(w.prefsFile()); err == nil {
		_ = json.Unmarshal(raw, &w.prefs)
	}
}

func (w *widget) place() {
	w.screenDPI = windowDPI(w.hwnd)
	w.anchor = winPoint{w.prefs.X, w.prefs.Y}
	area := windowsArea(w.anchor)
	ball := w.ballSize(area)
	if !w.prefs.Moved {
		margin := scaleDip(20, w.screenDPI)
		w.anchor = winPoint{area.right - ball.x - margin, area.bottom - ball.y - margin}
	} else if !w.prefs.BallAnchor {
		width, height := scaleDip(widgetWidth, w.screenDPI), scaleDip(widgetHeight, w.screenDPI)
		// A saved panel near the right or bottom edge collapses toward that edge.
		if w.anchor.x+width/2 > area.center().x {
			w.anchor.x += width - ball.x
		}
		if w.anchor.y+height/2 > area.center().y {
			w.anchor.y += height - ball.y
		}
	}
	w.applyLayout(area)
}

func (w *widget) applyLayout(area winRect) {
	w.dpi = fitDPI(w.screenDPI, w.dimensions(), area)
	size, ball := w.pixelSize(), w.ballSize(area)
	w.anchor = fitPoint(w.anchor, ball, area)
	origin := w.anchor
	if w.expanded {
		if origin.x+size.x > area.right {
			origin.x += ball.x - size.x
		}
		if origin.y+size.y > area.bottom {
			origin.y += ball.y - size.y
		}
	}
	origin = fitPoint(origin, size, area)
	w.anchorOffset = w.anchor.sub(origin)
	user32.NewProc("SetWindowPos").Call(w.hwnd, 0, uintptr(origin.x), uintptr(origin.y), uintptr(size.x), uintptr(size.y), 0x14)
	w.roundWindow()
	w.redraw()
}

func (w *widget) roundWindow() {
	size := w.pixelSize()
	setWindowRegion := user32.NewProc("SetWindowRgn")
	if !w.expanded {
		preference := uint32(1)
		dwmapi.NewProc("DwmSetWindowAttribute").Call(w.hwnd, 33, uintptr(unsafe.Pointer(&preference)), 4)
		region, _, _ := gdi32.NewProc("CreateEllipticRgn").Call(0, 0, uintptr(size.x), uintptr(size.y))
		if ok, _, _ := setWindowRegion.Call(w.hwnd, region, 1); ok == 0 {
			removeObject(region)
		}
		return
	}

	setWindowRegion.Call(w.hwnd, 0, 1)
	preference := uint32(2)
	result, _, _ := dwmapi.NewProc("DwmSetWindowAttribute").Call(w.hwnd, 33, uintptr(unsafe.Pointer(&preference)), 4)
	if result != 0 {
		region, _, _ := gdi32.NewProc("CreateRoundRectRgn").Call(
			0, 0, uintptr(size.x+1), uintptr(size.y+1), uintptr(w.px(21)), uintptr(w.px(21)))
		if ok, _, _ := setWindowRegion.Call(w.hwnd, region, 1); ok == 0 {
			removeObject(region)
		}
	}
}

func (w *widget) updateAnchor() winRect {
	if bounds, ok := windowBounds(w.hwnd); ok {
		area := windowsArea(bounds.center())
		w.anchor = fitPoint(bounds.origin().add(w.anchorOffset), w.ballSize(area), area)
		return area
	}
	return windowsArea(w.anchor)
}

func (w *widget) setExpanded(expanded bool) {
	if w.expanded == expanded {
		return
	}
	area := w.updateAnchor()
	w.expanded, w.focus = expanded, -1
	title := "近传 · 单击展开"
	if expanded {
		title = "近传 · 文件互传"
	}
	user32.NewProc("SetWindowTextW").Call(w.hwnd, uintptr(unsafe.Pointer(wide(title))))
	w.applyLayout(area)
}

func (w *widget) savePosition() {
	w.updateAnchor()
	w.prefs.X, w.prefs.Y, w.prefs.Moved, w.prefs.BallAnchor = w.anchor.x, w.anchor.y, true, true
	raw, _ := json.Marshal(w.prefs)
	_ = writeAtomic(w.prefsFile(), raw)
}

func (w *widget) close() {
	w.savePosition()
	user32.NewProc("DestroyWindow").Call(w.hwnd)
}

func (w *widget) constrainPosition(parameter uintptr) {
	if parameter == 0 {
		return
	}
	var proposed windowPosition
	copySystemMemory.Call(uintptr(unsafe.Pointer(&proposed)), parameter, unsafe.Sizeof(proposed))
	if proposed.flags&3 == 3 {
		return
	}
	origin := winPoint{proposed.x, proposed.y}
	size := winPoint{proposed.width, proposed.height}
	if proposed.flags&3 != 0 {
		if bounds, ok := windowBounds(w.hwnd); ok {
			if proposed.flags&1 != 0 { // SWP_NOSIZE
				size = bounds.size()
			}
			if proposed.flags&2 != 0 { // SWP_NOMOVE
				origin = bounds.origin()
			}
		}
	}
	if size.x <= 0 || size.y <= 0 {
		return
	}
	area := windowsArea(origin.add(winPoint{size.x / 2, size.y / 2}))
	bounded := fitPoint(origin, size, area)
	if bounded == origin {
		return
	}
	proposed.x, proposed.y = bounded.x, bounded.y
	proposed.flags &^= 2
	copySystemMemory.Call(parameter, uintptr(unsafe.Pointer(&proposed)), unsafe.Sizeof(proposed))
}

func (w *widget) changeDPI(dpi int32, parameter uintptr) {
	previous := w.screenDPI
	w.screenDPI = max(1, dpi)
	var suggested winRect
	if parameter == 0 {
		w.keepOnScreen()
		return
	}
	copySystemMemory.Call(uintptr(unsafe.Pointer(&suggested)), parameter, unsafe.Sizeof(suggested))
	if w.drag.active && w.drag.moved {
		w.anchor = w.drag.last.sub(w.dragOffset())
		w.applyLayout(windowsArea(w.drag.last))
		return
	}
	w.anchor = suggested.origin().add(winPoint{w.anchorOffset.x * dpi / previous, w.anchorOffset.y * dpi / previous})
	w.applyLayout(windowsArea(suggested.center()))
}

func (w *widget) keepOnScreen() {
	w.applyLayout(w.updateAnchor())
}

func (w *widget) beginBallDrag(point winPoint) {
	screen := clientToScreen(w.hwnd, point)
	w.drag = ballDrag{active: true, start: screen, last: screen, grab: point, dpi: w.screenDPI}
	user32.NewProc("SetCapture").Call(w.hwnd)
}

func (w *widget) dragOffset() winPoint {
	return winPoint{w.drag.grab.x * w.screenDPI / w.drag.dpi, w.drag.grab.y * w.screenDPI / w.drag.dpi}
}

func (w *widget) moveBall(point winPoint) {
	screen := clientToScreen(w.hwnd, point)
	if !w.drag.moved {
		delta := screen.sub(w.drag.start)
		tolerance := w.px(4)
		if delta.x > -tolerance && delta.x < tolerance && delta.y > -tolerance && delta.y < tolerance {
			return
		}
		w.drag.moved = true
	}
	w.drag.last = screen
	w.anchor = screen.sub(w.dragOffset())
	w.applyLayout(windowsArea(screen))
}

func (w *widget) endBallDrag(point winPoint) {
	if !w.drag.active {
		return
	}
	w.moveBall(point)
	moved := w.drag.moved
	w.drag = ballDrag{}
	user32.NewProc("ReleaseCapture").Call()
	if moved {
		w.savePosition()
	} else if w.action(point.x, point.y) == expandAction {
		w.setExpanded(true)
	}
}

func (w *widget) cancelBallDrag() {
	moved := w.drag.moved
	w.drag = ballDrag{}
	if moved {
		w.savePosition()
	}
}
