# Glide interpreter — canonical target set.
BIN  := bin/glide
DIST := dist

.PHONY: build test run release clean docker-build deploy logs

build:
	go build -o $(BIN) ./cmd/glide

test:
	go vet ./... && go test ./...

run: build
	./$(BIN) run examples/wordfreq.gld testdata/sample.txt

# CLI tool: macOS + linux (arm64, amd64) + windows.
# CGO_ENABLED=0 for static binaries; -s -w strips symbol tables.
release:
	CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -ldflags "-s -w" -o $(DIST)/glide-darwin-arm64 ./cmd/glide
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -ldflags "-s -w" -o $(DIST)/glide-linux-arm64  ./cmd/glide
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -ldflags "-s -w" -o $(DIST)/glide-linux-amd64  ./cmd/glide
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "-s -w" -o $(DIST)/glide-windows-amd64.exe ./cmd/glide

clean:
	rm -rf $(BIN) $(DIST)

# Not a service: no container, no deployment. Targets exist so the
# command interface matches every other repo.
docker-build:
	@echo "glide is a local CLI; nothing to containerise"

deploy:
	@echo "glide is a local CLI; nothing to deploy"

logs:
	@echo "glide is a local CLI; no service logs"
