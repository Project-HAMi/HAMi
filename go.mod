module github.com/Project-HAMi/HAMi

<<<<<<< HEAD
go 1.25.5
=======
go 1.21
>>>>>>> c7a3893 (Remake this repo to HAMi)

require (
<<<<<<< HEAD
	github.com/NVIDIA/go-gpuallocator v0.6.0
	github.com/NVIDIA/go-nvlib v0.9.0
	github.com/NVIDIA/go-nvml v0.13.0-1
	github.com/NVIDIA/k8s-device-plugin v0.18.1
	github.com/NVIDIA/nvidia-container-toolkit v1.18.1
	github.com/ccoveille/go-safecast v1.8.2
	github.com/fsnotify/fsnotify v1.9.0
	github.com/google/uuid v1.6.0
	github.com/imdario/mergo v0.3.16
	github.com/julienschmidt/httprouter v1.3.0
	github.com/onsi/ginkgo/v2 v2.27.5
	github.com/onsi/gomega v1.39.0
	github.com/prometheus/client_golang v1.23.2
	github.com/sirupsen/logrus v1.9.4
	github.com/spf13/cobra v1.10.2
	github.com/spf13/pflag v1.0.10
	github.com/stretchr/testify v1.11.1
	github.com/urfave/cli/v2 v2.27.7
	golang.org/x/net v0.49.0
	golang.org/x/term v0.39.0
	golang.org/x/tools v0.41.0
	google.golang.org/grpc v1.78.0
	gopkg.in/yaml.v2 v2.4.0
	gopkg.in/yaml.v3 v3.0.1
	gotest.tools/v3 v3.5.2
	k8s.io/api v0.33.0
	k8s.io/apimachinery v0.33.0
	k8s.io/client-go v0.33.0
	k8s.io/klog/v2 v2.130.1
	k8s.io/kube-scheduler v0.28.3
	k8s.io/kubelet v0.32.3
	sigs.k8s.io/controller-runtime v0.21.0
	tags.cncf.io/container-device-interface v1.1.0
	tags.cncf.io/container-device-interface/specs-go v1.1.0
)

require (
	github.com/Masterminds/semver/v3 v3.4.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.7 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/emicklei/go-restful/v3 v3.11.3 // indirect
	github.com/evanphx/json-patch v5.9.0+incompatible // indirect
	github.com/fxamacker/cbor/v2 v2.7.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-openapi/jsonpointer v0.21.0 // indirect
	github.com/go-openapi/jsonreference v0.20.4 // indirect
	github.com/go-openapi/swag v0.23.0 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/gnostic-models v0.6.9 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/pprof v0.0.0-20250403155104-27863c87afa6 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/moby/sys/capability v0.4.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/opencontainers/runtime-spec v1.3.0 // indirect
	github.com/opencontainers/runtime-tools v0.9.1-0.20251114084447-edf4cb3d2116 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.66.1 // indirect
	github.com/prometheus/procfs v0.17.0 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xrash/smetrics v0.0.0-20240521201337-686a1a2994c1 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/mod v0.32.0 // indirect
	golang.org/x/oauth2 v0.32.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/sys v0.40.0 // indirect
	golang.org/x/telemetry v0.0.0-20260109210033-bd525da824e2 // indirect
	golang.org/x/text v0.33.0 // indirect
	golang.org/x/time v0.9.0 // indirect
	gomodules.xyz/jsonpatch/v2 v2.4.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251029180050-ab9386a59fda // indirect
	google.golang.org/protobuf v1.36.10 // indirect
	gopkg.in/evanphx/json-patch.v4 v4.12.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	k8s.io/kube-openapi v0.0.0-20250318190949-c8a335a9a2ff // indirect
	k8s.io/utils v0.0.0-20241104100929-3ea5e8cea738 // indirect
	sigs.k8s.io/json v0.0.0-20241010143419-9aa6b5e7a4b3 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.6.0 // indirect
	sigs.k8s.io/yaml v1.4.0 // indirect
)

replace (
	github.com/Project-HAMi/HAMi/pkg/api => ./pkg/api
	github.com/Project-HAMi/HAMi/pkg/device-plugin => ./pkg/device-plugin
	github.com/Project-HAMi/HAMi/test/utils => ./test/utils
	k8s.io/api => k8s.io/api v0.31.10
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.31.10
	k8s.io/apimachinery => k8s.io/apimachinery v0.31.10
	k8s.io/apiserver => k8s.io/apiserver v0.31.10
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.31.10
	k8s.io/client-go => k8s.io/client-go v0.31.10
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.31.10
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.31.10
	k8s.io/code-generator => k8s.io/code-generator v0.31.10
	k8s.io/component-base => k8s.io/component-base v0.31.10
	k8s.io/component-helpers => k8s.io/component-helpers v0.31.10
	k8s.io/cri-api => k8s.io/cri-api v0.31.10
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.31.10
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.31.10
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.31.10
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.31.10
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.31.10
	k8s.io/kubectl => k8s.io/kubectl v0.31.10
	k8s.io/kubelet => k8s.io/kubelet v0.31.10
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.31.10
	k8s.io/metrics => k8s.io/metrics v0.31.10
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.31.10
=======
	github.com/NVIDIA/go-gpuallocator v0.2.3
	github.com/NVIDIA/go-nvlib v0.0.0-20231212194527-f3264c8a6a7a
	github.com/NVIDIA/go-nvml v0.12.0-1.0.20231020145430-e06766c5e74f
	github.com/NVIDIA/k8s-device-plugin v0.14.1
	github.com/NVIDIA/nvidia-container-toolkit v1.14.4-0.20231115203935-5d7ee25b37e2
	github.com/container-orchestrated-devices/container-device-interface v0.5.4-0.20230111111500-5b3b5d81179a
	github.com/fsnotify/fsnotify v1.6.0
	github.com/golang/glog v1.1.0
	github.com/golang/mock v1.6.0
	github.com/google/uuid v1.4.0
	github.com/jessevdk/go-flags v1.5.0
	github.com/julienschmidt/httprouter v1.3.0
	github.com/kubevirt/device-plugin-manager v1.19.5
	github.com/onsi/ginkgo v1.16.5
	github.com/onsi/gomega v1.27.10
	github.com/opencontainers/runtime-spec v1.1.0
	github.com/prometheus/client_golang v1.16.0
	github.com/sirupsen/logrus v1.9.3
	github.com/spf13/cobra v1.7.0
	github.com/stretchr/testify v1.8.4
	github.com/urfave/cli/v2 v2.4.0
	golang.org/x/exp v0.0.0-20220827204233-334a2380cb91
	golang.org/x/net v0.17.0
	google.golang.org/grpc v1.56.0
	google.golang.org/protobuf v1.30.0
	gotest.tools/v3 v3.4.0
	k8s.io/api v0.28.3
	k8s.io/apimachinery v0.28.3
	k8s.io/client-go v0.28.3
	k8s.io/klog v1.0.0
	k8s.io/klog/v2 v2.100.1
	k8s.io/kube-scheduler v0.28.3
	k8s.io/kubelet v0.28.3
	sigs.k8s.io/controller-runtime v0.16.3
	tags.cncf.io/container-device-interface v0.6.2
)

