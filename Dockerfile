FROM --platform=${BUILDPLATFORM:-linux/amd64} golang:1.22-alpine AS builder

ARG TARGETPLATFORM
ARG BUILDPLATFORM
ARG TARGETOS
ARG TARGETARCH

RUN apk update && apk add \
    git \
    ca-certificates

COPY *.go go.mod $GOPATH/src/docker_state_exporter/

WORKDIR $GOPATH/src/docker_state_exporter/

RUN go mod tidy
RUN go mod vendor -v
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -a -installsuffix cgo -ldflags="-w -s" -o /go/bin/docker_state_exporter

FROM alpine:3

RUN apk -U --no-cache upgrade

COPY --from=builder /go/bin/docker_state_exporter /go/bin/docker_state_exporter

EXPOSE 8080

ENTRYPOINT ["/go/bin/docker_state_exporter"]
CMD ["-listen-address=:8080"]