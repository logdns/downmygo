.PHONY: run build test vet fmt generate preview screens icon store site extensions package clean

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

site:
	mkdir -p site/img
	cp resources/icon.png site/img/icon.png
	cp docs/screens/main-light.png site/img/main-light.png
	cp docs/screens/main-dark.png site/img/main-dark.png
	cp docs/screens/main-light-zh-Hans.png site/img/main-zh.png
	cp docs/screens/main-narrow.png site/img/main-narrow.png

extensions:
	./extensions/build.sh

clean:
	rm -rf build dist .mygo downmygo
