.PHONY: all devrun

CRISPYCI := $(CURDIR)/crispyci
DEV_SUPPORT_DIR := $(CURDIR)/.dev
DEV_WORKING_DIR := $(DEV_SUPPORT_DIR)/var
DEV_SCRIPTS_DIR := $(DEV_SUPPORT_DIR)/libexec
WWW_DIR := $(CURDIR)/www

all:
	cd "$(CURDIR)"
	go build

clean:
	rm "$(CRISPYCI)"
	rm -rf "$(DEV_SUPPORT_DIR)"

$(DEV_WORKING_DIR):
	mkdir -p "$(DEV_WORKING_DIR)"

$(DEV_SCRIPTS_DIR):
	mkdir -p "$(DEV_SCRIPTS_DIR)"
	ln -s "$(WWW_DIR)" "$(DEV_SCRIPTS_DIR)/www"

devrun: $(DEV_WORKING_DIR) $(DEV_SCRIPTS_DIR) all
	exec "$(CRISPYCI)" -workingDir "$(DEV_WORKING_DIR)" -scriptDir "$(DEV_SCRIPTS_DIR)"
