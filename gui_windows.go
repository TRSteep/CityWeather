//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// The native Windows interface uses only the Go standard library.
var (
	user32         = syscall.NewLazyDLL("user32.dll")
	kernel32       = syscall.NewLazyDLL("kernel32.dll")
	gdi32          = syscall.NewLazyDLL("gdi32.dll")
	comctl32       = syscall.NewLazyDLL("comctl32.dll")
	createWindow   = user32.NewProc("CreateWindowExW")
	sendMessage    = user32.NewProc("SendMessageW")
	postMessage    = user32.NewProc("PostMessageW")
	setText        = user32.NewProc("SetWindowTextW")
	defWindowProc  = user32.NewProc("DefWindowProcW")
	windowCallback = syscall.NewCallback(windowProc)
	activeUI       *weatherUI
)

type point struct{ X, Y int32 }
type rect struct{ Left, Top, Right, Bottom int32 }
type message struct {
	Hwnd           uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Pt             point
	Private        uint32
}
type windowClass struct {
	Size, Style                        uint32
	WndProc                            uintptr
	ClassExtra, WindowExtra            int32
	Instance, Icon, Cursor, Background uintptr
	MenuName, ClassName                *uint16
	SmallIcon                          uintptr
}
type listColumn struct {
	Mask                                                               uint32
	Fmt, Width                                                         int32
	Text                                                               *uint16
	TextMax, SubItem, Image, Order, MinWidth, DefaultWidth, IdealWidth int32
}
type listItem struct {
	Mask             uint32
	Item, SubItem    int32
	State, StateMask uint32
	Text             *uint16
	TextMax, Image   int32
	Param            uintptr
	Indent, GroupID  int32
	Columns          uint32
	ColumnIndices    *uint32
	ColumnFormats    *int32
	Group            int32
}
type notifyHeader struct {
	Window, ID uintptr
	Code       int32
}
type listCustomDraw struct {
	Header                     notifyHeader
	Stage                      uint32
	DC                         uintptr
	Bounds                     rect
	Item                       uintptr
	State                      uint32
	Param                      uintptr
	TextColor, BackgroundColor uint32
	SubItem                    int32
}
type itemActivate struct {
	Header                      notifyHeader
	Item, SubItem               int32
	NewState, OldState, Changed uint32
	Action                      point
	Param                       uintptr
	KeyFlags                    uint32
}
type uiResult struct {
	cities []City
	report *WeatherReport
	err    error
	search bool
}
type weatherUI struct {
	hwnd, font                                     uintptr
	controls                                       map[int]uintptr
	all, visible                                   []City
	service                                        *service
	results                                        chan uiResult
	cancel                                         context.CancelFunc
	busy                                           bool
	scale                                          float64
	rows                                           []DayRow
	copyColumn                                     int
	selectedRow                                    int
	store                                          cityStore
	persistenceWarning                             string
	headingFont, reportFont, labelFont, buttonFont uintptr
	surfaceBrush, backgroundBrush                  uintptr
	cards                                          []rect
	fields                                         map[int]rect
	hoveredButton                                  uintptr
}

const (
	idQuery = 101 + iota
	idSearch
	idCity
	idDays
	idFetch
	idCancel
	idStatus
	idTable
	idQueryLabel
	idCityLabel
	idDaysLabel
	idHint
	idSource
	idReport
	idCopyMorning
	idCopyAfternoon
	idDeleteCity
	idClearQuery
	idChart
	idBrand
	idSubtitle
	idVersion
	idContext
)
const resultMessage = 0x8001

func utf16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(strings.ReplaceAll(s, "\x00", ""))
	return p
}
func send(hwnd uintptr, msg uint32, w, l uintptr) uintptr {
	r, _, _ := sendMessage.Call(hwnd, uintptr(msg), w, l)
	return r
}
func caption(hwnd uintptr, s string) { setText.Call(hwnd, uintptr(unsafe.Pointer(utf16(s)))) }
func textOf(hwnd uintptr) string {
	n, _, _ := user32.NewProc("GetWindowTextLengthW").Call(hwnd)
	buf := make([]uint16, n+1)
	user32.NewProc("GetWindowTextW").Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}
