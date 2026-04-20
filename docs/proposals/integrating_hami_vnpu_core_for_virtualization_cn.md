HAMi-vnpu-core 是一个Rust语言编写的昇腾 NPU 容器内资源控制器，通过 libvnpu.so（拦截器） + Limiter（管理器）实现用户态拦截，通过NPU_MEM_QUOTA 和 NPU_PRIORITY 两个环境变量分别声明显存限制和调度优先级。本方案在 HAMi 调度中集成该能力，以支持昇腾 NPU 的显存虚拟化与算力时间片软切分。

## 一、前置条件

昇腾驱动版本25.5以上，芯片开启 device-share 模式:

#### 功能说明

**npu-smi set -t device-share -i** *id* **-d** value 用于设置指定设备的所有芯片的容器共享模式。

#### 参数说明

| 类型    | 描述                                                        |
| ------- | ----------------------------------------------------------- |
| *id*    | 设备ID。通过**npu-smi info -l**命令查出的NPU ID即为设备ID。 |
| *value* | 容器使能状态：分为禁用、使能。默认禁用。0：禁用 1：使能     |

## 二、HAMi-scheduler改动点

### 2\.1 扩展资源名

复用 huawei.com/Ascend910B3-memory ：显存分配逻辑可复用原逻辑；

新增 huawei.com/Ascend910B3-core：声明了该资源名的请求走vnpu软切分，否则走原硬切分逻辑。
| 资源名称 | 单位 | 含义 | 示例 |
|--------------|----------|----------|----------|
| huawei.com/Ascend910B3 | 整数 | NPU卡数 | 1 |
| huawei.com/Ascend910B3-memory | Mi | 显存配额 | 28672 (28Gi) |
| huawei.com/Ascend910B3-core | 整数 | 百分比 | 20, 40 |



### 2\.2 Filter阶段

**Filter阶段，**修改 **Fit** 函数核心逻辑，确保单张卡总算力不超过 100；同时修改 PatchAnnotation 注入的注解。

**Pod配额预期分配结果格式（Pod Annotation）:**

```shell {"data-theme":"githubLight"}
{
  "huawei.com/Ascend910B3": "[
    {
      "UUID":"xxx",
      "memory": 28672,   
      "core": 20,
    }
  ]"
}
```

**PatchAnnotation**判断逻辑 vnpu软切分新增显存（memory）和核心（core）字段

```go {"data-title":"pkg/device/ascend/device.go","data-theme":"githubLight"}
func (dev *Devices) PatchAnnotations(pod *corev1.Pod, annoInput *map[string]string, pd device.PodDevices) map[string]string {
	commonWord := dev.CommonWord()
	devList, ok := pd[commonWord]
	if ok && len(devList) > 0 {
		...
		for _, dp := range devList {
			for _, val := range dp {
              ...
    
				// 解析 Pod 的memQuota、 priority 资源声明 构建 RuntimeInfo
				rtInfo = append(rtInfo, RuntimeInfo{
					UUID:     val.UUID,
					Temp:     tempName,        
					MemQuota: memory,        
					Priority: core,        
				})
			}
		}
       ...
	}
	return *annoInput
}
```

### 2\.3 容器limiter进程启动

通过K8S生命周期钩子 PostStart 执行自定义动作，启动limiter进程。pkg\\device\\ascend\\device.go **MutateAdmission**注入 postStart 钩子

```ruby {"data-theme":"githubLight"}
lifecycle:
  postStart:
    exec:
      command:
        - "bash"
        - "-c"
        - |
          export RUST_LOG=info
          /usr/local/hami-vnpu-core/limiter > /usr/local/hami-vnpu-core/inst1_manager.log 2>&1 &
```

**libvnpu.so改动点：**因为 postStart 无法保证在entrypoint之前完成。在业务真正开启分配内存或启动算力之前，**循环检测确保 lilmiter已经准备就绪**。

```ruby {"data-theme":"githubLight"}
impl SchedulerClient {
    pub fn new() -> Self {
        let pid = std::process::id();
        let shmem_name = local_shmem_name();
        
        // ================== 新增：自动化等待逻辑 ==================
        let shm_path = format!("/dev/shm/{}", shmem_name);
        let mut retry_count = 0;
        
        info!("[Worker PID:{}] Checking for limiter at {}...", pid, shm_path);
        
        while !std::path::Path::new(&shm_path).exists() {
            if retry_count % 50 == 0 { // 每 5 秒提醒一次
                warn!("[Worker PID:{}] Still waiting for limiter process to create shared memory...", pid);
            }
            
            std::thread::sleep(std::time::Duration::from_millis(100));
            retry_count += 1;
            
            // 如果 1 分钟还没起来就崩溃
            if retry_count > 600 { 
                panic!("[Scheduler] FATAL: Limiter not found after 60 seconds.");
            }
        }
        info!("[Worker PID:{}] Limiter detected. Connecting to shared memory.", pid);
        // ========================================================

        // 此时文件一定存在了，安全打开
        let shmem = shmem::shm_setup::open_shmem::<LocalContainerShmem>(shmem_name.as_str());
    }
}
```

## 三、hami-ascend-device-plugin改动点

### 3\.1 宿主机固定路径存放limiter和libvnpu.so

宿主机固定路径存放limiter和libvnpu.so，为后续映射limiter和libvnpu.so到容器内准备：

```shell {"data-theme":"githubLight"}
/usr/local/hami-vnpu-core/
├── limiter          
└── libvnpu.so       
└── ld.so.preload  // 动态链接预加载配置文件
```

**ld.so.preload文件内容**

```plain-text {"data-theme":"githubLight"}
/hami-vnpu-core/target/debug/libvnpu.so
```

### 3\.2 宿主机创建共享内存区域

```shell {"data-theme":"githubLight"}
sudo mkdir -p /usr/local/hami-shared-region
sudo chmod 777 /usr/local/hami-shared-region
```

### 3\.3 Allocate函数增强

```go {"data-theme":"githubLight"}
func (ps *PluginServer) Allocate(ctx context.Context, reqs *v1beta1.AllocateRequest)
   /*
   1. (可省略)处理设备注入 可见设备环境变量设置后 设备文件会自动注入)
   2. 处理存储卷注入 (-v)
      A. 华为驱动与 SMI 工具链 
      B. vnpu-core代码路径 : /usr/local/hami-vnpu-core
      C. 注入 HAMi 劫持库路径 (对应脚本中的 LIBRARY_PATH)，通过/etc/ld.so.preload 挂载
      D. HAMi 算力切分专用共享目录 : /usr/local/hami-shared-region:/hami-shared-region
   3. 环境变量注入 
      A. 可见设备IDs
      B. 共享内存通信路径：NPU_GLOBAL_SHM_PATH = /hami-shared-region/{ID}_global_registry
      C. 显存配额NPU_MEM_QUOTA：从Annotation读取 memory = 28672
      D. 优先级NPU_PRIORITY：从Annotation读取 core = 20
   */
}
```