//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	uxtheme        = syscall.NewLazyDLL("uxtheme.dll")
	dwmapi         = syscall.NewLazyDLL("dwmapi.dll")
	headerCallback = syscall.NewCallback(headerProc)
	buttonCallback uintptr
)

func init() { buttonCallback = syscall.NewCallback(buttonProc) }

type paintInfo struct {
	DC                   uintptr
	Erase                int32
	Paint                rect
	Restore, Incremental int32
	Reserved             [32]byte
}

func newUIFont(height, weight int) uintptr {
	font, _, _ := gdi32.NewProc("CreateFontW").Call(uintptr(int32(-height)), 0, 0, 0, uintptr(weight), 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(utf16("Segoe UI"))))
	return font
}

func fillColor(dc uintptr, bounds rect, color uint32) {
	brush, _, _ := gdi32.NewProc("CreateSolidBrush").Call(uintptr(color))
	user32.NewProc("FillRect").Call(dc, uintptr(unsafe.Pointer(&bounds)), brush)
	gdi32.NewProc("DeleteObject").Call(brush)
}

func roundedSurface(dc uintptr, bounds rect, fill, border uint32, radius int) {
	brush, _, _ := gdi32.NewProc("CreateSolidBrush").Call(uintptr(fill))
	pen, _, _ := gdi32.NewProc("CreatePen").Call(0, 1, uintptr(border))
	oldBrush, _, _ := gdi32.NewProc("SelectObject").Call(dc, brush)
	oldPen, _, _ := gdi32.NewProc("SelectObject").Call(dc, pen)
	gdi32.NewProc("RoundRect").Call(dc, uintptr(bounds.Left), uintptr(bounds.Top), uintptr(bounds.Right), uintptr(bounds.Bottom), uintptr(radius), uintptr(radius))
	gdi32.NewProc("SelectObject").Call(dc, oldBrush)
	gdi32.NewProc("SelectObject").Call(dc, oldPen)
	gdi32.NewProc("DeleteObject").Call(brush)
	gdi32.NewProc("DeleteObject").Call(pen)
}

func themedText(dc, font uintptr, color uint32, text string, bounds rect, flags uintptr) {
	old, _, _ := gdi32.NewProc("SelectObject").Call(dc, font)
	gdi32.NewProc("SetBkMode").Call(dc, 1)
	gdi32.NewProc("SetTextColor").Call(dc, uintptr(color))
	user32.NewProc("DrawTextW").Call(dc, uintptr(unsafe.Pointer(utf16(text))), ^uintptr(0), uintptr(unsafe.Pointer(&bounds)), 0x20|0x4|0x8000|0x800|flags)
	gdi32.NewProc("SelectObject").Call(dc, old)
}

func (ui *weatherUI) initTheme() {
	ui.headingFont = newUIFont(ui.px(28), 600)
	ui.reportFont = newUIFont(ui.px(20), 600)
	ui.labelFont = newUIFont(ui.px(13), 500)
	ui.buttonFont = newUIFont(ui.px(15), 600)
	ui.surfaceBrush, _, _ = gdi32.NewProc("CreateSolidBrush").Call(themeSurface)
	ui.backgroundBrush, _, _ = gdi32.NewProc("CreateSolidBrush").Call(themeBackground)
	for id, h := range ui.controls {
		font := ui.font
		switch id {
		case idBrand:
			font = ui.headingFont
		case idReport:
			font = ui.reportFont
		case idQueryLabel, idCityLabel, idDaysLabel, idHint, idStatus, idSource, idSubtitle, idVersion, idContext:
			font = ui.labelFont
		}
		send(h, 0x30, font, 1)
		uxtheme.NewProc("SetWindowTheme").Call(h, uintptr(unsafe.Pointer(utf16("Explorer"))), 0)
	}
	for _, entry := range []struct {
		id    uint32
		value uint32
	}{{33, 2}, {35, themeBackground}, {36, themeText}} {
		dwmapi.NewProc("DwmSetWindowAttribute").Call(ui.hwnd, uintptr(entry.id), uintptr(unsafe.Pointer(&entry.value)), 4)
	}
	send(ui.controls[idQuery], 0x1501, 0, uintptr(unsafe.Pointer(utf16(tr("design_search_placeholder")))))
	send(ui.controls[idCity], 0x153, 0, uintptr(ui.px(27)))
	send(ui.controls[idCity], 0x153, ^uintptr(0), uintptr(ui.px(31)))
	send(ui.controls[idCity], 0x160, uintptr(ui.px(850)), 0)
	header := send(ui.controls[idTable], 0x101F, 0, 0)
	comctl32.NewProc("SetWindowSubclass").Call(header, headerCallback, 1, 0)
	send(ui.controls[idTable], 0x1001, 0, themeSurface)
	send(ui.controls[idTable], 0x1026, 0, themeSurface)
	for _, id := range []int{idFetch, idCancel, idSearch, idClearQuery, idDeleteCity, idCopyMorning, idCopyAfternoon} {
		comctl32.NewProc("SetWindowSubclass").Call(ui.controls[id], buttonCallback, 2, 0)
	}
}

func (ui *weatherUI) releaseTheme() {
	for _, handle := range []uintptr{ui.headingFont, ui.reportFont, ui.labelFont, ui.buttonFont, ui.surfaceBrush, ui.backgroundBrush} {
		if handle != 0 {
			gdi32.NewProc("DeleteObject").Call(handle)
		}
	}
}