func showFatal(text string) {
	user32.NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(utf16(text))), uintptr(unsafe.Pointer(utf16(windowTitle()))), 0x10)
}

func runGUI(cities []City) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	user32.NewProc("SetProcessDPIAware").Call()
	scale := 1.0
	dpi := user32.NewProc("GetDpiForSystem")
	if dpi.Find() == nil {
		value, _, _ := dpi.Call()
		if value > 0 {
			scale = float64(value) / 96
		}
	}
	init := struct{ Size, Classes uint32 }{8, 1}
	comctl32.NewProc("InitCommonControlsEx").Call(uintptr(unsafe.Pointer(&init)))
	instance, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	cursor, _, _ := user32.NewProc("LoadCursorW").Call(0, 32512)
	background, _, _ := gdi32.NewProc("CreateSolidBrush").Call(themeBackground)
	defer gdi32.NewProc("DeleteObject").Call(background)
	class := windowClass{Size: uint32(unsafe.Sizeof(windowClass{})), WndProc: windowCallback, Instance: instance, Cursor: cursor, Background: background, ClassName: utf16("CityWeatherWindow")}
	// The manifest occupies resource 1, and the application icon group is 2.
	// LR_SHARED lets Windows manage these class icon handles.
	class.Icon, _, _ = user32.NewProc("LoadImageW").Call(instance, 2, 1, uintptr(int(32*scale)), uintptr(int(32*scale)), 0x8000)
	class.SmallIcon, _, _ = user32.NewProc("LoadImageW").Call(instance, 2, 1, uintptr(int(16*scale)), uintptr(int(16*scale)), 0x8000)
	atom, _, err := user32.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&class)))
	if atom == 0 {
		return err
	}
	prefs, prefsErr := loadPreferences(dataDirectory())
	ui := &weatherUI{all: cities, visible: cities, service: newService(), controls: make(map[int]uintptr), results: make(chan uiResult, 1), scale: scale, copyColumn: 1, selectedRow: -1, store: cityStore{dir: dataDirectory(), cities: cities, prefs: prefs}}
	activeUI = ui
	defer func() {
		activeUI = nil
		if ui.cancel != nil {
			ui.cancel()
		}
		if ui.font != 0 {
			gdi32.NewProc("DeleteObject").Call(ui.font)
		}
		ui.releaseTheme()
	}()
	hwnd, _, err := createWindow.Call(0, uintptr(unsafe.Pointer(class.ClassName)), uintptr(unsafe.Pointer(utf16(windowTitle()))), 0x00CF0000|0x02000000, 0x80000000, 0x80000000, uintptr(ui.px(1120)), uintptr(ui.px(960)), 0, 0, instance, 0)
	if hwnd == 0 {
		return err
	}
	ui.hwnd = hwnd
	if err := ui.build(); err != nil {
		user32.NewProc("DestroyWindow").Call(hwnd)
		return err
	}
	if prefsErr != nil {
		caption(ui.controls[idStatus], tr("gui_windows_01", prefsErr))
	}
	ui.layout()
	user32.NewProc("ShowWindow").Call(hwnd, 5)
	user32.NewProc("UpdateWindow").Call(hwnd)
	user32.NewProc("SetFocus").Call(ui.controls[idQuery])
	var msg message
	for {
		r, _, err := user32.NewProc("GetMessageW").Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) == -1 {
			return err
		}
		if r == 0 {
			break
		}
		handled, _, _ := user32.NewProc("IsDialogMessageW").Call(hwnd, uintptr(unsafe.Pointer(&msg)))
		if handled == 0 {
			user32.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&msg)))
			user32.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&msg)))
		}
	}
	return nil
}

