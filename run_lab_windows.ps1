# Function to check for required tools and install if requested
function Check-Tools {
    # Check if Docker is installed
    if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
        Write-Host "[-] Docker is not installed!" -ForegroundColor Red
        $answer = Read-Host "Would you like to install Docker? (y/n)"
        if ($answer -eq "y" -or $answer -eq "Y") {
            Write-Host "[*] Installing Docker..." -ForegroundColor Yellow
            # Install Docker Desktop using winget
            winget install -e --id Docker.DockerDesktop
            # Verify installation
            if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
                Write-Host "[-] Docker installation failed. Please install manually." -ForegroundColor Red
                exit 1
            }
        } else {
            Write-Host "[-] Lab cannot start without Docker. Exiting..." -ForegroundColor Red
            exit 1
        }
    }

    # Check if Docker Compose is installed (docker-compose is part of Docker Desktop on Windows :>)
    if (-not (Get-Command docker-compose -ErrorAction SilentlyContinue)) {
        Write-Host "[-] Docker Compose is not installed!" -ForegroundColor Red
        $answer = Read-Host "Would you like to install Docker Compose? (y/n)"
        if ($answer -eq "y" -or $answer -eq "Y") {
            Write-Host "[*] Installing Docker Compose (via Docker Desktop)..." -ForegroundColor Yellow
            # Docker Compose is bundled with Docker Desktop, so reinstall Docker Desktop
            winget install -e --id Docker.DockerDesktop
            # Verify installation
            if (-not (Get-Command docker-compose -ErrorAction SilentlyContinue)) {
                Write-Host "[-] Docker Compose installation failed. Please install manually." -ForegroundColor Red
                exit 1
            }
        } else {
            Write-Host "[-] Lab cannot start without Docker Compose. Exiting..." -ForegroundColor Red
            exit 1
        }
    }
}

# Check tools before proceeding
Check-Tools

# Change to the lab directory
Set-Location -Path "lab\"

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
