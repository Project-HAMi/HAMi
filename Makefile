GO=go
GO111MODULE=on
CMDS=scheduler vGPUmonitor
DEVICES=mlu nvidia
OUTPUT_DIR=bin

VERSION ?= unknown

all: build

docker:
	docker build . -f=docker/Dockerfile

tidy:
	$(GO) mod tidy

proto:
	$(GO) get github.com/gogo/protobuf/protoc-gen-gofast@v1.3.2
	protoc --gofast_out=plugins=grpc:. ./pkg/api/*.proto

build: $(CMDS) $(DEVICES)

$(CMDS):
	$(GO) build -ldflags '-s -w -X 4pd.io/k8s-vgpu/pkg/version.version=$(VERSION)' -o ${OUTPUT_DIR}/$@ ./cmd/$@

$(DEVICES):
	$(GO) build -ldflags '-s -w -X 4pd.io/k8s-vgpu/pkg/version.version=$(VERSION)' -o ${OUTPUT_DIR}/$@-device-plugin ./cmd/device-plugin/$@

clean:
	$(GO) clean -r -x ./cmd/...
	-rm -rf $(OUTPUT_DIR)

.PHONY: all build docker clean $(CMDS)
