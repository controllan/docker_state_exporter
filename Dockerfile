FROM golang:1.17-alpine as builder

RUN apk update && apk add \
    git \
    ca-certificates

COPY *.go go.mod go.sum $GOPATH/src/docker_state_exporter/

WORKDIR $GOPATH/src/docker_state_exporter/

RUN go mod download all
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags="-w -s" -o /go/bin/docker_state_exporter

FROM alpine:3

COPY --from=builder /go/bin/docker_state_exporter /go/bin/docker_state_exporter

EXPOSE 8080

ENTRYPOINT ["/go/bin/docker_state_exporter"]
CMD ["-listen-address=:8080"]