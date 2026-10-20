BIN := malina-arch
GOFLAGS := -trimpath -ldflags="-s -w"
.PHONY: all arm clean
all: $(BIN)
$(BIN): *.go
	go build $(GOFLAGS) -o $@ .
arm:
	GOOS=linux GOARCH=arm GOARM=7 go build $(GOFLAGS) -o dist/malina-arch-armv7 .
clean:
	rm -rf $(BIN) dist
