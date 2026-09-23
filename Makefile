.PHONY: build format-check test test-race vet check security smoke e2e release-check

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

e2e:
	go test -count=1 -tags=e2e -v ./test/e2e

release-check:
	./scripts/check-release-builds.sh
