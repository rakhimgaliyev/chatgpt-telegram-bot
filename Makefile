SHELL := /bin/bash

BIN ?= main
CMD ?= ./cmd/bot

.PHONY: build run tidy test clean

build:
	go build -o bin/$(BIN) $(CMD)

run:
	go run $(CMD)

tidy:
	go mod tidy

test:
	go test ./...

clean:
	rm -rf bin/$(BIN)
