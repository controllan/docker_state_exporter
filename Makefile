build:
	CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags="-w -s" -o out/docker_state_exporter

docker_build:
	docker build . -t docker_state_exporter

all: build docker_build