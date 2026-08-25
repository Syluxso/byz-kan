BINARY=byz-kan

.PHONY: build run test tidy

tidy:
	go mod tidy

build:
	CGO_ENABLED=0 go build -o $(BINARY) .

run: build
	./$(BINARY)

test:
	go test ./...