func (ui *weatherUI) px(v int) int { return int(float64(v) * ui.scale) }
func (ui *weatherUI) build() error {
	font := newUIFont(ui.px(16), 400)
	ui.font = font
	controls := []struct {
		id          int
		class, text string
		style       uint32
	}{
		{idBrand, "STATIC", appName, 0},
		{idSubtitle, "STATIC", tr("design_subtitle"), 0},
		{idVersion, "STATIC", tr("design_version", appVersion), 2},
		{idQueryLabel, "STATIC", tr("gui_windows_02"), 0},
		{idQuery, "EDIT", "", 0x00010000 | 0x80},
		{idClearQuery, "BUTTON", tr("gui_windows_03"), 0x00010000},
		{idSearch, "BUTTON", tr("gui_windows_04"), 0x00010000},
		{idCityLabel, "STATIC", tr("gui_windows_05"), 0},
		{idCity, "COMBOBOX", "", 0x00010000 | 0x00200000 | 3},
		{idDeleteCity, "BUTTON", tr("gui_windows_06"), 0x00010000},
		{idDaysLabel, "STATIC", tr("design_days_label"), 0},
		{idDays, "EDIT", strconv.Itoa(defaultDays), 0x00010000 | 0x2000},
		{idFetch, "BUTTON", tr("gui_windows_08"), 0x00010000 | 1},
		{idCancel, "BUTTON", tr("gui_windows_09"), 0x00010000},
		{idCopyMorning, "BUTTON", tr("gui_windows_10"), 0x00010000},
		{idCopyAfternoon, "BUTTON", tr("gui_windows_11"), 0x00010000},
		{idHint, "STATIC", tr("gui_windows_12"), 0},
		{idStatus, "STATIC", tr("gui_windows_13", defaultDays), 0},
		{idReport, "STATIC", tr("design_empty_report"), 0},
		{idContext, "STATIC", tr("design_empty_context"), 0},
		{idTable, "SysListView32", "", 0x00010000 | 1 | 4 | 8},
		{idChart, "STATIC", tr("chart_title"), 0xD}, // SS_OWNERDRAW
		{idSource, "STATIC", tr("gui_windows_15"), 0},
	}
	for _, c := range controls {
		if c.class == "BUTTON" {
			c.style = 0x00010000 | 0xB
		}
		h, _, err := createWindow.Call(0, uintptr(unsafe.Pointer(utf16(c.class))), uintptr(unsafe.Pointer(utf16(c.text))), uintptr(0x40000000|0x10000000|c.style), 0, 0, 0, 0, ui.hwnd, uintptr(c.id), 0, 0)
		if h == 0 {
			return fmt.Errorf(tr("gui_windows_16"), c.id, err)
		}
		ui.controls[c.id] = h
		send(h, 0x30, font, 1)
	}
	ui.initTheme()
	send(ui.controls[idQuery], 0xC5, 200, 0)
	send(ui.controls[idDays], 0xC5, 3, 0)
	send(ui.controls[idTable], 0x1036, 0, 0x00010000|0x20) // Double buffer, full rows.
	headers := []string{tr("gui_windows_17"), tr("gui_windows_18"), tr("gui_windows_19"), tr("gui_windows_20"), tr("gui_windows_21"), tr("gui_windows_19"), tr("gui_windows_22")}
	widths := []int{180, 100, 100, 200, 100, 100, 200}
	for i, h := range headers {
		col := listColumn{Mask: 7, Width: int32(ui.px(widths[i])), Text: utf16(h)}
		send(ui.controls[idTable], 0x1061, uintptr(i), uintptr(unsafe.Pointer(&col)))
	}
	initial := ui.all
	if last := ui.store.prefs.LastCity; last != nil && cityIndex(initial, *last) < 0 {
		initial = append(append([]City{}, initial...), *last)
	}
	ui.setCities(initial)
	if last := ui.store.prefs.LastCity; last != nil {
		if index := cityIndex(initial, *last); index >= 0 {
			send(ui.controls[idCity], 0x14E, uintptr(index), 0)
		}
	}
	ui.updateDeleteButton()
	ui.enable(idCancel, false)
	ui.enable(idCopyMorning, false)
	ui.enable(idCopyAfternoon, false)
	return nil
}

func (ui *weatherUI) enable(id int, enabled bool) {
	value := uintptr(0)
	if enabled {
		value = 1
	}
	user32.NewProc("EnableWindow").Call(ui.controls[id], value)
}
func (ui *weatherUI) setCities(cities []City) {
	ui.visible = cities
	h := ui.controls[idCity]
	send(h, 0x14B, 0, 0)
	for _, c := range cities {
		send(h, 0x143, 0, uintptr(unsafe.Pointer(utf16(c.label()))))
	}
	if len(cities) > 0 {
		send(h, 0x14E, 0, 0)
	}
	ui.updateDeleteButton()
}

