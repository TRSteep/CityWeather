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
    New-Item -ItemType Directory -Path build/package -Force | Out-Null
    Copy-Item -LiteralPath build/HistoryTemp.exe -Destination build/package/HistoryTemp.exe -Force
    Copy-Item -LiteralPath cities.json -Destination build/package/cities.json -Force
    Copy-Item -LiteralPath locale.json -Destination build/package/locale.json -Force
    Compress-Archive -LiteralPath build/package/HistoryTemp.exe,build/package/cities.json,build/package/locale.json -DestinationPath build/HistoryTemp-windows-amd64.zip -Force
    Write-Output 'Built build/HistoryTemp.exe and build/HistoryTemp-windows-amd64.zip'
} finally {
    Pop-Location
}
