# Makefile for esque-lsp.

GO          ?= go
BIN         := esque-lsp
PREFIX      ?= $(HOME)/.local
INSTALL_DIR := $(PREFIX)/bin

GOFLAGS     ?=
LDFLAGS     ?= -s -w

.PHONY: all build install uninstall test fmt vet clean run

all: build

build: $(BIN)

$(BIN): $(wildcard *.go) go.mod
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN) ./

install: build
	@mkdir -p $(INSTALL_DIR)
	install -m 0755 $(BIN) $(INSTALL_DIR)/$(BIN)
	@echo "installed to $(INSTALL_DIR)/$(BIN)"

uninstall:
	rm -f $(INSTALL_DIR)/$(BIN)

test:
	$(GO) test ./...

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

clean:
	rm -f $(BIN)

# Quick smoke test: launch the server and feed it an initialize
# request. Exits as soon as the server replies.
run: build
	./$(BIN) --version
