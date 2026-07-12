BINARY := pimonitor
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build build-arm64 build-arm run test lint install clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/pimonitor

# GOARM=6 covers Raspberry Pi Zero/1 (ARMv6). Use build-arm64 instead for
# Pi 3/4/5 running a 64-bit OS.
build-arm:
	GOOS=linux GOARCH=arm GOARM=6 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-arm ./cmd/pimonitor

build-arm64:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-arm64 ./cmd/pimonitor

run:
	go run ./cmd/pimonitor -config packaging/pimonitor.example.yaml

test:
	go test ./...

lint:
	golangci-lint run

install: build-arm64
	sudo ./packaging/install.sh bin/$(BINARY)-arm64

clean:
	rm -rf bin
