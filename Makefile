.PHONY: build test test-integration lint run docker

build:
	go build -o bin/server ./cmd/server

test:
	go test ./...

# Real Vertex AI calls; needs ADC and GCP_PROJECT_ID.
test-integration:
	go test -tags integration -v -timeout 25m ./internal/research

lint:
	go vet ./...
	test -z "$$(gofmt -l .)"

run:
	set -a; [ -f .env ] && . ./.env; set +a; go run ./cmd/server

docker:
	docker build -t line-research-bot .
