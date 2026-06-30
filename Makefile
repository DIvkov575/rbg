# rbg — remote Claude agent management
#
# Common targets:
#   make build      build the rbg client into ./bin/rbg
#   make install    install the rbg client to $(PREFIX)  (default ~/.local/bin)
#   make uninstall  remove the installed client
#   make test       run unit tests
#   make test-all   run unit + integration tests (needs sshd + go on this host)
#   make deploy     install the agent on the remote desktop (needs RBG_HOST)
#   make fmt vet    format / vet
#
# Override the install dir:  make install PREFIX=/usr/local/bin

PREFIX ?= $(HOME)/.local/bin
BINDIR := ./bin
CLIENT := $(BINDIR)/rbg

.PHONY: build install uninstall test test-all fmt vet deploy clean

build:
	@mkdir -p $(BINDIR)
	go build -o $(CLIENT) ./cmd/rbg
	@echo "built $(CLIENT)"

install: build
	@mkdir -p $(PREFIX)
	install -m 0755 $(CLIENT) $(PREFIX)/rbg
	@echo "installed $(PREFIX)/rbg"
	@case ":$$PATH:" in \
	  *":$(PREFIX):"*) echo "$(PREFIX) is on your PATH — run: rbg" ;; \
	  *) echo "WARNING: $(PREFIX) is not on your PATH; add it to use 'rbg' directly" ;; \
	esac

uninstall:
	rm -f $(PREFIX)/rbg
	@echo "removed $(PREFIX)/rbg"

test:
	go test ./...

test-all:
	go test ./...
	pytest -m integration

fmt:
	gofmt -w .

vet:
	go vet ./...

# Install/update the agent binary on the remote desktop. Requires RBG_HOST
# (and any RBG_SSH/RBG_CWD) in the environment, same as the rbg client.
deploy: build
	$(CLIENT) deploy

clean:
	rm -rf $(BINDIR)
