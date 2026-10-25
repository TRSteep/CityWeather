//go:build windows

package main

import (
	"math"
	"strconv"
	"time"
	"unsafe"
)

type drawItem struct {
	ControlType, ControlID, ItemID, Action, State uint32
	Window, DC                                    uintptr
	Bounds                                        rect
	Data                                          uintptr
}

// Draw into a memory bitmap so resizing or reloading never exposes half a chart.
func (ui *weatherUI) drawChart(item *drawItem) {
	w, h := int(item.Bounds.Right-item.Bounds.Left), int(item.Bounds.Bottom-item.Bounds.Top)
	if w < 1 || h < 1 {
		return
	}
	dc, _, _ := gdi32.NewProc("CreateCompatibleDC").Call(item.DC)
	bitmap, _, _ := gdi32.NewProc("CreateCompatibleBitmap").Call(item.DC, uintptr(w), uintptr(h))
	if dc == 0 || bitmap == 0 {
		if bitmap != 0 {
			gdi32.NewProc("DeleteObject").Call(bitmap)
		}
		if dc != 0 {
			gdi32.NewProc("DeleteDC").Call(dc)
		}
		return
	}
	old, _, _ := gdi32.NewProc("SelectObject").Call(dc, bitmap)
	defer func() {
		gdi32.NewProc("SelectObject").Call(dc, old)
		gdi32.NewProc("DeleteObject").Call(bitmap)
		gdi32.NewProc("DeleteDC").Call(dc)
	}()
	ui.paintChart(dc, w, h)
	gdi32.NewProc("BitBlt").Call(item.DC, uintptr(item.Bounds.Left), uintptr(item.Bounds.Top), uintptr(w), uintptr(h), dc, 0, 0, 0x00CC0020)
}

