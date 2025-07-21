# dockdiver

**dockdiver** is a Go-based tool to explore and extract contents from Docker registries. It can list repositories, dump specific repositories, or dump all repositories, including manifests, config blobs, and layer tarballs, with SHA256 integrity verification. This repository includes a Docker registry lab in the `lab/` directory for testing.

[![dockdiver-usage](https://asciinema.org/a/kLgWzDiTWnoGURFHBs5XXXbLg.svg)](https://asciinema.org/a/kLgWzDiTWnoGURFHBs5XXXbLg)

## Features

- List all repositories in a Docker registry.
- Dump specific or all repositories with manifests, configs, and layers.
- Rate limiting for safe operation.
- SHA256 verification for downloaded blobs.

## Prerequisites

- [Go](https://golang.org/dl/) (1.16 or later) for building the tool.
- [Docker](https://www.docker.com/get-started) and [Docker Compose](https://docs.docker.com/compose/install/) for running the lab (Optional).

## Instructions

### Setup and Build

**Building the tool**

```bash
git clone https://github.com/MachiavelliII/dockdiver.git
cd dockdiver
go build
./dockdiver

       __           __       ___
  ____/ /___  _____/ /______/ (_)   _____  _____
 / __  / __ \/ ___/ //_/ __  / / | / / _ \/ ___/
/ /_/ / /_/ / /__/ ,< / /_/ / /| |/ /  __/ /   @MachIaVellill
\__,_/\____/\___/_/|_|\__,_/_/ |___/\___/_/

Usage of ./dockdiver:
  -bearer string
        Bearer token for Authorization
  -dir string
        Output directory for dumped files (default "docker_dump")
  -dump string
        Specific repository to dump
  -dump-all
        Dump all repositories
  -headers string
        Custom headers as JSON (e.g., '{"X-Custom": "Value"}')
  -insecure
        Skip TLS certificate verification
  -list
        List all repositories
  -password string
        Password for Basic authentication
  -port int
        Port of the registry (used if not specified in URL) (default 5000)
  -proxy string
        Proxy URL (e.g., http://127.0.0.1:8080, https://proxy.com:8443, or socks5://127.0.0.1:1080)
  -proxy-password string
        Password for SOCKS5 proxy authentication
  -proxy-username string
        Username for SOCKS5 proxy authentication
  -rate int
        Requests per second (default 3)
  -timeout duration
        HTTP request timeout (e.g., 10s, 500ms) (default 30s)
  -url string
        Base URL or hostname of the Docker registry (e.g., http://example.com or example.com)
  -username string
        Username for Basic authentication
```

### Launching registry lab for testing:

Run the following command to set up the lab **(Linux & macOS)**:
```bash
cd lab/ && chmod +x run_lab.sh && ./run_lab.sh
```
For **Windows**:
```ps1
Set-ExecutionPolicy -Scope CurrentUser -ExecutionPolicy RemoteSigned # Run once if needed
cd .\lab; .\run_lab_windows.ps1
```

### Cleaning up after testing

For **(Linux & macOS)**:
```bash
# Inside the lab/docker/ directory.
sudo docker-compose down
rm -rf registry-data
```

For **Windows**:
```ps1
docker-compose down # Inside the lab/docker/ directory.
Remove-Item -Path "registry-data" -Recurse -Force
```

#### Inspired By [DockerRegistryGrabber](https://github.com/Syzik/DockerRegistryGrabber)
