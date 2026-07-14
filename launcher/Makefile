BINARY := egov-launcher
DIST := dist
GOFUMPT := $(shell go env GOPATH)/bin/gofumpt

.PHONY: fmt fmt-check test build cross run clean

fmt:
	$(GOFUMPT) -w .

fmt-check:
	@test -z "$$($(GOFUMPT) -l .)" || (echo "gofumpt needed:"; $(GOFUMPT) -l .; exit 1)

test:
	go test ./...

build:
	go build -o $(BINARY) .

PORT := 7070

run: build
	-@lsof -ti tcp:$(PORT) | xargs kill 2>/dev/null; sleep 1; true
	./$(BINARY)

cross:
	GOOS=darwin  GOARCH=arm64 go build -o $(DIST)/$(BINARY)-darwin-arm64 .
	GOOS=darwin  GOARCH=amd64 go build -o $(DIST)/$(BINARY)-darwin-amd64 .
	GOOS=windows GOARCH=amd64 go build -o $(DIST)/$(BINARY)-windows-amd64.exe .

clean:
	rm -rf $(BINARY) $(DIST) .work
