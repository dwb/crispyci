.PHONY: all devrun

DEV_SUPPORT_DIR := $(CURDIR)/.dev
DEV_WORKING_DIR := $(DEV_SUPPORT_DIR)/var
DEV_SCRIPTS_DIR := $(DEV_SUPPORT_DIR)/libexec

all:
	cd "$(CURDIR)"
	go build

$(DEV_WORKING_DIR):
	mkdir -p "$(DEV_WORKING_DIR)"

$(DEV_SCRIPTS_DIR):
	mkdir -p "$(DEV_SCRIPTS_DIR)"

devrun: $(DEV_WORKING_DIR) $(DEV_SCRIPTS_DIR) all
	exec "$(CURDIR)/crispyci" -workingDir "$(DEV_WORKING_DIR)" -scriptDir "$(DEV_SCRIPTS_DIR)"
