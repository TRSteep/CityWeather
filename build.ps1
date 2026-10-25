$ErrorActionPreference = 'Stop'
Push-Location $PSScriptRoot
try {
    go build -trimpath -ldflags='-H=windowsgui -s -w' -o build/CityWeather.exe .
    if ($LASTEXITCODE -ne 0) { throw 'Go build failed' }
    # Preserve the user's saved cities during a rebuild.
    if (-not (Test-Path -LiteralPath build/cities.json)) {
        Copy-Item -LiteralPath cities.json -Destination build/cities.json
    }
    # Distribute the default list, without personal cities or settings.
    New-Item -ItemType Directory -Path build/package -Force | Out-Null
    Copy-Item -LiteralPath build/CityWeather.exe -Destination build/package/CityWeather.exe -Force
    Copy-Item -LiteralPath cities.json -Destination build/package/cities.json -Force
    Compress-Archive -LiteralPath build/package/CityWeather.exe,build/package/cities.json -DestinationPath build/CityWeather-windows-amd64.zip -Force
    Write-Output 'Built build/CityWeather.exe and build/CityWeather-windows-amd64.zip'
} finally {
    Pop-Location
}
