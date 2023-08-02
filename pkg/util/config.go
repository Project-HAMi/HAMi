package util

import (
	"flag"
	"os"

	cli "github.com/urfave/cli/v2"
	"k8s.io/klog/v2"
)

func GlobalFlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	fs.StringVar(&ResourceName, "resource-name", "nvidia.com/gpu", "resource name")
	fs.StringVar(&ResourceMem, "resource-mem", "nvidia.com/gpumem", "gpu memory to allocate")
	fs.StringVar(&ResourceMemPercentage, "resource-mem-percentage", "nvidia.com/gpumem-percentage", "gpu memory fraction to allocate")
	fs.StringVar(&ResourceCores, "resource-cores", "nvidia.com/gpucores", "cores percentage to use")
	fs.StringVar(&ResourcePriority, "resource-priority", "vgputaskpriority", "vgpu task priority 0 for high and 1 for low")
	fs.StringVar(&MLUResourceCount, "mlu-name", "cambricon.com/mlunum", "mlu resource count name ")
	fs.StringVar(&MLUResourceMemory, "mlu-memory", "cambricon.com/mlumem", "mlu resource memory name")
	fs.BoolVar(&DebugMode, "debug", false, "debug mode")
	klog.InitFlags(fs)
	return fs
}

func AddFlags() []cli.Flag {
	addition := []cli.Flag{
		&cli.StringFlag{
			Name:  "resource-name",
			Value: "nvidia.com/gpu",
			Usage: "the name of field for number GPU visible in container",
		},
		&cli.StringFlag{
			Name:  "resource-mem",
			Value: "nvidia.com/gpumem",
			Usage: "the name of field of GPU device memory(in MB)",
		},
		&cli.StringFlag{
			Name:  "resoure-mem-percentage",
			Value: "nvidia.com/gpumem-percentage",
			Usage: "the name of field of GPU device memory percentage ",
		},
		&cli.StringFlag{
			Name:  "resource-cores",
			Value: "nvidia.com/gpucores",
			Usage: "the name of field of cores percentage to use",
		},
		&cli.StringFlag{
			Name:  "resource-priority",
			Value: "vgputaskpriority",
			Usage: "the name of field for task priority",
		},
		&cli.StringFlag{
			Name:  "mlu-name",
			Value: "cambricon.com/mlunum",
			Usage: "the name of field for MLU number",
		},
		&cli.StringFlag{
			Name:  "mlu-memory",
			Value: "cambricon.com/mlumem",
			Usage: "the name of field for MLU device memory",
		},
	}
	return addition
}
