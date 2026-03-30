.PHONY: build test vet clean

build:
	go build -o drydock ./cmd/drydock

test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -f drydock
	rm -rf .drydock/

check: vet test
