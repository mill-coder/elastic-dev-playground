GOROOT  ?= $(shell go env GOROOT)
WASM_OUT = web/public/parser.wasm
WASM_EXEC = web/public/wasm_exec.js

# Go 1.22+ on Ubuntu stores wasm_exec.js in misc/wasm/ instead of lib/wasm/
WASM_EXEC_SRC = $(firstword $(wildcard $(GOROOT)/lib/wasm/wasm_exec.js $(GOROOT)/../share/go-*/misc/wasm/wasm_exec.js /usr/share/go-*/misc/wasm/wasm_exec.js))

.PHONY: all clean wasm wasm-exec deps dev build registry

all: wasm wasm-exec deps build

wasm: $(WASM_OUT)
$(WASM_OUT): go/main.go go/registry.go go/validate.go go/complete.go go/go.mod $(wildcard go/registrydata/*.json)
	cd go && GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o ../$(WASM_OUT) .

wasm-exec: $(WASM_EXEC)
$(WASM_EXEC):
	cp "$(WASM_EXEC_SRC)" $(WASM_EXEC)

deps: web/node_modules
web/node_modules: web/package.json
	cd web && npm install
	touch web/node_modules

dev: wasm wasm-exec deps
	cd web && npx vite

build: wasm wasm-exec deps
	cd web && npx vite build

registry:
	@if [ -z "$(VERSION)" ]; then echo "Usage: make registry VERSION=8.19"; exit 1; fi
	cd tools/scrape-registry && go run . -version $(VERSION) -out ../../go/registrydata/$(VERSION).json

clean:
	rm -f $(WASM_OUT) $(WASM_EXEC)
	rm -rf dist web/node_modules
