.PHONY: test test-integration vet

test:
	go test -short ./...

test-integration:
	go test -tags integration ./...

vet:
	go vet ./...
