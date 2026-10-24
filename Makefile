PI_HOST ?= pi@raspberrypi.local
BIN := bin

.PHONY: all test vet lint fmt build build-pi deploy chart-lint image clean

all: lint test build-pi chart-lint

test:
	go test ./...

vet:
	go vet ./...

# golangci-lint v2, config in .golangci.yml (includes govet).
lint:
	golangci-lint run ./...
	golangci-lint fmt --diff ./...

fmt:
	golangci-lint fmt ./...

# Native build (for local runs against a test server with -host).
build:
	go build -o $(BIN)/client ./cmd/client

# Raspberry Pi 5 (64-bit Raspberry Pi OS).
build-pi:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o $(BIN)/client-linux-arm64 ./cmd/client

deploy: build-pi
	scp $(BIN)/client-linux-arm64 $(PI_HOST):~/client

CHART := charts/soracom-beam

chart-lint:
	helm lint $(CHART) --set server.beam.signatureKey=lint
	helm template soracom-beam $(CHART) --set server.beam.signatureKey=lint | kubeconform -strict -summary

# Local server image (CI builds and pushes the multi-arch one).
image:
	docker build -t soracom-beam-server:dev .

clean:
	rm -rf $(BIN)
