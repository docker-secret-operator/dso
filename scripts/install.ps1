# ==============================================================================
# Docker Secret Operator (DSO) - Windows Installer (V3.1)
# ==============================================================================
# This script performs a full installation of the DSO Docker CLI plugin
# to the local user configuration folder.
# ==============================================================================

$ErrorActionPreference = "Stop"

# Configuration
$RepoUrl = "https://github.com/docker-secret-operator/dso"
$BuildDir = Join-Path $Env:TEMP "dso-install"
$DockerConfig = if ($Env:DOCKER_CONFIG) { $Env:DOCKER_CONFIG } else { Join-Path $Env:USERPROFILE ".docker" }
$PluginDir = Join-Path $DockerConfig "cli-plugins"
$BinaryName = "docker-dso.exe"

Write-Host "==========================================" -ForegroundColor Cyan
Write-Host "   Installing Docker Secret Operator (DSO) " -ForegroundColor Cyan
Write-Host "==========================================" -ForegroundColor Cyan

# 1. Dependency Check
Write-Host "[1/5] Checking dependencies..." -ForegroundColor Green

# Check for Docker
if (!(Get-Command docker -ErrorAction SilentlyContinue)) {
    Write-Host "Error: Docker not found. Please install Docker for Windows first." -ForegroundColor Red
    exit 1
}

# Check for Go (Minimum 1.22)
if (!(Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "Error: Go not found. DSO requires Go 1.22+ to build." -ForegroundColor Red
    exit 1
}

# 2. Download Project Files
Write-Host "[2/5] Downloading DSO source..." -ForegroundColor Green
if (Test-Path $BuildDir) { Remove-Item -Recurse -Force $BuildDir }
New-Item -ItemType Directory -Path $BuildDir | Out-Null
Set-Location $BuildDir
git clone $RepoUrl .

# 3. Build Core Binary
Write-Host "[3/5] Building docker-dso.exe..." -ForegroundColor Green
$env:CGO_ENABLED = "0"
go build -ldflags="-s -w" -o $BinaryName ./cmd/docker-dso/

# 4. Install Docker CLI Plugin
Write-Host "[4/5] Installing plugin to $PluginDir..." -ForegroundColor Green
if (!(Test-Path $PluginDir)) { New-Item -ItemType Directory -Path $PluginDir | Out-Null }

# Safe Overwrite
$DestPath = Join-Path $PluginDir $BinaryName
if (Test-Path $DestPath) {
    Write-Host "Existing plugin found, updating..." -ForegroundColor Cyan
    Remove-Item $DestPath -Force
}

Copy-Item $BinaryName $DestPath

# 5. Verification
Write-Host "[5/5] Verifying installation..." -ForegroundColor Green

docker dso version | Out-Null
if ($LASTEXITCODE -eq 0) {
    $Version = (docker dso version) | Select-Object -First 1
    Write-Host "✓ $Version installed successfully!" -ForegroundColor Green
} else {
    Write-Host "❌ Docker CLI plugin installation failed..." -ForegroundColor Red
    Write-Host "Ensure $PluginDir is in your Docker config path." -ForegroundColor Yellow
    exit 1
}

# Success message
Write-Host "================================================================ " -ForegroundColor Cyan
Write-Host "   DSO is now a native Docker CLI plugin for Windows!           " -ForegroundColor Green
Write-Host "================================================================ " -ForegroundColor Cyan
Write-Host "Usage:"
Write-Host "  - docker dso up            (Deploys and starts agent)"
Write-Host "  - docker dso agent         (Runs engine in foreground)"
Write-Host "  - docker dso version       (Check status)"
Write-Host "================================================================ " -ForegroundColor Cyan

# Cleanup
Set-Location $HOME
Remove-Item -Recurse -Force $BuildDir
