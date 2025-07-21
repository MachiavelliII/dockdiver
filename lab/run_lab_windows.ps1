# Function to check for required tools and install if requested
function Check-Tools {
    # Check if Docker is installed
    if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
        Write-Host "[-] Docker is not installed!" -ForegroundColor Red
        $answer = Read-Host "Would you like to install Docker? (y/n)"
        if ($answer -eq "y" -or $answer -eq "Y") {
            Write-Host "[*] Installing Docker..." -ForegroundColor Yellow
            winget install -e --id Docker.DockerDesktop
            Write-Host "[*] Docker Desktop installed. Please start Docker Desktop from the Start menu." -ForegroundColor Yellow
            Write-Host "[*] You may need to accept the terms and conditions and complete the initial setup in the GUI." -ForegroundColor Yellow
            Write-Host "[*] After setup, this script will restart and continue automatically." -ForegroundColor Yellow
            $scriptPath = $PSCommandPath
            Start-Process powershell -ArgumentList "-NoProfile -ExecutionPolicy Bypass -File `"$scriptPath`"" -NoNewWindow
            exit 0
        } else {
            Write-Host "[-] Lab cannot start without Docker. Exiting..." -ForegroundColor Red
            exit 1
        }
    }

    # Check if Docker daemon is running
    Write-Host "[*] Checking Docker daemon status..." -ForegroundColor Yellow
    try {
        $result = & docker info --format '{{.ServerVersion}}' 2>&1
        if ($LASTEXITCODE -ne 0) {
            throw "Docker daemon not running: $result"
        }
        Write-Host "[+] Docker daemon is running." -ForegroundColor Green
    } catch {
        Write-Host "[-] Docker daemon is not running!" -ForegroundColor Red
        Write-Host "[*] Please start Docker Desktop from the Start menu if it’s not already running." -ForegroundColor Yellow
        Write-Host "[*] If recently installed, accept the terms and conditions and complete the GUI setup." -ForegroundColor Yellow
        $answer = Read-Host "Wait for Docker Desktop to start? (y/n)"
        if ($answer -eq "y" -or $answer -eq "Y") {
            Write-Host "[*] Waiting for Docker daemon... Press Ctrl+C to cancel." -ForegroundColor Yellow
            do {
                Start-Sleep -Seconds 5
                try {
                    $result = & docker info --format '{{.ServerVersion}}' 2>&1
                    if ($LASTEXITCODE -ne 0) {
                        throw "Docker daemon not running: $result"
                    }
                    Write-Host "[+] Docker daemon is now running!" -ForegroundColor Green
                    break
                } catch {
                    Write-Host "[*] Still waiting for Docker daemon... Ensure Docker Desktop is running and setup is complete." -ForegroundColor Yellow
                }
            } while ($true)
        } else {
            Write-Host "[-] Lab cannot start without Docker. Exiting..." -ForegroundColor Red
            exit 1
        }
    }

    # Check if Docker Compose is installed
    if (-not (Get-Command docker-compose -ErrorAction SilentlyContinue)) {
        Write-Host "[-] Docker Compose is not installed! This should be included with Docker Desktop." -ForegroundColor Red
        Write-Host "[-] Please ensure Docker Desktop is installed correctly and rerun this script." -ForegroundColor Red
        exit 1
    }
}

# Check tools and daemon before proceeding
Check-Tools

# Change to the lab directory
Set-Location -Path "docker\"

# Stop any existing lab
Write-Host "[*] Stopping and removing any existing lab for a fresh start..." -ForegroundColor Yellow
docker-compose down
if (Test-Path "registry-data") {
    Remove-Item -Path "registry-data" -Recurse -Force
}
Start-Sleep -Seconds 1

# Start the lab
Write-Host "[+] Starting the Docker registry lab on localhost:5000..." -ForegroundColor Green
docker-compose up -d
Start-Sleep -Seconds 2

# Tag and push the test image
Write-Host "[+] Tagging test-ubuntu image..." -ForegroundColor Green
docker tag test-ubuntu:latest localhost:5000/test-ubuntu:latest
Start-Sleep -Seconds 2

Write-Host "[+] Pushing test-ubuntu to localhost:5000..." -ForegroundColor Green
docker push localhost:5000/test-ubuntu:latest
Start-Sleep -Seconds 2

# Verify the registry
Write-Host "[+] Verifying the registry with Invoke-RestMethod..." -ForegroundColor Green
Invoke-RestMethod -Uri "http://localhost:5000/v2/_catalog"
Start-Sleep -Seconds 1
