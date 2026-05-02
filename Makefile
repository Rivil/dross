.PHONY: build install uninstall clean test tidy doctor

BIN_DIR     ?= $(HOME)/.local/bin
SKILLS_DIR  ?= $(HOME)/.claude/skills
PROMPTS_DIR ?= $(HOME)/.claude/dross/prompts

build:
	go build -o dross ./cmd/dross

tidy:
	go mod tidy

test:
	go test -count=1 ./...

# Symlink-based dev install: edits to assets/ apply immediately.
# Slash commands become skills under $(SKILLS_DIR)/dross-<name>/SKILL.md.
# Prompt files referenced by @-imports live at $(PROMPTS_DIR)/<name>.md.
install: build
	@mkdir -p $(BIN_DIR)
	@cp dross $(BIN_DIR)/dross
	@echo "binary  → $(BIN_DIR)/dross"
	@mkdir -p $(SKILLS_DIR)
	@for cmd in $(CURDIR)/assets/commands/dross-*.md; do \
		name=$$(basename $$cmd .md); \
		skill_dir=$(SKILLS_DIR)/$$name; \
		mkdir -p $$skill_dir; \
		ln -sfn $$cmd $$skill_dir/SKILL.md; \
		echo "skill   → $$skill_dir/SKILL.md"; \
	done
	@mkdir -p $(dir $(PROMPTS_DIR))
	@ln -sfn $(CURDIR)/assets/prompts $(PROMPTS_DIR)
	@echo "prompts → $(PROMPTS_DIR) → $(CURDIR)/assets/prompts"
	@echo ""
	@echo "Done. If $(BIN_DIR) isn't in PATH, add to your shell rc:"
	@echo "  export PATH=\"$(BIN_DIR):\$$PATH\""
	@echo ""
	@echo "Then in any Claude Code session:"
	@echo "  /dross-init     greenfield bootstrap"
	@echo "  /dross-onboard  adopt existing repo"

uninstall:
	@if [ -z "$(SKILLS_DIR)" ]; then echo "refusing — SKILLS_DIR is empty"; exit 1; fi
	rm -f $(BIN_DIR)/dross
	@for d in $(SKILLS_DIR)/dross-*; do \
		[ -d "$$d" ] && rm -rf "$$d" && echo "removed $$d"; \
	done
	rm -f $(PROMPTS_DIR)
	@echo "Uninstalled. If $(PROMPTS_DIR)'s parent (~/.claude/dross) is empty you may want to remove it manually."

# Sanity-check the install. Detects: stale binary (sources newer than the
# installed binary), PATH dross resolving somewhere other than $(BIN_DIR),
# slash commands not symlinked or pointing into a different checkout, and
# missing prompts. Exits non-zero on any issue so CI can use it.
doctor:
	@issues=0; \
	echo "Checking dross install..."; \
	echo ""; \
	echo "Binary:"; \
	if command -v dross >/dev/null 2>&1; then \
		path_bin=$$(command -v dross); \
		echo "  ✓ on PATH at $$path_bin"; \
		if [ "$$path_bin" != "$(BIN_DIR)/dross" ]; then \
			echo "  ⚠ \$$(which dross) differs from \$$(BIN_DIR)/dross ($(BIN_DIR)/dross)"; \
			echo "    'make install' writes to $(BIN_DIR) — your PATH dross will not be updated"; \
			issues=$$((issues+1)); \
		fi; \
	else \
		echo "  ✗ not on PATH"; \
		issues=$$((issues+1)); \
		if [ -x "$(BIN_DIR)/dross" ]; then \
			echo "    binary exists at $(BIN_DIR)/dross — add to PATH:"; \
			echo "      export PATH=\"$(BIN_DIR):\$$PATH\""; \
		fi; \
	fi; \
	if [ -x "$(BIN_DIR)/dross" ]; then \
		newer=$$(find $(CURDIR)/cmd $(CURDIR)/internal -name '*.go' -newer $(BIN_DIR)/dross 2>/dev/null | wc -l | tr -d ' '); \
		if [ "$$newer" -gt 0 ]; then \
			echo "  ⚠ $$newer .go file(s) newer than $(BIN_DIR)/dross — run 'make install' to rebuild"; \
			issues=$$((issues+1)); \
		fi; \
	fi; \
	echo ""; \
	echo "Slash commands:"; \
	cmd_total=0; cmd_ok=0; \
	for cmd in $(CURDIR)/assets/commands/dross-*.md; do \
		cmd_total=$$((cmd_total+1)); \
		name=$$(basename $$cmd .md); \
		skill_md=$(SKILLS_DIR)/$$name/SKILL.md; \
		if [ ! -e "$$skill_md" ] && [ ! -L "$$skill_md" ]; then \
			echo "  ✗ /$$name missing — expected $$skill_md"; \
			issues=$$((issues+1)); \
		elif [ -L "$$skill_md" ]; then \
			target=$$(readlink "$$skill_md"); \
			if [ "$$target" = "$$cmd" ]; then \
				cmd_ok=$$((cmd_ok+1)); \
			else \
				echo "  ⚠ /$$name → $$target"; \
				echo "    expected      $$cmd (different checkout?)"; \
				issues=$$((issues+1)); \
			fi; \
		else \
			echo "  ⚠ /$$name is a regular file, not a symlink — re-run 'make install'"; \
			issues=$$((issues+1)); \
		fi; \
	done; \
	echo "  $$cmd_ok/$$cmd_total slash commands correctly linked"; \
	echo ""; \
	echo "Prompts:"; \
	p_total=0; p_ok=0; \
	for p in $(CURDIR)/assets/prompts/*.md; do \
		p_total=$$((p_total+1)); \
		name=$$(basename $$p); \
		target=$(PROMPTS_DIR)/$$name; \
		if [ -e "$$target" ] || [ -L "$$target" ]; then \
			p_ok=$$((p_ok+1)); \
		else \
			echo "  ✗ prompt $$name missing at $$target"; \
			issues=$$((issues+1)); \
		fi; \
	done; \
	echo "  $$p_ok/$$p_total prompts present"; \
	echo ""; \
	if [ $$issues -eq 0 ]; then \
		echo "All checks passed."; \
	else \
		echo "$$issues issue(s) found."; \
		exit 1; \
	fi

clean:
	rm -f dross
	rm -rf dist/
