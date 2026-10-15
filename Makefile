BINARY_NAME := sapi
INSTALL_DIR := $(HOME)/bin
MAN_DIR     := $(HOME)/.local/share/man/man1
GO          := go
VERSION     ?= $(shell dpkg-parsechangelog -S Version 2>/dev/null | cut -d- -f1 || echo "1.0.0")
DEB_REV     ?= 1
ARCH        ?= $(shell dpkg --print-architecture 2>/dev/null || echo "amd64")
LDFLAGS     := -s -w -X github.com/laurentL/salt-api-cli/internal/cli.Version=$(VERSION)

.PHONY: all build vet test man deb install clean

all: vet test build man

vet:
	$(GO) vet ./...

build:
	CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) ./cmd/sapi

test:
	$(GO) test -v -race ./...

man: build
	./$(BINARY_NAME) man --dir man

deb: build man
	@mkdir -p dist
	@STAGE=$$(mktemp -d); \
	trap 'rm -rf "$$STAGE"' EXIT; \
	chmod 0755 "$$STAGE"; \
	mkdir -p "$$STAGE/DEBIAN" \
	         "$$STAGE/usr/bin" \
	         "$$STAGE/usr/share/man/man1" \
	         "$$STAGE/usr/share/doc/$(BINARY_NAME)" \
	         "$$STAGE/usr/share/bash-completion/completions" \
	         "$$STAGE/usr/share/zsh/vendor-completions" \
	         "$$STAGE/usr/share/fish/vendor_completions.d"; \
	install -m 0755 $(BINARY_NAME) "$$STAGE/usr/bin/$(BINARY_NAME)"; \
	for f in man/*.1; do \
		gzip -9 -n -c "$$f" > "$$STAGE/usr/share/man/man1/$$(basename "$$f").gz"; \
	done; \
	./$(BINARY_NAME) completion bash > "$$STAGE/usr/share/bash-completion/completions/$(BINARY_NAME)"; \
	./$(BINARY_NAME) completion zsh > "$$STAGE/usr/share/zsh/vendor-completions/_$(BINARY_NAME)"; \
	./$(BINARY_NAME) completion fish > "$$STAGE/usr/share/fish/vendor_completions.d/$(BINARY_NAME).fish"; \
	install -m 0644 README.md config.example.toml "$$STAGE/usr/share/doc/$(BINARY_NAME)/"; \
	install -m 0644 debian/copyright "$$STAGE/usr/share/doc/$(BINARY_NAME)/copyright"; \
	gzip -9 -n -c debian/changelog > "$$STAGE/usr/share/doc/$(BINARY_NAME)/changelog.gz"; \
	find "$$STAGE/usr" -type d -exec chmod 0755 {} +; \
	find "$$STAGE/usr/share" -type f -exec chmod 0644 {} +; \
	INSTALLED_KB=$$(du -sk "$$STAGE/usr" | cut -f1); \
	{ \
		printf "Package: %s\nVersion: %s-%s\nSection: admin\nPriority: optional\n" "$(BINARY_NAME)" "$(VERSION)" "$(DEB_REV)"; \
		printf "Architecture: %s\nInstalled-Size: %s\nDepends: ca-certificates\n" "$(ARCH)" "$$INSTALLED_KB"; \
		printf "Maintainer: Laurent LABATUT <laurent@labatut.net>\nHomepage: https://github.com/laurentL/salt-api-cli\n"; \
		printf "Description: SaltStack salt-api hybrid CLI client and Full-Access MCP Server\n"; \
		printf " sapi is a fast, standalone command-line client wrapping SaltStack's salt-api\n"; \
		printf " (netapi: rest_cherrypy / rest_tornado) with an integrated Full-Access MCP\n"; \
		printf " server over stdio.\n"; \
	} > "$$STAGE/DEBIAN/control"; \
	DEB_OUT="dist/$(BINARY_NAME)_$(VERSION)-$(DEB_REV)_$(ARCH).deb"; \
	dpkg-deb --root-owner-group --build "$$STAGE" "$$DEB_OUT"; \
	echo "Built Debian package: $$DEB_OUT"

install: build man
	mkdir -p $(INSTALL_DIR) $(MAN_DIR)
	install -m 0755 $(BINARY_NAME) $(INSTALL_DIR)/$(BINARY_NAME)
	install -m 0644 man/*.1 $(MAN_DIR)/
	@echo "Installed $(INSTALL_DIR)/$(BINARY_NAME) and man pages in $(MAN_DIR)"

clean:
	rm -rf $(BINARY_NAME) dist/ completions/ .gocache/ debian/sapi/ debian/.debhelper/ debian/files debian/*.debhelper* debian/*.substvars
