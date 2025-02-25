##### Global variables #####
include version.mk Makefile.defs

all: build

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Build
.PHONY: docker-build
docker-build: ## Build docker image with the vgpu.
	docker build \
	--build-arg GOLANG_IMAGE=${GOLANG_IMAGE} \
	--build-arg TARGET_ARCH=${TARGET_ARCH} \
	--build-arg NVIDIA_IMAGE=${NVIDIA_IMAGE} \
	--build-arg DEST_DIR=${DEST_DIR} \
	--build-arg VERSION=${VERSION} \
	--build-arg GOPROXY=https://goproxy.cn,direct \
	. -f=docker/Dockerfile -t ${IMG_TAG}

.PHONY: docker-buildwithlib
docker-buildwithlib: ## Build docker image without the enterprise vgpu and vgpuvalidator.
	docker build \
	--no-cache \
	--build-arg GOLANG_IMAGE=${GOLANG_IMAGE} \
	--build-arg TARGET_ARCH=${TARGET_ARCH} \
	--build-arg NVIDIA_IMAGE=${NVIDIA_IMAGE} \
	--build-arg DEST_DIR=${DEST_DIR} \
	--build-arg VERSION=${VERSION} \
	--build-arg GOPROXY=https://goproxy.cn,direct \
	. -f=docker/Dockerfile.withlib -t ${IMG_TAG}

tidy:
	$(GO) mod tidy

.PHONY: lint
lint:
	bash hack/verify-staticcheck.sh

proto:
	$(GO) get github.com/gogo/protobuf/protoc-gen-gofast@v1.3.2
	protoc --gofast_out=plugins=grpc:. ./pkg/api/*.proto

.PHONY: build
build: tidy lint $(CMDS) $(DEVICES) ## Build hami-scheduler,hami-device-plugin,vGPUmonitor binary

$(CMDS):
	$(GO) build -ldflags '-s -w -X github.com/Project-HAMi/HAMi/pkg/version.version=$(VERSION)' -o ${OUTPUT_DIR}/$@ ./cmd/$@

$(DEVICES):
	$(GO) build -ldflags '-s -w -X github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/info.version=$(VERSION)' -o ${OUTPUT_DIR}/$@-device-plugin ./cmd/device-plugin/$@

clean:
	$(GO) clean -r -x ./cmd/...
	-rm -rf $(OUTPUT_DIR)

.PHONY: all build docker-build clean $(CMDS)

##@ Test
.PHONY: test
test:	## Unit test
	mkdir -p ./_output/coverage/
	bash hack/unit-test.sh

.PHONY: e2e-test 
e2e-test:	## e2-test
	./hack/e2e-test.sh "${E2E_TYPE}" "${KUBE_CONF}"

.PHONY: e2e-env-setup
e2e-env-setup:
	./hack/e2e-test-setup.sh


##@ Security
.PHONY: dockerfile-security
dockerfile-security:	##Scan Dockerfile security
	@ docker run --rm \
          -v $(ROOT_DIR)/.trivyignore:/.trivyignore \
          -v /tmp/trivy:/root/trivy.cache/  \
          -v $(ROOT_DIR):/tmp/src  \
          aquasec/trivy:$(TRIVY_VERSION) config --exit-code 1  --severity $(LINT_TRIVY_SEVERITY_LEVEL) /tmp/src/docker  ; \
      (($$?==0)) || { echo "error, failed to check dockerfile trivy" && exit 1 ; } ; \
      echo "dockerfile trivy check: pass"

.PHONY: chart-security
chart-security:	##Scan Charts security
	@ docker run --rm \
          -v $(ROOT_DIR)/.trivyignore:/.trivyignore \
          -v /tmp/trivy:/root/trivy.cache/  \
          -v $(ROOT_DIR):/tmp/src  \
          aquasec/trivy:$(TRIVY_VERSION) config --exit-code 1  --severity $(LINT_TRIVY_SEVERITY_LEVEL) /tmp/src/charts  ; \
      (($$?==0)) || { echo "error, failed to check chart trivy" && exit 1 ; } ; \
      echo "chart trivy check: pass"



##@ Deploy
.PHONY: helm-deploy
helm-deploy: 		##Deploy hami to the K8s cluster specified in ~/.kube/config.
	./hack/deploy-helm.sh "${E2E_TYPE}" "${KUBE_CONF}" "${HAMI_VERSION}"


.PHONY: verify
verify:
	hack/verify-all.sh

