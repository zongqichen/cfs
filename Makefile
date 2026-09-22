.PHONY: build test vet check smoke

build:
	go build -o bin/cfs ./cmd/cfs

test:
	go test ./...

vet:
	go vet ./...

check: test vet

smoke:
	./scripts/smoke-real-cf.sh
