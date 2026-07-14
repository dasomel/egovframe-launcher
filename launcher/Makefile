BINARY := egov-launcher
DIST := dist
GOFUMPT := $(shell go env GOPATH)/bin/gofumpt

# 버전은 CHANGELOG.md 최신 항목에서 가져온다 — 릴리스 파이프라인이 태깅하는
# 버전과 동일한 소스. (git describe는 CI에서 태그가 빌드 뒤에 생성되므로
# 한 버전 이전을 가리킨다.)
VERSION := v$(shell grep -oEm1 '\[[0-9]+\.[0-9]+\.[0-9]+\]' ../CHANGELOG.md | tr -d '[]')
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: fmt fmt-check test build cross run clean

fmt:
	$(GOFUMPT) -w .

fmt-check:
	@test -z "$$($(GOFUMPT) -l .)" || (echo "gofumpt needed:"; $(GOFUMPT) -l .; exit 1)

test:
	go test ./...

build:
	go build -ldflags="$(LDFLAGS)" -o $(BINARY) .

PORT := 7070

run: build
	-@lsof -ti tcp:$(PORT) | xargs kill 2>/dev/null; sleep 1; true
	./$(BINARY)

cross:
	mkdir -p $(DIST)
	# macOS
	GOOS=darwin  GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o $(DIST)/$(BINARY)-darwin-arm64 .
	GOOS=darwin  GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(DIST)/$(BINARY)-darwin-amd64 .
	# Windows
	GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(DIST)/$(BINARY)-windows-amd64.exe .
	GOOS=windows GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o $(DIST)/$(BINARY)-windows-arm64.exe .



clean:
	rm -rf $(BINARY) $(DIST) .work
