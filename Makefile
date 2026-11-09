.PHONY: run build test vet fmt generate preview screens icon package clean

run:
	go run .

build:
	go build -o downmygo .

package:
	go tool mygo build

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

generate:
	go tool mygo generate

preview:
	go run ./cmd/preview docs/screens

screens: preview

icon:
	go run ./tools/makeicon

clean:
	rm -rf build dist .mygo downmygo
