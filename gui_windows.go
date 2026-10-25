//go:build windows

package main

import (
	"context"
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
	hwnd, font         uintptr
	controls           map[int]uintptr
	all, visible       []City
	service            *service
	results            chan uiResult
	cancel             context.CancelFunc
	busy               bool
	scale              float64
	rows               []DayRow
	copyColumn         int
	selectedRow        int
	store              cityStore
	persistenceWarning string
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
	idTitle
	idQueryLabel
	idCityLabel
	idDaysLabel
	idHint
	idSource
	idReport
	idCopyMorning
	idCopyAfternoon
	idDeleteCity
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
	user32.NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(utf16(text))), uintptr(unsafe.Pointer(utf16("Архив погоды"))), 0x10)
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
	class := windowClass{Size: uint32(unsafe.Sizeof(windowClass{})), WndProc: windowCallback, Instance: instance, Cursor: cursor, Background: 16, ClassName: utf16("CityWeatherWindow")}
	// Resource group 1 is the application icon embedded by go generate.
	// LR_SHARED lets Windows manage these class icon handles.
	class.Icon, _, _ = user32.NewProc("LoadImageW").Call(instance, 1, 1, uintptr(int(32*scale)), uintptr(int(32*scale)), 0x8000)
	class.SmallIcon, _, _ = user32.NewProc("LoadImageW").Call(instance, 1, 1, uintptr(int(16*scale)), uintptr(int(16*scale)), 0x8000)
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
	}()
	hwnd, _, err := createWindow.Call(0, uintptr(unsafe.Pointer(class.ClassName)), uintptr(unsafe.Pointer(utf16("Архив погоды"))), 0x00CF0000|0x02000000, 0x80000000, 0x80000000, uintptr(ui.px(1120)), uintptr(ui.px(760)), 0, 0, instance, 0)
	if hwnd == 0 {
		return err
	}
	ui.hwnd = hwnd
	if err := ui.build(); err != nil {
		user32.NewProc("DestroyWindow").Call(hwnd)
		return err
	}
	if prefsErr != nil {
		caption(ui.controls[idStatus], "Не удалось прочитать последний город: "+prefsErr.Error())
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
	font, _, _ := gdi32.NewProc("CreateFontW").Call(uintptr(int32(-ui.px(17))), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(utf16("Segoe UI"))))
	ui.font = font
	controls := []struct {
		id          int
		class, text string
		style       uint32
	}{
		{idTitle, "STATIC", "Погода за прошедшие дни", 0},
		{idQueryLabel, "STATIC", "Поиск города в списке", 0},
		{idQuery, "EDIT", "", 0x00800000 | 0x00010000 | 0x80},
		{idSearch, "BUTTON", "Найти в интернете", 0x00010000},
		{idCityLabel, "STATIC", "Город — выберите из списка или результатов поиска", 0},
		{idCity, "COMBOBOX", "", 0x00010000 | 0x00200000 | 3},
		{idDeleteCity, "BUTTON", "Удалить город", 0x00010000},
		{idDaysLabel, "STATIC", "Прошедших дней (1–366)", 0},
		{idDays, "EDIT", "7", 0x00800000 | 0x00010000 | 0x2000},
		{idFetch, "BUTTON", "Запросить погоду", 0x00010000 | 1},
		{idCancel, "BUTTON", "Отмена", 0x00010000},
		{idCopyMorning, "BUTTON", "Копировать 08:00", 0x00010000},
		{idCopyAfternoon, "BUTTON", "Копировать 14:00", 0x00010000},
		{idHint, "STATIC", "08:00 и 14:00 — местное время города. Сегодня не входит в период.", 0},
		{idStatus, "STATIC", "Выберите город и запросите погоду. По умолчанию — последние 7 завершённых дней.", 0},
		{idReport, "STATIC", "Результаты появятся здесь после запроса.", 0},
		{idTable, "SysListView32", "", 0x00010000 | 0x00800000 | 1 | 4 | 8},
		{idSource, "STATIC", "Open-Meteo / ECMWF IFS — модельная оценка погоды  •  Города: GeoNames", 0},
	}
	for _, c := range controls {
		h, _, err := createWindow.Call(0, uintptr(unsafe.Pointer(utf16(c.class))), uintptr(unsafe.Pointer(utf16(c.text))), uintptr(0x40000000|0x10000000|c.style), 0, 0, 0, 0, ui.hwnd, uintptr(c.id), 0, 0)
		if h == 0 {
			return fmt.Errorf("элемент интерфейса %d: %w", c.id, err)
		}
		ui.controls[c.id] = h
		send(h, 0x30, font, 1)
	}
	send(ui.controls[idQuery], 0xC5, 200, 0)
	send(ui.controls[idDays], 0xC5, 3, 0)
	send(ui.controls[idTable], 0x1036, 0, 0x00010000|0x20|1) // Double buffer, full rows, grid.
	headers := []string{"Дата", "08:00 · °C", "Влажн. · %", "Погода в 08:00", "14:00 · °C", "Влажн. · %", "Погода в 14:00"}
	widths := []int{105, 100, 100, 220, 100, 100, 220}
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
	caption(ui.controls[idStatus], "Город удалён из списка: "+city.label())
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
	move(idTitle, 24, 18, w-48, 32)
	move(idQueryLabel, 24, 60, w-240, 24)
	move(idQuery, 24, 88, w-254, 32)
	move(idSearch, w-214, 87, 190, 34)
	move(idCityLabel, 24, 132, w-320, 24)
	move(idCity, 24, 160, w-510, 320)
	move(idDeleteCity, w-486, 159, 190, 34)
	move(idDaysLabel, w-274, 132, 250, 24)
	move(idDays, w-274, 160, 250, 32)
	move(idFetch, 24, 212, 244, 34)
	move(idCancel, 280, 212, 116, 34)
	move(idCopyMorning, w-420, 212, 192, 34)
	move(idCopyAfternoon, w-216, 212, 192, 34)
	move(idHint, 24, 260, w-48, 24)
	move(idStatus, 24, 293, w-48, 24)
	move(idReport, 24, 321, w-48, 28)
	move(idTable, 24, 355, w-48, h-403)
	move(idSource, 24, h-33, w-48, 25)
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
			caption(ui.controls[idStatus], "Для поиска в интернете введите не менее двух букв.")
			return
		}
	} else {
		selected := int32(send(ui.controls[idCity], 0x147, 0, 0))
		if selected < 0 || int(selected) >= len(ui.visible) {
			caption(ui.controls[idStatus], "Выберите город. Если его нет в списке, нажмите «Найти в интернете».")
			return
		}
		city = ui.visible[selected]
		var err error
		days, err = strconv.Atoi(strings.TrimSpace(textOf(ui.controls[idDays])))
		if err != nil || days < 1 || days > 366 {
			caption(ui.controls[idStatus], "Укажите целое число дней от 1 до 366.")
			return
		}
		ui.persistenceWarning = ""
		if err := ui.store.remember(city); err != nil {
			ui.persistenceWarning = err.Error()
		}
	}
	ui.busy = true
	for _, id := range []int{idQuery, idSearch, idCity, idDays, idFetch} {
		ui.enable(id, false)
	}
	ui.updateDeleteButton()
	ui.enable(idCancel, true)
	status := "Загрузка погоды: " + city.label() + "…"
	if search {
		status = "Поиск городов в интернете…"
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
	for _, id := range []int{idQuery, idSearch, idCity, idDays, idFetch} {
		ui.enable(id, true)
	}
	ui.updateDeleteButton()
	ui.enable(idCancel, false)
	if result.err != nil {
		text := "Ошибка: " + result.err.Error()
		if result.err == context.Canceled {
			text = "Запрос отменён."
		}
		if result.err == context.DeadlineExceeded {
			text = "Время ожидания истекло. Повторите запрос."
		}
		if ui.persistenceWarning != "" {
			text += "; " + ui.persistenceWarning
		}
		caption(ui.controls[idStatus], text)
		return
	}
	if result.search {
		ui.setCities(result.cities)
		caption(ui.controls[idStatus], fmt.Sprintf("Найдено городов: %d. Выберите нужный по региону и стране. Измените поиск для возврата к cities.json.", len(result.cities)))
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
		date, _ := time.Parse(time.DateOnly, row.Date)
		cells := append([]string{date.Format("02.01.2006")}, observationCells(row.Morning)...)
		cells = append(cells, observationCells(row.Afternoon)...)
		if row.Morning == nil || row.Morning.Temp == nil {
			missing++
		}
		if row.Afternoon == nil || row.Afternoon.Temp == nil {
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
	caption(ui.controls[idReport], fmt.Sprintf("%s • %s — %s • %s", report.City.label(), report.Start, report.End, report.Timezone))
	status := fmt.Sprintf("Дней: %d. Пропусков: %d. Двойной щелчок по температуре или Ctrl+C — копировать число.", len(report.Rows), missing)
	if added {
		status = "Город добавлен в список. " + status
	}
	if saveErr != nil {
		status = saveErr.Error() + ". Погода загружена."
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
			caption(ui.controls[idStatus], "Выберите ячейку температуры для копирования.")
			return
		}
		rows = rows[selected : selected+1]
	}
	value := temperatureText(rows, column)
	if value == "" && !all {
		caption(ui.controls[idStatus], "В выбранной ячейке нет температуры.")
		return
	}
	if err := copyToClipboard(ui.hwnd, value); err != nil {
		caption(ui.controls[idStatus], "Не удалось скопировать: "+err.Error())
		return
	}
	timeLabel := "08:00"
	if column == 4 {
		timeLabel = "14:00"
	}
	message := "Скопировано: " + value
	if all {
		message = fmt.Sprintf("Скопирован столбец %s: %d строк, без единиц и заголовка. Пропуски — пустые строки.", timeLabel, len(rows))
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
		return fmt.Errorf("буфер обмена занят; повторите попытку")
	}
	defer user32.NewProc("CloseClipboard").Call()
	size := uintptr(len(data) * 2)
	memory, _, _ := kernel32.NewProc("GlobalAlloc").Call(0x42, size)
	if memory == 0 {
		return fmt.Errorf("недостаточно памяти")
	}
	transferred := false
	defer func() {
		if !transferred {
			kernel32.NewProc("GlobalFree").Call(memory)
		}
	}()
	dest, _, _ := kernel32.NewProc("GlobalLock").Call(memory)
	if dest == 0 {
		return fmt.Errorf("не удалось подготовить текст")
	}
	kernel32.NewProc("RtlMoveMemory").Call(dest, uintptr(unsafe.Pointer(&data[0])), size)
	kernel32.NewProc("GlobalUnlock").Call(memory)
	if ok, _, _ := user32.NewProc("EmptyClipboard").Call(); ok == 0 {
		return fmt.Errorf("не удалось открыть запись")
	}
	if ok, _, _ := user32.NewProc("SetClipboardData").Call(13, memory); ok == 0 {
		return fmt.Errorf("не удалось записать текст")
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
	background, foreground := uint32(0xFFFFFF), uint32(0x302A24)
	if col == 1 {
		background, foreground = 0xFFF2E6, 0x854500
	}
	if col == 4 {
		background, foreground = 0xE6F1FF, 0x194A91
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
		background = 0xF5E4D4
	}
	brush, _, _ := gdi32.NewProc("CreateSolidBrush").Call(uintptr(background))
	user32.NewProc("FillRect").Call(draw.DC, uintptr(unsafe.Pointer(&bounds)), brush)
	gdi32.NewProc("DeleteObject").Call(brush)
	day := ui.rows[row]
	date, _ := time.Parse(time.DateOnly, day.Date)
	cells := append([]string{date.Format("02.01.2006")}, observationCells(day.Morning)...)
	cells = append(cells, observationCells(day.Afternoon)...)
	textBounds := bounds
	textBounds.Left += int32(ui.px(6))
	textBounds.Right -= int32(ui.px(4))
	gdi32.NewProc("SetBkMode").Call(draw.DC, 1)
	gdi32.NewProc("SetTextColor").Call(draw.DC, uintptr(foreground))
	gdi32.NewProc("SelectObject").Call(draw.DC, ui.font)
	user32.NewProc("DrawTextW").Call(draw.DC, uintptr(unsafe.Pointer(utf16(cells[col]))), ^uintptr(0), uintptr(unsafe.Pointer(&textBounds)), 0x4|0x20|0x8000|0x800)
	if active {
		border, _, _ := gdi32.NewProc("CreateSolidBrush").Call(0xBB946D)
		user32.NewProc("FrameRect").Call(draw.DC, uintptr(unsafe.Pointer(&bounds)), border)
		gdi32.NewProc("DeleteObject").Call(border)
	}
	return 4 // CDRF_SKIPDEFAULT prevents the bright native selection overlay.
}

func windowProc(hwnd uintptr, msg uint32, w uintptr, l unsafe.Pointer) uintptr {
	ui := activeUI
	switch msg {
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
			info[3] = point{int32(ui.px(880)), int32(ui.px(600))}
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
		if id == idQuery && notification == 0x300 && !ui.busy {
			ui.setCities(filterCities(ui.all, textOf(ui.controls[idQuery])))
			caption(ui.controls[idStatus], fmt.Sprintf("В локальном списке: %d. Если нужного города нет — нажмите «Найти в интернете».", len(ui.visible)))
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
