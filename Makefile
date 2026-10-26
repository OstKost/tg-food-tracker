.PHONY: build test run setup clean

build:
	go build -o bin/food_tracker ./cmd/food_tracker

test:
	go test -race -v ./...

run:
	go run ./cmd/food_tracker

setup:
	go run ./cmd/food_tracker -setup

clean:
	rm -rf bin/