func (ui *weatherUI) selectedCity() (City, bool) {
	index := int32(send(ui.controls[idCity], 0x147, 0, 0))
	if index < 0 || int(index) >= len(ui.visible) {
		return City{}, false
	}
	return ui.visible[index], true
}

func (ui *weatherUI) updateDeleteButton() {
	city, ok := ui.selectedCity()
	ui.enable(idDeleteCity, !ui.busy && ok && cityIndex(ui.all, city) >= 0)
}

func (ui *weatherUI) deleteCity() {
	if ui.busy {
		return
	}
	city, ok := ui.selectedCity()
	if !ok {
		return
	}
	err := ui.store.remove(city)
	ui.all = ui.store.cities
	caption(ui.controls[idQuery], "")
	ui.setCities(ui.all)
	if err != nil {
		caption(ui.controls[idStatus], err.Error())
		return
	}
	caption(ui.controls[idStatus], tr("gui_windows_23", city.label()))
}

func (ui *weatherUI) repaintTable() {
	user32.NewProc("InvalidateRect").Call(ui.controls[idTable], 0, 0)
}
func (ui *weatherUI) layout() {
	if ui.controls[idTable] == 0 {
		return
	}
	var r rect
	user32.NewProc("GetClientRect").Call(ui.hwnd, uintptr(unsafe.Pointer(&r)))
	w, h := int(float64(r.Right)/ui.scale), int(float64(r.Bottom)/ui.scale)
	move := func(id, x, y, width, height int) {
		if width < 1 {
			width = 1
		}
		if height < 1 {
			height = 1
		}
		user32.NewProc("MoveWindow").Call(ui.controls[id], uintptr(ui.px(x)), uintptr(ui.px(y)), uintptr(ui.px(width)), uintptr(ui.px(height)), 1)
	}
	move(idBrand, 28, 16, w-230, 36)
	move(idSubtitle, 29, 55, w-200, 22)
	move(idVersion, w-196, 27, 164, 24)
	move(idQueryLabel, 44, 108, w-420, 20)
	move(idQuery, 56, 142, w-512, 24)
	move(idClearQuery, w-428, 133, 36, 38)
	move(idSearch, w-380, 133, 184, 38)
	move(idDaysLabel, w-164, 108, 120, 20)
	move(idDays, w-152, 142, 96, 24)
	move(idCityLabel, 44, 181, w-540, 20)
	move(idCity, 44, 206, w-660, 320)
	move(idDeleteCity, w-588, 205, 144, 38)
	move(idFetch, w-428, 205, 248, 38)
	move(idCancel, w-164, 205, 120, 38)
	move(idHint, 28, 268, w-56, 22)
	move(idReport, 44, 318, w-500, 30)
	move(idContext, 44, 353, w-88, 22)
	move(idCopyMorning, w-430, 316, 180, 36)
	move(idCopyAfternoon, w-234, 316, 190, 36)
	available := h - 500
	chartHeight := available * 48 / 100
	if chartHeight < 170 {
		chartHeight = 170
	}
	if chartHeight > 320 {
		chartHeight = 320
	}
	tableHeight := available - chartHeight
	chartTop := 388 + tableHeight + 44
	move(idTable, 40, 388, w-80, tableHeight)
	move(idChart, 32, chartTop, w-64, chartHeight-16)
	move(idStatus, 28, h-54, w-56, 22)
	move(idSource, 28, h-29, w-56, 20)
	pxRect := func(x, y, width, height int) rect {
		return rect{int32(ui.px(x)), int32(ui.px(y)), int32(ui.px(x + width)), int32(ui.px(y + height))}
	}
	ui.cards = []rect{pxRect(24, 92, w-48, 164), pxRect(24, 304, w-48, tableHeight+100), pxRect(24, chartTop-8, w-48, chartHeight)}
	ui.fields = map[int]rect{idQuery: pxRect(44, 134, w-488, 36), idDays: pxRect(w-164, 134, 120, 36)}
	user32.NewProc("InvalidateRect").Call(ui.hwnd, 0, 0)
	// Owner-drawn controls must repaint the whole content after a resize.
	user32.NewProc("InvalidateRect").Call(ui.controls[idChart], 0, 0)
	user32.NewProc("InvalidateRect").Call(send(ui.controls[idTable], 0x101F, 0, 0), 0, 0)
}

