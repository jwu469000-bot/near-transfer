package main

import (
	"fmt"
	"unsafe"
)

func (p *setupPanel) paint(dc uintptr) {
	if dc == 0 {
		return
	}
	c := painter{dc, p.dpi}
	c.fill(winRect{0, 0, setupWidth, setupHeight}, 0xf7f8fc)
	c.round(17, 13, 451, 52, 12, 0xedeff7, 0xedeff7)
	for tab, text := range []string{"设备配对", "安装安卓端", "异地连接"} {
		area := setupButtons[tab]
		border := uint32(cardLine)
		if p.focus == tab {
			border = 0x9b92ee
		}
		color := uint32(cardMuted)
		if tab == p.tab {
			c.round(area.left, area.top, area.right, area.bottom, 9, cardWhite, border)
			color = indigo
		} else if p.focus == tab {
			c.round(area.left, area.top, area.right, area.bottom, 9, 0xedeff7, border)
		}
		c.drawText(text, area, 13, color, true, 1)
	}
	if p.tab == remoteTab {
		p.paintRemote(c)
		return
	}

	title, detail := "绑定这台安卓手机", "在安卓近传里点「打开相机」，扫描配对二维码。"
	if p.tab == installTab {
		title, detail = "安装安卓近传", "用手机相机扫码下载，也可以把 APK 保存到电脑。"
	} else if p.state.paired {
		title, detail = "已绑定这台手机", "以后在同一局域网打开两端近传即可自动连接。"
	}
	c.label(title, 20, 65, 448, 99, 22, cardInk, true)
	c.label(detail, 20, 100, 448, 123, 12, cardMuted, false)

	tile := winRect{20, 133, 202, 315}
	c.round(tile.left, tile.top, tile.right, tile.bottom, 14, cardWhite, cardLine)
	switch {
	case len(p.qr) > 0:
		c.drawQR(tile, p.qr)
	case p.tab == pairTab && p.state.paired:
		c.round(84, 181, 138, 235, 16, 0xe7f6ef, 0xe7f6ef)
		c.line(0x177c63, 3, 99, 207, 107, 215, 123, 199)
		status := "等待手机连接"
		if p.state.online {
			status = "手机在线"
		}
		c.drawText(status, winRect{27, 246, 195, 275}, 13, cardMuted, true, 1)
	case p.tab == installTab && len(installerAPK) == 0:
		c.drawText("请使用独立安装包", tile, 13, cardMuted, false, 1)
	default:
		c.drawText("等待局域网", tile, 13, cardMuted, false, 1)
	}

	address := p.state.address
	if address == "" {
		address = "连接 Wi‑Fi 后显示"
	}
	if p.tab == installTab {
		c.label("Android 8.0 及以上", 221, 141, 447, 170, 15, cardInk, true)
		if len(installerAPK) > 0 {
			c.label(fmt.Sprintf("近传 %s  ·  %d KB", androidVersion, (len(installerAPK)+512)/1024), 221, 175, 447, 198, 12, cardMuted, false)
		}
		c.label("手机下载后，打开 APK 安装。", 221, 207, 447, 231, 12, cardMuted, false)
		if p.enabled(setupPrimary) {
			p.button(c, setupPrimary, "保存 APK", true)
		}
	} else {
		section := "手动连接"
		if p.state.paired {
			section = "电脑地址"
		}
		c.label(section, 221, 140, 447, 166, 15, cardInk, true)
		c.round(221, 173, 448, 205, 8, 0xeff0fa, 0xeff0fa)
		c.label(address, 231, 173, 440, 205, 13, cardInk, true)
		if code := p.state.code; len(code) == 6 {
			c.label("配对码", 221, 209, 280, 252, 12, cardMuted, false)
			c.label(code[:3]+" "+code[3:], 281, 206, 448, 253, 23, indigo, true)
		}
		if p.enabled(setupPrimary) {
			p.button(c, setupPrimary, "复制地址", false)
		}
	}
	if p.enabled(setupNetwork) {
		p.button(c, setupNetwork, "切换地址", false)
	}
	if p.notice != "" {
		c.label(p.notice, 20, 315, 448, 337, 11, 0x16775f, false)
	}
}

func (p *setupPanel) paintRemote(c painter) {
	c.label("在外面连接这台电脑", 20, 65, 448, 99, 22, cardInk, true)
	c.label("让电脑和手机连接到同一个 Tailscale 私有网络。", 20, 100, 448, 123, 12, cardMuted, false)

	c.round(20, 133, 448, 245, 14, cardWhite, cardLine)
	c.label("电脑的异地地址", 34, 145, 432, 171, 14, cardInk, true)
	address := p.state.address
	color := uint32(indigo)
	if address == "" {
		color = cardMuted
		switch {
		case !p.state.bound:
			address = "先在「设备配对」页绑定安卓手机"
		case p.state.remoteFailed:
			address = "无法启用异地连接，请检查 TCP 47332 端口"
		default:
			address = "电脑连接 Tailscale 后自动显示"
		}
	}
	c.label(address, 34, 176, 432, 204, 15, color, true)
	c.label("在手机近传的「连接设置 → 异地连接」里填入这个地址。", 34, 209, 432, 235, 12, cardMuted, false)

	c.label("异地使用时，电脑需要保持唤醒。", 20, 263, 216, 299, 12, cardMuted, false)
	if p.enabled(setupPrimary) {
		p.button(c, setupPrimary, "复制地址", false)
	}
	if p.notice != "" {
		c.label(p.notice, 20, 315, 448, 337, 11, 0x16775f, false)
	}
}

func (p *setupPanel) button(c painter, action int, text string, primary bool) {
	rect := setupButtons[action]
	fill, border, ink := uint32(cardWhite), uint32(cardLine), uint32(indigo)
	if primary {
		fill, border, ink = indigo, indigo, cardWhite
	}
	if p.focus == action {
		border = 0xa59cf7
	}
	c.round(rect.left, rect.top, rect.right, rect.bottom, 8, fill, border)
	c.drawText(text, rect, 13, ink, true, 1)
}

func (c painter) drawQR(tile winRect, matrix [][]bool) {
	width := int32(len(matrix))
	if width == 0 {
		return
	}
	tileSize := tile.size()
	module := max(1, (c.px(min(tileSize.x, tileSize.y))-c.px(12))/width)
	origin := winPoint{c.px(tile.center().x) - module*width/2, c.px(tile.center().y) - module*width/2}
	ink := brush(0x141c31)
	defer removeObject(ink)
	fill := user32.NewProc("FillRect")
	for y, row := range matrix {
		for x := 0; x < len(row); {
			if !row[x] {
				x++
				continue
			}
			start := x
			for x < len(row) && row[x] {
				x++
			}
			area := winRect{
				origin.x + int32(start)*module, origin.y + int32(y)*module,
				origin.x + int32(x)*module, origin.y + int32(y+1)*module,
			}
			fill.Call(c.dc, uintptr(unsafe.Pointer(&area)), ink)
		}
	}
}
