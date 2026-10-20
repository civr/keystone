.PHONY: test test-integration vet web web-test

test:
	go test -short ./...

test-integration:
	go test -tags integration ./...

vet:
	go vet ./...

web:
	cd web && npm ci && npm run build

web-test:
	cd web && npm ci && npm test && npm run build && node scripts/check-size.mjs