func (ui *weatherUI) start(search bool) {
	if ui.busy {
		return
	}
	query := textOf(ui.controls[idQuery])
	var city City
	days := 0
	if search {
		if len([]rune(strings.TrimSpace(query))) < 2 {
			caption(ui.controls[idStatus], tr("gui_windows_24"))
			return
		}
	} else {
		selected := int32(send(ui.controls[idCity], 0x147, 0, 0))
		if selected < 0 || int(selected) >= len(ui.visible) {
			caption(ui.controls[idStatus], tr("gui_windows_25"))
			return
		}
		city = ui.visible[selected]
		var err error
		days, err = strconv.Atoi(strings.TrimSpace(textOf(ui.controls[idDays])))
		if err != nil || days < 1 || days > maxDays {
			caption(ui.controls[idStatus], tr("gui_windows_26", maxDays))
			return
		}
		ui.persistenceWarning = ""
		if err := ui.store.remember(city); err != nil {
			ui.persistenceWarning = err.Error()
		}
	}
	ui.busy = true
	for _, id := range []int{idQuery, idClearQuery, idSearch, idCity, idDays, idFetch} {
		ui.enable(id, false)
	}
	ui.updateDeleteButton()
	ui.enable(idCancel, true)
	status := tr("gui_windows_27", city.label())
	if search {
		status = tr("gui_windows_28")
	}
	caption(ui.controls[idStatus], status)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	ui.cancel = cancel
	go func() {
		defer cancel()
		result := uiResult{search: search}
		if search {
			result.cities, result.err = ui.service.search(ctx, query)
		} else {
			report, err := ui.service.weather(ctx, city, days)
			result.report, result.err = &report, err
		}
		ui.results <- result
		postMessage.Call(ui.hwnd, resultMessage, 0, 0)
	}()
}

func (ui *weatherUI) completed() {
	var result uiResult
	select {
	case result = <-ui.results:
	default:
		return
	}
	ui.busy = false
	ui.cancel = nil
	for _, id := range []int{idQuery, idClearQuery, idSearch, idCity, idDays, idFetch} {
		ui.enable(id, true)
	}
	ui.updateDeleteButton()
	ui.enable(idCancel, false)
	if result.err != nil {
		text := tr("gui_windows_29", result.err)
		if result.err == context.Canceled {
			text = tr("gui_windows_30")
		}
		if result.err == context.DeadlineExceeded {
			text = tr("gui_windows_31")
		}
		if ui.persistenceWarning != "" {
			text += "; " + ui.persistenceWarning
		}
		caption(ui.controls[idStatus], text)
		return
	}
	if result.search {
		ui.setCities(result.cities)
		caption(ui.controls[idStatus], fmt.Sprintf(tr("gui_windows_32"), len(result.cities)))
		return
	}
	report := result.report
	city := report.City
	city.Timezone = report.Timezone
	added, saveErr := ui.store.add(city)
	ui.all = ui.store.cities
	ui.updateDeleteButton()
	ui.rows = report.Rows
	ui.copyColumn = 1
	ui.selectedRow = -1
	ui.enable(idCopyMorning, true)
	ui.enable(idCopyAfternoon, true)
	send(ui.controls[idTable], 0xB, 0, 0)
	send(ui.controls[idTable], 0x1009, 0, 0)
	missing := 0
	for i, row := range report.Rows {
		cells := rowCells(row)
		if !row.MorningPending && (row.Morning == nil || row.Morning.Temp == nil) {
			missing++
		}
		if !row.AfternoonPending && (row.Afternoon == nil || row.Afternoon.Temp == nil) {
			missing++
		}
		for j, text := range cells {
			item := listItem{Mask: 1, Item: int32(i), SubItem: int32(j), Text: utf16(text)}
			msg := uint32(0x1074)
			if j == 0 {
				msg = 0x104D
			}
			send(ui.controls[idTable], msg, uintptr(i), uintptr(unsafe.Pointer(&item)))
		}
	}
	send(ui.controls[idTable], 0xB, 1, 0)
	user32.NewProc("InvalidateRect").Call(ui.controls[idTable], 0, 1)
	user32.NewProc("InvalidateRect").Call(ui.controls[idChart], 0, 0)
	caption(ui.controls[idReport], report.City.label())
	caption(ui.controls[idContext], tr("design_period", report.Start, report.End, report.Timezone))
	status := fmt.Sprintf(tr("gui_windows_34"), len(report.Rows), missing)
	if added {
		status = tr("gui_windows_35") + status
	}
	if saveErr != nil {
		status = tr("gui_windows_36", saveErr)
	}
	if ui.persistenceWarning != "" {
		status += " " + ui.persistenceWarning
	}
	caption(ui.controls[idStatus], status)
}

