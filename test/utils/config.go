package utils

// test data
const (
	GPUNodeLabelKey      = "gpu"
	GPUNodeLabelValue    = "on"
	GPUExecuteNvidiaSMI  = "nvidia-smi"
	GPUExecuteCudaSample = "/cuda-samples/sample"
	GPUPodMemory         = "300"
	GPUPodMemoryUnit     = "MiB"
	GPUPodCore           = "40"
	GPUNameSpace         = "hami-system"
	GPUNode              = "gpu-master"
	GPUCudaTestPass      = "Test PASSED"
)

// hami related
const (
	HamiScheduler              = "hami-scheduler"
	HamiDevicePlugin           = "hami-device-plugin"
	ErrReasonFilteringFailed   = "FilteringFailed"
	ErrMessageFilteringFailed  = "no available node, all node scores do not meet"
	ErrReasonFailedScheduling  = "FilteringFailed"
	ErrMessageFailedScheduling = "0/1 nodes are available"
)
