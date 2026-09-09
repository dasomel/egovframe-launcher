.PHONY: help fmt-check lint test build verify cross clean

help:
	@printf '%s\n' \
	  'make verify     Run formatting, lint, tests, and current-platform build' \
	  'make fmt-check  Check Go formatting with the project formatter' \
	  'make lint       Run Go vet static checks' \
	  'make test       Run Go tests' \
	  'make build      Build the current-platform launcher' \
	  'make cross      Build supported macOS/Windows binaries' \
	  'make clean      Remove launcher build artifacts'

fmt-check:
	$(MAKE) -C launcher fmt-check

lint:
	$(MAKE) -C launcher lint

test:
	$(MAKE) -C launcher test

build:
	$(MAKE) -C launcher build

verify: fmt-check lint test build

cross:
	$(MAKE) -C launcher cross

clean:
	$(MAKE) -C launcher clean
