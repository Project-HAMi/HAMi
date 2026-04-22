# Vulkan vGPU 支持

HAMi 通过注入 Vulkan 隐式层（`VK_LAYER_HAMI_vgpu`）对 NVIDIA GPU 进行 Vulkan 工作负载的切分。该层与已有的 CUDA 钩子共享同一套 VRAM 与 SM 预算。

## 启用方式

在使用 HAMi NVIDIA 资源的 Pod 上添加 annotation `hami.io/vulkan: "true"`。Webhook 会：

- 将 `graphics` 合并进 `NVIDIA_DRIVER_CAPABILITIES`，以便 NVIDIA Container Toolkit 挂载 Vulkan ICD 与图形库。
- 设置 `HAMI_VULKAN_ENABLE=1`，通过隐式层 manifest 的 `enable_environment` 激活 HAMi Vulkan 层。

示例：`examples/nvidia/vulkan_example.yaml`。

## 生效范围

- `nvidia.com/gpumem` 对容器内 CUDA 与 Vulkan 的 VRAM 分配**共享同一预算**。
- `nvidia.com/gpucores` 通过与 `cuLaunchKernel` 相同的 token-bucket 限速器对 `vkQueueSubmit[2]` 进行限速。
- `vkGetPhysicalDeviceMemoryProperties[2]` 将 device-local 堆大小裁剪为 Pod 预算。

## 未涵盖项（未来工作）

- Vulkan Video（`VK_KHR_video_queue`）提交。
- 图形队列限速导致的帧抖动（已记录，未来提供 strict/cooperative 模式）。

## 故障排查

| 现象 | 检查 |
|------|------|
| 容器没有 Vulkan 库 | annotation 缺失，或镜像已冻结 `NVIDIA_DRIVER_CAPABILITIES=compute`。 |
| `vkAllocateMemory` 总是成功 | 层未激活 — 确认 `HAMI_VULKAN_ENABLE=1` 与 `/etc/vulkan/implicit_layer.d/hami.json` 存在。 |
| `vulkaninfo` 仍报告全量 VRAM | Manifest 未加载；可 `VK_LOADER_DEBUG=all vulkaninfo` 查看扫描日志。 |
| 限速未生效 | `rate_limiter` 在 SM 限额为 0、>=100 或 HAMi 利用率开关关闭时不工作。确认 Pod 已请求 `nvidia.com/gpucores`。 |
