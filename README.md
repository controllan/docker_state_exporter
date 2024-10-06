# Docker State Exporter

Exporter for docker container state.

This repository is an unlinked fork of https://github.com/AdaptiveConsulting/docker_state_exporter and 
https://github.com/karugaru/docker_state_exporter.

The fork link has been removed due to these reasons:
- the source repositories are not well maintained as already stated in karugaru/docker_state_exporter
- this version of the docker state exporter also includes a new metric ``container_combined_status`` which provides the
health status and general status in one metric to improve the filtering using Prometheus/Grafana

Prometheus exporter for docker container state, written in Go.

One of the best known exporters of docker container information is [cAdvisor](https://github.com/google/cadvisor).\
However, cAdvisor does not export the state of the container.

This exporter will only export the container status and the restarts count.

## Installation and Usage

The `docker_state_exporter` listens on HTTP port 8080 by default.

### Docker

For Docker run.

```bash
docker run -d \
  -v "/var/run/docker.sock:/var/run/docker.sock" \
  -p 8080:8080 \
  ghcr.io/controllan/docker-state-exporter \
  --web.listen-address=:8080
```

For Docker compose.

```yaml
---
version: '3.8'
services:
  docker_state_exporter:
    image: ghcr.io/controllan/docker-state-exporter:latest
    container_name: docker_state_exporter
    volumes:
      - type: bind
        source: /var/run/docker.sock
        target: /var/run/docker.sock
    ports:
      - "8080:8080"
    command:
      - --no-add-container-labels # Keep this if you do not wand to add container labels.
      - --web.listen-address=:8080 # Set the port of the docker state exporter
    healthcheck:
      test: "netstat -ltun | grep -c ':8080'"
      interval: 30s
      timeout: 10s
      retries: 3
```

## Metrics

This exporter will export the following metrics.

- container_state_health_status
- container_state_status
- container_state_oomkilled
- container_state_startedat
- container_state_finishedat
- container_restartcount
- container_combined_status

These metrics will be the same as the results of docker inspect.

The container_combined_status will contain the general and health status of the container in one metric.

This exporter also exports the standard
[Go Collector](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus#NewGoCollector)
and [Process Collector](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus#NewProcessCollector).

## Performance

The polling of docker inspect commands is set to every one second.

## Build the go binary and container yourself

```bash
git clone https://github.com/controllan/docker_state_exporter
cd docker_state_exporter
docker build . -t docker-state-exporter
```

## Run

```bash
docker run -d \
  -v "/var/run/docker.sock:/var/run/docker.sock" \
  -p 8080:8080 \
  docker-state-exporter \
  --web.listen-address=:8080
```