func (ui *weatherUI) paintChart(dc uintptr, w, h int) {
	area := rect{Right: int32(w), Bottom: int32(h)}
	white, _, _ := gdi32.NewProc("CreateSolidBrush").Call(0xFFFFFF)
	user32.NewProc("FillRect").Call(dc, uintptr(unsafe.Pointer(&area)), white)
	gdi32.NewProc("DeleteObject").Call(white)
	gdi32.NewProc("SelectObject").Call(dc, ui.font)
	gdi32.NewProc("SetBkMode").Call(dc, 1)
	text := func(value string, x, y, width, height int, align uintptr) {
		bounds := rect{int32(x), int32(y), int32(x + width), int32(y + height)}
		gdi32.NewProc("SetTextColor").Call(dc, 0x4F4438)
		user32.NewProc("DrawTextW").Call(dc, uintptr(unsafe.Pointer(utf16(value))), ^uintptr(0), uintptr(unsafe.Pointer(&bounds)), 0x20|0x4|0x8000|0x800|align)
	}
	line := func(x1, y1, x2, y2 int, color uint32, width int) {
		pen, _, _ := gdi32.NewProc("CreatePen").Call(0, uintptr(width), uintptr(color))
		old, _, _ := gdi32.NewProc("SelectObject").Call(dc, pen)
		gdi32.NewProc("MoveToEx").Call(dc, uintptr(x1), uintptr(y1), 0)
		gdi32.NewProc("LineTo").Call(dc, uintptr(x2), uintptr(y2))
		gdi32.NewProc("SelectObject").Call(dc, old)
		gdi32.NewProc("DeleteObject").Call(pen)
	}
	text(tr("chart_title"), ui.px(14), ui.px(7), w-ui.px(290), ui.px(27), 0)
	legendX := w - ui.px(252)
	for i, entry := range []struct {
		key   string
		color uint32
	}{{"chart_morning", chartMorningColor}, {"chart_afternoon", chartAfternoonColor}} {
		x := legendX + ui.px(i*122)
		line(x, ui.px(21), x+ui.px(26), ui.px(21), entry.color, ui.px(3))
		text(tr(entry.key), x+ui.px(34), ui.px(7), ui.px(80), ui.px(27), 0)
	}
	morning, afternoon := temperatureSegments(ui.rows, false), temperatureSegments(ui.rows, true)
	low, high, step, available := chartScale(morning, afternoon)
	if !available {
		key := "chart_no_data"
		if len(ui.rows) == 0 {
			key = "chart_empty"
		}
		text(tr(key), ui.px(16), ui.px(48), w-ui.px(32), h-ui.px(64), 1)
		return
	}
	left, right, top, bottom := ui.px(65), w-ui.px(46), ui.px(47), h-ui.px(35)
	if right <= left || bottom <= top {
		return
	}
	xAt := func(day int) int {
		if len(ui.rows) < 2 {
			return (left + right) / 2
		}
		return left + int(math.Round(float64(day)*float64(right-left)/float64(len(ui.rows)-1)))
	}
	yAt := func(value float64) int { return bottom - int(math.Round((value-low)*float64(bottom-top)/(high-low))) }
	for value := low; value <= high+step*0.001; value += step {
		if math.Abs(value) < step*0.001 {
			value = 0
		}
		y := yAt(value)
		color := uint32(0xEEE9E4)
		if value == 0 {
			color = 0xC6B9AD
		}
		line(left, y, right, y, color, 1)
		label := strconv.FormatFloat(value, 'f', -1, 64)
		// Axis ticks are rounded to their step to avoid floating point tails.
		precision := 0
		if step < 1 {
			precision = int(math.Ceil(-math.Log10(step)))
		}
		if step == 2.5 {
			precision = 1
		}
		label = strconv.FormatFloat(value, 'f', precision, 64)
		text(label, ui.px(4), y-ui.px(11), left-ui.px(13), ui.px(22), 2)
	}
	line(left, top, left, bottom, 0xC6B9AD, 1)
	line(left, bottom, right, bottom, 0xC6B9AD, 1)
	limit := (right - left) / ui.px(90)
	if limit < 2 {
		limit = 2
	}
	if limit > 12 {
		limit = 12
	}
	for _, day := range chartLabelDays(len(ui.rows), limit) {
		x := xAt(day)
		date, _ := time.Parse(time.DateOnly, ui.rows[day].Date)
		line(x, bottom, x, bottom+ui.px(4), 0xC6B9AD, 1)
		text(date.Format(tr("chart_date_format")), x-ui.px(42), bottom+ui.px(6), ui.px(84), ui.px(24), 1)
	}
	for i, segments := range [][][]temperaturePoint{morning, afternoon} {
		color := uint32(chartMorningColor)
		if i == 1 {
			color = chartAfternoonColor
		}
		for _, segment := range segments {
			for j, point := range segment {
				x, y := xAt(point.Day), yAt(point.Value)
				if j > 0 {
					previous := segment[j-1]
					line(xAt(previous.Day), yAt(previous.Value), x, y, color, ui.px(2))
				}
				if len(ui.rows) <= 60 || len(segment) == 1 {
					brush, _, _ := gdi32.NewProc("CreateSolidBrush").Call(uintptr(color))
					pen, _, _ := gdi32.NewProc("CreatePen").Call(0, 1, uintptr(color))
					oldBrush, _, _ := gdi32.NewProc("SelectObject").Call(dc, brush)
					oldPen, _, _ := gdi32.NewProc("SelectObject").Call(dc, pen)
					radius := ui.px(3)
					gdi32.NewProc("Ellipse").Call(dc, uintptr(x-radius), uintptr(y-radius), uintptr(x+radius+1), uintptr(y+radius+1))
					gdi32.NewProc("SelectObject").Call(dc, oldPen)
					gdi32.NewProc("SelectObject").Call(dc, oldBrush)
					gdi32.NewProc("DeleteObject").Call(pen)
					gdi32.NewProc("DeleteObject").Call(brush)
				}
			}
		}
	}
}
