.PHONY: run build test vet fmt generate preview screens icon store extensions package clean

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

store:
	go run ./tools/makestore

extensions:
	./extensions/build.sh

clean:
	rm -rf build dist .mygo downmygo