func (ui *weatherUI) copyTemperature(column int, all bool) {
	rows := ui.rows
	if !all {
		// LVM_GETNEXTITEM: focused/selected row; the column follows the last cell click.
		selected := ui.selectedRow
		if selected < 0 || int(selected) >= len(rows) {
			caption(ui.controls[idStatus], tr("gui_windows_37"))
			return
		}
		rows = rows[selected : selected+1]
	}
	value := temperatureText(rows, column)
	if value == "" && !all {
		caption(ui.controls[idStatus], tr("gui_windows_38"))
		return
	}
	if err := copyToClipboard(ui.hwnd, value); err != nil {
		caption(ui.controls[idStatus], tr("gui_windows_39", err))
		return
	}
	timeLabel := "08:00"
	if column == 4 {
		timeLabel = "14:00"
	}
	message := tr("gui_windows_40", value)
	if all {
		message = fmt.Sprintf(tr("gui_windows_41"), timeLabel, len(rows))
	}
	caption(ui.controls[idStatus], message)
}

func copyToClipboard(owner uintptr, text string) error {
	data, err := syscall.UTF16FromString(text)
	if err != nil {
		return err
	}
	opened, _, _ := user32.NewProc("OpenClipboard").Call(owner)
	if opened == 0 {
		return errors.New(tr("gui_windows_42"))
	}
	defer user32.NewProc("CloseClipboard").Call()
	size := uintptr(len(data) * 2)
	memory, _, _ := kernel32.NewProc("GlobalAlloc").Call(0x42, size)
	if memory == 0 {
		return errors.New(tr("gui_windows_43"))
	}
	transferred := false
	defer func() {
		if !transferred {
			kernel32.NewProc("GlobalFree").Call(memory)
		}
	}()
	dest, _, _ := kernel32.NewProc("GlobalLock").Call(memory)
	if dest == 0 {
		return errors.New(tr("gui_windows_44"))
	}
	kernel32.NewProc("RtlMoveMemory").Call(dest, uintptr(unsafe.Pointer(&data[0])), size)
	kernel32.NewProc("GlobalUnlock").Call(memory)
	if ok, _, _ := user32.NewProc("EmptyClipboard").Call(); ok == 0 {
		return errors.New(tr("gui_windows_45"))
	}
	if ok, _, _ := user32.NewProc("SetClipboardData").Call(13, memory); ok == 0 {
		return errors.New(tr("gui_windows_46"))
	}
	transferred = true // Windows owns the global memory after SetClipboardData.
	return nil
}

