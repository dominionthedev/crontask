.PHONY: build test install clean

PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin

build:
	go build -ldflags="-s -w" -o crontask .

test:
	go test ./...

install: build
	install -m 755 crontask $(BINDIR)/crontask

clean:
	rm -f crontask

# Cross-compile release binaries
release:
	GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o dist/crontask-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o dist/crontask-darwin-arm64 .
	GOOS=linux  GOARCH=amd64 go build -ldflags="-s -w" -o dist/crontask-linux-amd64 .
	GOOS=linux  GOARCH=arm64 go build -ldflags="-s -w" -o dist/crontask-linux-arm64 .
