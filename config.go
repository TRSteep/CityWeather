package main

// Application metadata and configurable defaults live here, independently of translations.
const (
	appName              = "HistoryTemp"
	appVersion           = "1.2.0"
	defaultDays          = 7
	maxDays              = 366
	archiveURL           = "https://archive-api.open-meteo.com/v1/archive"
	geocodingURL         = "https://geocoding-api.open-meteo.com/v1/search"
	activeCellBackground = 0x9AE2FF // #FFE29A, COLORREF (BGR)
	activeCellForeground = 0x242424
	activeCellBorder     = 0x166A9D // #9D6A16
	chartMorningColor    = 0xCF7227 // #2772CF
	chartAfternoonColor  = 0x2773D6 // #D67327
)
