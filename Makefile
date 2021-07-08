GO=go
GO111MODULE=on
CMDS=scheduler device-plugin
OUTPUT_DIR=bin

#ifeq ($(VERSION), )
#VERSION:=$(shell git describe --long --dirty --tags --match='v*')
#endif
#MAJOR_MINOR_PATCH_VERSION:=$(shell echo $(VERSION) | cut -f1 -d'-' | sed -e 's/^v//')
#MAJOR_MINOR_VERSION:=$(shell echo $(MAJOR_MINOR_PATCH_VERSION) | sed -e 's/\([0-9]*\.[0-9]*\)\..*/\1/')

all: build

build: $(CMDS)

$(CMDS):
	$(GO) build -ldflags '-s -w' -o ${OUTPUT_DIR}/$@ ./cmd/$@

clean:
	$(GO) clean -r -x ./cmd/...
	-rm -rf $(OUTPUT_DIR)

.PHONY: all build clean $(CMDS)
