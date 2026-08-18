.PHONY: build test test-coverage fmt vet clean install

build:
	mkdir -p bin
	go build -o bin/lrn-mcp ./cmd/lrn-mcp
	go build -o bin/lrn ./cmd/lrn

test:
	go test -v ./...

test-coverage:
	go test -count=1 -coverprofile=coverage.out ./internal/...
	go tool cover -func=coverage.out

fmt:
	gofmt -s -w .

vet:
	go vet ./...
install: build
	cp bin/lrn-mcp $(HOME)/.local/bin/
	cp bin/lrn $(HOME)/.local/bin/
	rm -f $(HOME)/.local/bin/learning-mcp
clean:
	rm -rf bin coverage.out
