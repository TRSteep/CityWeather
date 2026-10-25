$ErrorActionPreference = 'Stop'
Push-Location $PSScriptRoot
try {
    go build -trimpath -ldflags='-H=windowsgui -s -w' -o build/HistoryTemp.exe .
    if ($LASTEXITCODE -ne 0) { throw 'Go build failed' }
    # Preserve the user's saved cities during a rebuild.
    if (-not (Test-Path -LiteralPath build/cities.json)) {
        Copy-Item -LiteralPath cities.json -Destination build/cities.json
    }
    if (-not (Test-Path -LiteralPath build/locale.json)) {
        Copy-Item -LiteralPath locale.json -Destination build/locale.json
    }
    # Distribute the default list, without personal cities or settings.
    Compress-Archive -LiteralPath build/HistoryTemp.exe,cities.json,locale.json -DestinationPath build/HistoryTemp-windows-amd64.zip -Force
    Write-Output 'Built build/HistoryTemp.exe and build/HistoryTemp-windows-amd64.zip'
} finally {
    Pop-Location
}
