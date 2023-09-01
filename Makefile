OWNER = anchore
PROJECT = binny

BINNY_VERSION = v0.1.0

TOOL_DIR = .tool
BINNY = $(TOOL_DIR)/binny
TASK = $(TOOL_DIR)/task

.DEFAULT_GOAL := default

.PHONY: default
default: $(TASK)
	$(TASK) $@

$(BINNY):
	@mkdir -p $(TOOL_DIR)
	curl -sSfL https://raw.githubusercontent.com/$(OWNER)/$(PROJECT)/main/install.sh | sh -s -- -b $(TOOL_DIR) $(BINNY_VERSION)

$(TASK): $(BINNY)
	$(BINNY) install task

# for those of us that can't seem to kick the habit of typing `make ...`
# assume that any other target is a task in the taskfile.
%: $(TASK)
	$(TASK) $@

help: $(TASK)
	$(TASK) -l
