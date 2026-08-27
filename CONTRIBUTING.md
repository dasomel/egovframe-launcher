# Contributing to egovframe-launcher

English | [한국어](CONTRIBUTING-ko.md)

## Development Setup

```bash
cd launcher
go test ./...
go run main.go
```

## Guidelines

- All Go code must pass `golangci-lint` and unit tests.
- UI styling must adhere to tokens in `DESIGN.md`.
