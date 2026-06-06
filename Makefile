VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: dev frontend frontend:dev frontend:build lint test audit verify build build-all clean

dev:
	go run .

frontend:dev:
	cd frontend && npm run dev -- --host 127.0.0.1

frontend:build:
	cd frontend && npm ci && npm run build

frontend: frontend:build

lint:
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@v0.6.1 ./...

test:
	go test -race ./...

audit:
	cd frontend && npm audit --omit=dev

verify: test audit

build: frontend:build
	go build -ldflags "-X main.version=$(VERSION) -X main.buildTime=$$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o aegis-gateway .

build-all: frontend:build
	mkdir -p dist
	GOOS=linux GOARCH=amd64 go build -o dist/aegis-gateway-linux-amd64 .
	GOOS=windows GOARCH=amd64 go build -o dist/aegis-gateway-windows-amd64.exe .
	GOOS=darwin GOARCH=amd64 go build -o dist/aegis-gateway-darwin-amd64 .

clean:
	rm -rf frontend/dist aegis-gateway aegis-gateway.exe
