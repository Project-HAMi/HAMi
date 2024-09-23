##### Global variables #####
include version.mk Makefile.defs

all: build

docker:
	docker build \
	--build-arg GOLANG_IMAGE=${GOLANG_IMAGE} \
	--build-arg TARGET_ARCH=${TARGET_ARCH} \
	--build-arg NVIDIA_IMAGE=${NVIDIA_IMAGE} \
	--build-arg DEST_DIR=${DEST_DIR} \
	--build-arg GOPROXY=https://goproxy.cn,direct \
	. -f=docker/Dockerfile -t ${IMG_TAG}

dockerwithlib:
	docker build \
	--no-cache \
	--build-arg GOLANG_IMAGE=${GOLANG_IMAGE} \
	--build-arg TARGET_ARCH=${TARGET_ARCH} \
	--build-arg NVIDIA_IMAGE=${NVIDIA_IMAGE} \
	--build-arg DEST_DIR=${DEST_DIR} \
	--build-arg GOPROXY=https://goproxy.cn,direct \
	. -f=docker/Dockerfile.withlib -t ${IMG_TAG}

tidy:
	$(GO) mod tidy

proto:
	$(GO) get github.com/gogo/protobuf/protoc-gen-gofast@v1.3.2
	protoc --gofast_out=plugins=grpc:. ./pkg/api/*.proto

build: $(CMDS) $(DEVICES)

$(CMDS):
	$(GO) build -ldflags '-s -w -X github.com/Project-HAMi/HAMi/pkg/version.version=$(VERSION)' -o ${OUTPUT_DIR}/$@ ./cmd/$@

$(DEVICES):
	$(GO) build -ldflags '-s -w -X github.com/Project-HAMi/HAMi/pkg/version.version=$(VERSION)' -o ${OUTPUT_DIR}/$@-device-plugin ./cmd/device-plugin/$@

clean:
	$(GO) clean -r -x ./cmd/...
	-rm -rf $(OUTPUT_DIR)

.PHONY: all build docker clean $(CMDS)

test:
	mkdir -p ./_output/coverage/
	bash hack/unit-test.sh

lint:
	bash hack/verify-staticcheck.sh

.PHONY: verify
verify:
	hack/verify-all.sh

.PHONY: lint_dockerfile
lint_dockerfile:
	@ docker run --rm \
          -v $(ROOT_DIR)/.trivyignore:/.trivyignore \
          -v /tmp/trivy:/root/trivy.cache/  \
          -v $(ROOT_DIR):/tmp/src  \
          aquasec/trivy:$(TRIVY_VERSION) config --exit-code 1  --severity $(LINT_TRIVY_SEVERITY_LEVEL) /tmp/src/docker  ; \
      (($$?==0)) || { echo "error, failed to check dockerfile trivy" && exit 1 ; } ; \
      echo "dockerfile trivy check: pass"

.PHONY: lint_chart
lint_chart:
	@ docker run --rm \
          -v $(ROOT_DIR)/.trivyignore:/.trivyignore \
          -v /tmp/trivy:/root/trivy.cache/  \
          -v $(ROOT_DIR):/tmp/src  \
          aquasec/trivy:$(TRIVY_VERSION) config --exit-code 1  --severity $(LINT_TRIVY_SEVERITY_LEVEL) /tmp/src/charts  ; \
      (($$?==0)) || { echo "error, failed to check chart trivy" && exit 1 ; } ; \
      echo "chart trivy check: pass"