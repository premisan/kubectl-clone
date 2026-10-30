.PHONY: all build test install clean

BIN_DIR := bin
BINARY_NAME := kubectl-clone
INSTALL_PATH ?= $(HOME)/.local/bin

all: test build

build:
	@mkdir -p $(BIN_DIR)
	go build -ldflags="-s -w" -o $(BIN_DIR)/$(BINARY_NAME) ./cmd/kubectl-clone

test:
	go test -v -race ./pkg/...

install: build
	@echo "Installing $(BINARY_NAME) to $(INSTALL_PATH)..."
	@mkdir -p $(INSTALL_PATH)
	cp $(BIN_DIR)/$(BINARY_NAME) $(INSTALL_PATH)/$(BINARY_NAME)
	chmod 755 $(INSTALL_PATH)/$(BINARY_NAME)
	ln -sf $(BINARY_NAME) $(INSTALL_PATH)/kubectl_complete-clone
	@echo "Successfully installed! You can now run: kubectl clone --help"

clean:
	rm -rf $(BIN_DIR)
