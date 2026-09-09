BINARY := egov-launcher
DIST := dist
GOFUMPT := $(shell go env GOPATH)/bin/gofumpt

# 버전은 CHANGELOG.md 최신 항목에서 가져온다 — 릴리스 파이프라인이 태깅하는
# 버전과 동일한 소스. (git describe는 CI에서 태그가 빌드 뒤에 생성되므로
# 한 버전 이전을 가리킨다.)
VERSION := v$(shell grep -oEm1 '\[[0-9]+\.[0-9]+\.[0-9]+\]' ../CHANGELOG.md | tr -d '[]')
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: fmt fmt-check lint test test-integration build cross run clean

fmt:
	$(GOFUMPT) -w .

fmt-check:
	@test -x "$(GOFUMPT)" || (echo "gofumpt not found at $(GOFUMPT); install with: go install mvdan.cc/gofumpt@v0.12.0"; exit 1)
	@files="$$($(GOFUMPT) -l .)"; \
	baseline=".gofumpt-baseline"; \
	unexpected=""; \
	for file in $$files; do \
		if ! grep -Fxq "$$file" "$$baseline"; then unexpected="$$unexpected $$file"; fi; \
	done; \
	if test -n "$$unexpected"; then \
		echo "gofumpt needed outside the recorded legacy baseline:"; \
		for file in $$unexpected; do echo "$$file"; done; \
		echo; $(GOFUMPT) -d $$unexpected; exit 1; \
	fi; \
	stale=0; \
	while IFS= read -r expected; do \
		case "$$expected" in ''|'#'*) continue ;; esac; \
		if ! printf '%s\n' "$$files" | grep -Fxq "$$expected"; then \
			echo "stale gofumpt baseline entry (remove it): $$expected"; stale=1; \
		fi; \
	done < "$$baseline"; \
	test "$$stale" -eq 0

lint:
	go vet ./...

test:
	go test -short ./...

test-integration:
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