require (
	github.com/NVIDIA/gpu-monitoring-tools v0.0.0-20201222072828-352eb4c503a7 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.2 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/emicklei/go-restful/v3 v3.11.0 // indirect
	github.com/go-logr/logr v1.2.4 // indirect
	github.com/go-openapi/jsonpointer v0.19.6 // indirect
	github.com/go-openapi/jsonreference v0.20.2 // indirect
	github.com/go-openapi/swag v0.22.3 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/google/gnostic-models v0.6.8 // indirect
	github.com/google/go-cmp v0.5.9 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/imdario/mergo v0.3.6 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.4 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/nxadm/tail v1.4.8 // indirect
	github.com/opencontainers/runc v1.1.7 // indirect
	github.com/opencontainers/runtime-tools v0.9.1-0.20221107090550-2e043c6bd626 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_model v0.4.0 // indirect
	github.com/prometheus/common v0.44.0 // indirect
	github.com/prometheus/procfs v0.10.1 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/syndtr/gocapability v0.0.0-20200815063812-42c35b437635 // indirect
	golang.org/x/mod v0.10.0 // indirect
	golang.org/x/oauth2 v0.8.0 // indirect
	golang.org/x/sys v0.13.0 // indirect
	golang.org/x/term v0.13.0 // indirect
	golang.org/x/text v0.13.0 // indirect
	golang.org/x/time v0.3.0 // indirect
	gomodules.xyz/jsonpatch/v2 v2.4.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20230525234030-28d5490b6b19 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/tomb.v1 v1.0.0-20141024135613-dd632973f1e7 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/kube-openapi v0.0.0-20230717233707-2695361300d9 // indirect
	k8s.io/utils v0.0.0-20230406110748-d93618cff8a2 // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.2.3 // indirect
	sigs.k8s.io/yaml v1.3.0 // indirect
	tags.cncf.io/container-device-interface/specs-go v0.6.0 // indirect
)

replace (
	4pd.io/k8s-vgpu/pkg/api => ./pkg/api
	4pd.io/k8s-vgpu/pkg/device-plugin => ./pkg/device-plugin
<<<<<<< HEAD
	k8s.io/api => k8s.io/api v0.25.4
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.25.4
	k8s.io/apimachinery => k8s.io/apimachinery v0.25.4
	k8s.io/apiserver => k8s.io/apiserver v0.25.4
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.25.4
	k8s.io/client-go => k8s.io/client-go v0.25.4
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.25.4
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.25.4
	k8s.io/code-generator => k8s.io/code-generator v0.25.4
	k8s.io/component-base => k8s.io/component-base v0.25.4
	k8s.io/component-helpers => k8s.io/component-helpers v0.25.4
	k8s.io/cri-api => k8s.io/cri-api v0.25.4
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.25.4
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.25.4
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.25.4
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.25.4
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.25.4
	k8s.io/kubectl => k8s.io/kubectl v0.25.4
	k8s.io/kubelet => k8s.io/kubelet v0.25.4
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.25.4
	k8s.io/metrics => k8s.io/metrics v0.25.4
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.25.4
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
=======
	k8s.io/api => k8s.io/api v0.28.3
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.28.3
	k8s.io/apimachinery => k8s.io/apimachinery v0.28.3
	k8s.io/apiserver => k8s.io/apiserver v0.28.3
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.28.3
	k8s.io/client-go => k8s.io/client-go v0.28.3
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.28.3
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.28.3
	k8s.io/code-generator => k8s.io/code-generator v0.28.3
	k8s.io/component-base => k8s.io/component-base v0.28.3
	k8s.io/component-helpers => k8s.io/component-helpers v0.28.3
	k8s.io/cri-api => k8s.io/cri-api v0.28.3
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.28.3
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.28.3
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.28.3
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.28.3
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.28.3
	k8s.io/kubectl => k8s.io/kubectl v0.28.3
	k8s.io/kubelet => k8s.io/kubelet v0.28.3
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.28.3
	k8s.io/metrics => k8s.io/metrics v0.28.3
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.28.3
>>>>>>> c7a3893 (Remake this repo to HAMi)
)
