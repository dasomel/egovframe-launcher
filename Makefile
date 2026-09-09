.PHONY: help fmt-check test build verify cross clean

help:
	@printf '%s\n' \
	  'make verify     Run formatting, tests, and current-platform build' \
	  'make test       Run Go tests' \
	  'make build      Build the current-platform launcher' \
	  'make fmt-check  Check Go formatting with the project formatter' \
	  'make cross      Build supported macOS/Windows binaries' \
	  'make clean      Remove launcher build artifacts'

fmt-check:
	$(MAKE) -C launcher fmt-check

test:
	$(MAKE) -C launcher test

build:
	$(MAKE) -C launcher build

verify: fmt-check test build

cross:
	$(MAKE) -C launcher cross

clean:
	$(MAKE) -C launcher clean
