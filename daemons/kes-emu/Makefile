GO ?= go
DIST := dist
LDFLAGS := -s -w

.PHONY: all host arm clean
all: host
host:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/kes-emu .
arm:
	GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/kes-emu-armv7 .
clean:
	rm -rf $(DIST)
