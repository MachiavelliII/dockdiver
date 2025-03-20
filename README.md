# dockdiver

**dockdiver** is a Go-based tool to explore and extract contents from Docker registries. It can list repositories, dump specific repositories, or dump all repositories, including manifests, config blobs, and layer tarballs, with SHA256 integrity verification. This repository includes a Docker registry lab in the `lab/` directory for testing.

## Features

- List all repositories in a Docker registry.
- Dump specific or all repositories with manifests, configs, and layers.
- Rate limiting for safe operation.
- SHA256 verification for downloaded blobs.

## Prerequisites

- [Go](https://golang.org/dl/) (1.16 or later) for building the tool.
- [Docker](https://www.docker.com/get-started) and [Docker Compose](https://docs.docker.com/compose/install/) for running the lab.

## Instructions

### Setup and Build

**Building the tool**

```bash
git clone https://github.com/MachiavelliII/dockdiver.git
cd dockdiver
go build
./dockdiver -h

       __           __       ___
  ____/ /___  _____/ /______/ (_)   _____  _____
 / __  / __ \/ ___/ //_/ __  / / | / / _ \/ ___/
/ /_/ / /_/ / /__/ ,< / /_/ / /| |/ /  __/ /
\__,_/\____/\___/_/|_|\__,_/_/ |___/\___/_/

Usage of dockdiver:
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
  -list
        List all repositories
  -password string
        Password for authentication
  -port int
        Port of the registry (default: 5000) (default 5000)
  -rate int
        Requests per second (default: 10) (default 10)
  -url string
        Base URL of the Docker registry (e.g., http://example.com or example.com) (default "http://localhost")
  -username string
        Username for authentication
```

### Launching registry lab for testing:

```bash
cd lab/
docker-compose up -d
docker tag test-ubuntu:latest localhost:5000/test-ubuntu:latest
docker push localhost:5000/test-ubuntu:latest
curl http://localhost:5000/v2/_catalog
```
