BINARY   := statsagent
VERSION  := 1.0.0
IMAGE    ?= statsagent
LDFLAGS  := -ldflags="-s -w -X main.version=$(VERSION)"

.PHONY: build build-linux tidy docker clean

build:
	go build $(LDFLAGS) -o $(BINARY) .

build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY)-linux .

tidy:
	go mod tidy

docker:
	docker build -f deploy/Dockerfile -t $(IMAGE):$(VERSION) -t $(IMAGE):latest .

clean:
	rm -f $(BINARY) $(BINARY)-linux

test-once:
	go run . once

test-serve:
	go run . serve --port 9101
