.PHONY: build web test dev clean

build: web
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/hmea-panel ./cmd/hmea-panel

web:
	cd web && npm install --no-audit --no-fund && npm run build && touch dist/.gitkeep

test:
	go vet ./... && go test ./...

# Runs the panel without root; every system change is simulated under data/devroot.
dev: build
	./bin/hmea-panel -dev

clean:
	rm -rf bin data web/node_modules
