##### Global variables #####
include version.mk

all: build

docker:
	docker build \
	--build-arg GOLANG_IMAGE=${GOLANG_IMAGE} \
	--build-arg TARGET_ARCH=${TARGET_ARCH} \
	--build-arg NVIDIA_IMAGE=${NVIDIA_IMAGE} \
	--build-arg DEST_DIR=${DEST_DIR} \
	. -f=docker/Dockerfile -t ${IMG_TAG}

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
