VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: dev frontend:dev frontend:build test audit verify build clean

dev:
	go run .

frontend:dev:
	cd frontend && npm run dev -- --host 127.0.0.1

frontend:build:
	cd frontend && npm ci && npm run build

test: frontend:build
	go test ./...
	go test -race ./...
	go vet ./...

audit:
	cd frontend && npm audit --omit=dev

verify: test audit

build: frontend:build
	go build -ldflags "-X main.version=$(VERSION) -X main.buildTime=$$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o aegis-gateway .

clean:
	rm -rf frontend/dist aegis-gateway aegis-gateway.exe
