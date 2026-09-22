.PHONY: build test vet check smoke release-check

build:
	go build -o bin/cfs ./cmd/cfs

test:
	go test ./...

vet:
	go vet ./...

check: test vet

smoke:
	./scripts/smoke-real-cf.sh

release-check:
	./scripts/check-release-builds.sh
