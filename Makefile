.PHONY: build run test clean format

build:
	go build -o bin/server .

run:
	go run .

test:
	go test -v ./...

clean:
	rm -rf bin

format:
	go fmt ./...
