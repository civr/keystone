.PHONY: test test-integration vet web web-test web-fixtures

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

# Settings fixture for the web settingsMeta test (commit the result).
web-fixtures:
	mkdir -p web/src/lib/fixtures
	go run ./cmd/keystone-core print-defaults > web/src/lib/fixtures/defaults.json
