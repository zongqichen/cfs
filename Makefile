.PHONY: build test vet check security smoke release-check

build:
	go build -o bin/cfs ./cmd/cfs

test:
	go test ./...

vet:
	go vet ./...

check: test vet

security:
	./scripts/check-security.sh

smoke:
	./scripts/smoke-real-cf.sh

release-check:
	./scripts/check-release-builds.sh
