.PHONY: install test test-integration lint run emulator

install:
	pip install -e ".[dev]"

emulator:
	firebase emulators:start --only firestore

test:
	USE_FIRESTORE_EMULATOR=true FIRESTORE_EMULATOR_HOST=localhost:8081 \
		pytest tests/unit -v

test-integration:
	pytest tests/integration -v -s

lint:
	ruff check app tests
	ruff format --check app tests

run:
	uvicorn app.main:app --reload --port 8080
