GO ?= go
APP := malina-web
DIST := dist
LDFLAGS := -s -w

.PHONY: all host arm pamcheck fmt vet clean

all: host

host:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP) ./cmd/malina-web

arm:
	GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP)-armv7 ./cmd/malina-web
	GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/malina-netd-armv7 ./cmd/malina-netd

netd-host:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP)-netd ./cmd/malina-netd

pamcheck:
	gcc -O2 -o $(DIST)/$(APP)-pamcheck pamcheck/pamcheck.c -ldl

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

clean:
	rm -rf $(DIST)
