.PHONY: build install clean test tidy

BIN_DIR ?= $(HOME)/.local/bin
CLAUDE_DIR ?= $(HOME)/.claude/dross

build:
	go build -o dross ./cmd/dross

tidy:
	go mod tidy

test:
	go test ./...

install: build
	mkdir -p $(BIN_DIR) $(CLAUDE_DIR)/commands $(CLAUDE_DIR)/prompts
	cp dross $(BIN_DIR)/dross
	# Symlink assets so edits in this repo apply immediately
	ln -sfn $(CURDIR)/assets/commands $(CLAUDE_DIR)/commands.linked || true
	ln -sfn $(CURDIR)/assets/prompts  $(CLAUDE_DIR)/prompts.linked  || true
	@echo "Installed dross to $(BIN_DIR)/dross"
	@echo "Linked assets at $(CLAUDE_DIR)/commands.linked, $(CLAUDE_DIR)/prompts.linked"
	@echo "If Claude Code reads from commands/ and prompts/ directly, copy or rename."

clean:
	rm -f dross
	rm -rf dist/
