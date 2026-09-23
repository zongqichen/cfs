.PHONY: build format-check test test-race vet check security smoke release-check

build:
	go build -o bin/cfs ./cmd/cfs

format-check:
	test -z "$$(gofmt -l .)"

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

check: format-check test-race vet

security:
	./scripts/check-security.sh

smoke:
	./scripts/smoke-real-cf.sh

release-check:
	./scripts/check-release-builds.sh