func (ui *weatherUI) drawCell(draw *listCustomDraw) uintptr {
	row, col := int(draw.Item), int(draw.SubItem)
	if row < 0 || row >= len(ui.rows) || col < 0 || col > 6 {
		return 0
	}
	var bounds rect
	bounds.Top = int32(col)
	send(ui.controls[idTable], 0x1038, uintptr(row), uintptr(unsafe.Pointer(&bounds))) // LVM_GETSUBITEMRECT
	if col == 0 {
		bounds.Right = bounds.Left + int32(send(ui.controls[idTable], 0x101D, 0, 0))
	}
	background, foreground := uint32(themeSurface), uint32(themeText)
	if col == 1 {
		background, foreground = 0xFFF7F0, 0xB66525
	}
	if col == 4 {
		background, foreground = 0xF0F8FF, 0x2263B4
	}
	active := row == ui.selectedRow && col == ui.copyColumn
	if row == ui.selectedRow {
		switch col {
		case 1:
			background = 0xF3E8DC
		case 4:
			background = 0xDCE8F3
		default:
			background = 0xF5F2EF
		}
	}
	if active {
		background, foreground = activeCellBackground, activeCellForeground
	}
	brush, _, _ := gdi32.NewProc("CreateSolidBrush").Call(uintptr(background))
	user32.NewProc("FillRect").Call(draw.DC, uintptr(unsafe.Pointer(&bounds)), brush)
	gdi32.NewProc("DeleteObject").Call(brush)
	cells := rowCells(ui.rows[row])
	textBounds := bounds
	textBounds.Left += int32(ui.px(10))
	textBounds.Right -= int32(ui.px(4))
	gdi32.NewProc("SetBkMode").Call(draw.DC, 1)
	gdi32.NewProc("SetTextColor").Call(draw.DC, uintptr(foreground))
	font := ui.font
	if col == 1 || col == 4 {
		font = ui.buttonFont
	}
	gdi32.NewProc("SelectObject").Call(draw.DC, font)
	user32.NewProc("DrawTextW").Call(draw.DC, uintptr(unsafe.Pointer(utf16(cells[col]))), ^uintptr(0), uintptr(unsafe.Pointer(&textBounds)), 0x4|0x20|0x8000|0x800)
	if active {
		border, _, _ := gdi32.NewProc("CreateSolidBrush").Call(activeCellBorder)
		user32.NewProc("FrameRect").Call(draw.DC, uintptr(unsafe.Pointer(&bounds)), border)
		bounds.Left++
		bounds.Top++
		bounds.Right--
		bounds.Bottom--
		user32.NewProc("FrameRect").Call(draw.DC, uintptr(unsafe.Pointer(&bounds)), border)
		gdi32.NewProc("DeleteObject").Call(border)
	}
	if !active {
		fillColor(draw.DC, rect{bounds.Left, bounds.Bottom - 1, bounds.Right, bounds.Bottom}, 0xFAF7F4)
	}
	return 4 // CDRF_SKIPDEFAULT prevents the bright native selection overlay.
}