func (ui *weatherUI) paintBackground(dc uintptr) {
	var bounds rect
	user32.NewProc("GetClientRect").Call(ui.hwnd, uintptr(unsafe.Pointer(&bounds)))
	fillColor(dc, bounds, themeBackground)
	for _, card := range ui.cards {
		roundedSurface(dc, card, themeSurface, themeBorder, ui.px(20))
	}
	focus, _, _ := user32.NewProc("GetFocus").Call()
	for id, field := range ui.fields {
		border := uint32(themeBorder)
		if focus == ui.controls[id] {
			border = themeAccent
		}
		roundedSurface(dc, field, themeSurface, border, ui.px(12))
	}
}

func (ui *weatherUI) drawButton(item *drawItem) {
	disabled := item.State&4 != 0
	pressed := item.State&1 != 0
	fill, border, text := uint32(themeSurface), uint32(themeBorder), uint32(themeText)
	if item.ControlID == idFetch {
		fill, border, text = themeAccent, themeAccent, 0xFFFFFF
		if pressed {
			fill, border = themeAccentPressed, themeAccentPressed
		}
	}
	if item.ControlID == idDeleteCity {
		fill, border, text = 0xF4F1FF, 0xF4F1FF, 0x3C12BE
	}
	if ui.hoveredButton == item.Window && !disabled && !pressed {
		if item.ControlID == idFetch {
			fill, border = 0xF6823B, 0xF6823B
		} else if item.ControlID == idDeleteCity {
			fill = 0xE7E4FF
		} else {
			fill = themeHeader
			border = 0xE0CEBC
		}
	}
	if disabled {
		fill, border, text = 0xFCFAF8, themeBorder, 0xB8A394
	} else if pressed && item.ControlID != idFetch {
		fill = 0xF5EDE6
	}
	bounds := item.Bounds
	bounds.Right--
	bounds.Bottom--
	roundedSurface(item.DC, bounds, fill, border, ui.px(12))
	if item.State&0x10 != 0 && !disabled {
		bounds.Left += 2
		bounds.Top += 2
		bounds.Right -= 2
		bounds.Bottom -= 2
		roundedSurface(item.DC, bounds, fill, themeAccent, ui.px(10))
	}
	themedText(item.DC, ui.buttonFont, text, textOf(item.Window), bounds, 1)
}

func buttonProc(hwnd uintptr, msg uint32, w uintptr, l unsafe.Pointer, _, _ uintptr) uintptr {
	ui := activeUI
	if ui != nil {
		switch msg {
		case 0x200: // WM_MOUSEMOVE
			if ui.hoveredButton != hwnd {
				previous := ui.hoveredButton
				ui.hoveredButton = hwnd
				if previous != 0 {
					user32.NewProc("InvalidateRect").Call(previous, 0, 0)
				}
				user32.NewProc("InvalidateRect").Call(hwnd, 0, 0)
				tracking := struct {
					Size, Flags uint32
					Window      uintptr
					HoverTime   uint32
				}{Flags: 2, Window: hwnd}
				tracking.Size = uint32(unsafe.Sizeof(tracking))
				user32.NewProc("TrackMouseEvent").Call(uintptr(unsafe.Pointer(&tracking)))
			}
		case 0x2A3: // WM_MOUSELEAVE
			if ui.hoveredButton == hwnd {
				ui.hoveredButton = 0
				user32.NewProc("InvalidateRect").Call(hwnd, 0, 0)
			}
		case 0x82:
			comctl32.NewProc("RemoveWindowSubclass").Call(hwnd, buttonCallback, 2)
		}
	}
	r, _, _ := comctl32.NewProc("DefSubclassProc").Call(hwnd, uintptr(msg), w, uintptr(l))
	return r
}

func headerProc(hwnd uintptr, msg uint32, w uintptr, l unsafe.Pointer, _, _ uintptr) uintptr {
	ui := activeUI
	if ui != nil {
		if msg == 0x14 {
			return 1
		}
		if msg == 0xF {
			var paint paintInfo
			dc, _, _ := user32.NewProc("BeginPaint").Call(hwnd, uintptr(unsafe.Pointer(&paint)))
			var bounds rect
			user32.NewProc("GetClientRect").Call(hwnd, uintptr(unsafe.Pointer(&bounds)))
			fillColor(dc, bounds, themeHeader)
			headers := []string{tr("gui_windows_17"), tr("gui_windows_18"), tr("gui_windows_19"), tr("gui_windows_20"), tr("gui_windows_21"), tr("gui_windows_19"), tr("gui_windows_22")}
			for i, text := range headers {
				var cell rect
				send(hwnd, 0x1207, uintptr(i), uintptr(unsafe.Pointer(&cell)))
				cell.Left += int32(ui.px(10))
				cell.Right -= int32(ui.px(6))
				themedText(dc, ui.labelFont, themeMuted, text, cell, 0)
			}
			fillColor(dc, rect{0, bounds.Bottom - 1, bounds.Right, bounds.Bottom}, themeBorder)
			user32.NewProc("EndPaint").Call(hwnd, uintptr(unsafe.Pointer(&paint)))
			return 0
		}
	}
	r, _, _ := comctl32.NewProc("DefSubclassProc").Call(hwnd, uintptr(msg), w, uintptr(l))
	return r
}
