package main

// Application metadata and configurable defaults live here, independently of translations.
const (
	appName              = "HistoryTemp"
	appVersion           = "1.3.0"
	defaultDays          = 7
	maxDays              = 366
	archiveURL           = "https://archive-api.open-meteo.com/v1/archive"
	geocodingURL         = "https://geocoding-api.open-meteo.com/v1/search"
	activeCellBackground = 0x9AE2FF // #FFE29A, COLORREF (BGR)
	activeCellForeground = 0x242424
	activeCellBorder     = 0x166A9D // #9D6A16
	chartMorningColor    = 0xCF7227 // #2772CF
	chartAfternoonColor  = 0x2773D6 // #D67327
	// Theme colours use Windows COLORREF (BGR).
	themeBackground    = 0xFBF7F4 // #F4F7FB
	themeSurface       = 0xFFFFFF
	themeText          = 0x2A170F // #0F172A
	themeMuted         = 0x8B7464 // #64748B
	themeBorder        = 0xF0E8E2 // #E2E8F0
	themeAccent        = 0xEB6325 // #2563EB
	themeAccentPressed = 0xAF401E
	themeHeader        = 0xFCFAF8 // #F8FAFC
)