func windowProc(hwnd uintptr, msg uint32, w uintptr, l unsafe.Pointer) uintptr {
	ui := activeUI
	switch msg {
	case 0xF: // WM_PAINT: background and surface cards.
		if ui != nil && ui.hwnd != 0 {
			var paint paintInfo
			dc, _, _ := user32.NewProc("BeginPaint").Call(hwnd, uintptr(unsafe.Pointer(&paint)))
			ui.paintBackground(dc)
			user32.NewProc("EndPaint").Call(hwnd, uintptr(unsafe.Pointer(&paint)))
			return 0
		}
	case 0x133, 0x134, 0x135, 0x138: // WM_CTLCOLOR* for native edit, list and text controls.
		if ui != nil && ui.surfaceBrush != 0 {
			surface := false
			if msg != 0x138 {
				surface = true
			}
			for _, id := range []int{idQueryLabel, idCityLabel, idDaysLabel, idReport, idContext} {
				if uintptr(l) == ui.controls[id] {
					surface = true
				}
			}
			color := uint32(themeMuted)
			if uintptr(l) == ui.controls[idBrand] || uintptr(l) == ui.controls[idReport] || msg != 0x138 {
				color = themeText
			}
			gdi32.NewProc("SetTextColor").Call(w, uintptr(color))
			gdi32.NewProc("SetBkMode").Call(w, 1)
			if surface {
				gdi32.NewProc("SetBkColor").Call(w, themeSurface)
				return ui.surfaceBrush
			}
			gdi32.NewProc("SetBkColor").Call(w, themeBackground)
			return ui.backgroundBrush
		}
	case 0x2B: // WM_DRAWITEM
		if ui != nil && l != nil {
			item := (*drawItem)(l)
			if item.ControlID == idChart {
				ui.drawChart(item)
				return 1
			}
			if item.ControlType == 4 {
				ui.drawButton(item)
				return 1
			}
		}
	case 0x4E: // WM_NOTIFY
		if ui == nil || l == nil {
			break
		}
		header := (*notifyHeader)(l)
		if header.Window != ui.controls[idTable] {
			break
		}
		switch header.Code {
		case -12: // NM_CUSTOMDRAW
			draw := (*listCustomDraw)(l)
			switch draw.Stage {
			case 1:
				return 0x20
			case 0x10001:
				return 0x120 // Subitem drawing; suppress the native row focus rectangle.
			case 0x30001:
				return ui.drawCell(draw)
			}
			return 0
		case -2, -3: // NM_CLICK, NM_DBLCLK
			cell := (*itemActivate)(l)
			ui.copyColumn = int(cell.SubItem)
			ui.selectedRow = int(cell.Item)
			ui.repaintTable()
			if header.Code == -3 && cell.Item >= 0 && (cell.SubItem == 1 || cell.SubItem == 4) {
				ui.copyTemperature(int(cell.SubItem), false)
			}
		case -155: // LVN_KEYDOWN (NMLVKEYDOWN is packed; key immediately follows NMHDR).
			key := *(*uint16)(unsafe.Add(l, unsafe.Sizeof(notifyHeader{})))
			ctrl, _, _ := user32.NewProc("GetKeyState").Call(0x11)
			if key == 'C' && ctrl&0x8000 != 0 && (ui.copyColumn == 1 || ui.copyColumn == 4) {
				ui.copyTemperature(ui.copyColumn, false)
			}
			if key == 0x25 && ui.copyColumn > 0 {
				ui.copyColumn--
				ui.repaintTable()
			}
			if key == 0x27 && ui.copyColumn < 6 {
				ui.copyColumn++
				ui.repaintTable()
			}
		case -101: // LVN_ITEMCHANGED keeps keyboard row navigation in sync.
			change := (*itemActivate)(l)
			if change.Changed&8 != 0 && change.NewState&2 != 0 {
				ui.selectedRow = int(change.Item)
				ui.repaintTable()
			}
		}
		return 0
	case 0x24: // WM_GETMINMAXINFO
		if ui != nil {
			info := (*[5]point)(l)
			info[3] = point{int32(ui.px(880)), int32(ui.px(780))}
			return 0
		}
	case 5:
		if ui != nil {
			ui.layout()
		}
		return 0
	case 0x111:
		if ui == nil {
			break
		}
		id, notification := int(w&0xFFFF), uint16(w>>16)
		if (id == idQuery || id == idDays) && (notification == 0x100 || notification == 0x200) {
			user32.NewProc("InvalidateRect").Call(ui.hwnd, 0, 0)
		}
		if id == idQuery && notification == 0x300 && !ui.busy {
			ui.setCities(filterCities(ui.all, textOf(ui.controls[idQuery])))
			caption(ui.controls[idStatus], fmt.Sprintf(tr("gui_windows_47"), len(ui.visible)))
		}
		if notification == 0 {
			switch id {
			case idSearch:
				ui.start(true)
			case idFetch, 1:
				ui.start(false)
			case idCopyMorning:
				ui.copyTemperature(1, true)
			case idCopyAfternoon:
				ui.copyTemperature(4, true)
			case idDeleteCity:
				ui.deleteCity()
			case idClearQuery:
				if !ui.busy {
					caption(ui.controls[idQuery], "")
					user32.NewProc("SetFocus").Call(ui.controls[idQuery])
				}
			case idCancel:
				if ui.cancel != nil {
					ui.cancel()
				}
			case 2:
				if ui.busy && ui.cancel != nil {
					ui.cancel()
				}
			}
		}
		if id == idCity && notification == 1 {
			ui.updateDeleteButton()
		}
		return 0
	case resultMessage:
		if ui != nil {
			ui.completed()
		}
		return 0
	case 0x10:
		if ui != nil && ui.cancel != nil {
			ui.cancel()
		}
		user32.NewProc("DestroyWindow").Call(hwnd)
		return 0
	case 2:
		user32.NewProc("PostQuitMessage").Call(0)
		return 0
	}
	r, _, _ := defWindowProc.Call(hwnd, uintptr(msg), w, uintptr(l))
	return r
}
