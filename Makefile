.PHONY: build test vet check

build:
	go build -o bin/cfs ./cmd/cfs

test:
	go test ./...

vet:
	go vet ./...

check: test vet
