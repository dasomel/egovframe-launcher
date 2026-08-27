# egovframe-launcher 기여 가이드 (Contributing Guide)

[English](CONTRIBUTING.md) | 한국어

## 로컬 개발 및 테스트

```bash
cd launcher
go test ./...
go run main.go
```

## 기여 지침

- Go 코드는 `golangci-lint` 및 단위 테스트를 통과해야 합니다.
- UI 스타일은 `DESIGN.md`의 시맨틱 토큰을 준수합니다.
