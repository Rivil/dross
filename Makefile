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

# Sanity-check the install: every slash command's @-imports resolve to a real file.
doctor:
	@echo "Checking dross install..."
	@command -v dross >/dev/null 2>&1 && echo "✓ dross binary on PATH" || echo "✗ dross not on PATH (try: export PATH=\"$(BIN_DIR):\$$PATH\")"
	@for cmd in $(CURDIR)/assets/commands/dross-*.md; do \
		name=$$(basename $$cmd .md); \
		skill_md=$(SKILLS_DIR)/$$name/SKILL.md; \
		if [ -e "$$skill_md" ]; then \
			echo "✓ /$$name → $$skill_md"; \
		else \
			echo "✗ /$$name not installed (run: make install)"; \
		fi; \
	done
	@for p in $(CURDIR)/assets/prompts/*.md; do \
		name=$$(basename $$p); \
		target=$(PROMPTS_DIR)/$$name; \
		if [ -e "$$target" ]; then \
			echo "✓ prompt $$name → $$target"; \
		else \
			echo "✗ prompt $$name not linked"; \
		fi; \
	done

clean:
	rm -f dross
	rm -rf dist/
