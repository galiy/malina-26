GO ?= go
DIST := dist
LDFLAGS := -s -w

.PHONY: all host arm fmt vet clean

all: host

host:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/bms-proxy .

arm:
	GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/bms-proxy-armv7 .

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

clean:
	rm -rf $(DIST)