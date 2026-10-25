$ErrorActionPreference = 'Stop'
Push-Location $PSScriptRoot
try {
    go build -trimpath -ldflags='-H=windowsgui -s -w' -o build/CityWeather.exe .
    if ($LASTEXITCODE -ne 0) { throw 'Go build failed' }
    Copy-Item -LiteralPath cities.json -Destination build/cities.json -Force
    Compress-Archive -LiteralPath build/CityWeather.exe,build/cities.json -DestinationPath build/CityWeather-windows-amd64.zip -Force
    Write-Output 'Built build/CityWeather.exe and build/CityWeather-windows-amd64.zip'
} finally {
    Pop-Location
}
