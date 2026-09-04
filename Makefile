.PHONY: fmt test race vet build

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

build:
	go build ./cmd/cloud-control
	go build ./cmd/edge-agent
