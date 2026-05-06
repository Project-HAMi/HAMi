# roadmap
| Production | Manufactor    | Type             | MemoryIsolation | CoreIsolation | MultiCard support |
|------------|---------------|------------------|-----------------|---------------|-------------------|
| GPU        | NVIDIA        | All              | ✅              | ✅            | ✅                |
| MLU        | Cambricon     | 370, 590         | ✅              | ✅            | ❌                |
| GCU        | Enflame       | S60              | ✅              | ✅            | ❌                |
| DCU        | Hygon         | Z100, Z100L      | ✅              | ✅            | ❌                |
| NPU        | Ascend        | 310P, 910B, 910B3| ✅              | ✅            | ❌                |
| GPU        | iluvatar      | All              | ✅              | ✅            | ❌                |
| DPU        | Teco          | Checking         | In progress     | In progress   | ❌                |
| GPU        | Moore Threads | MTT S4000        | ✅              | ✅            | ❌                |
| GPU        | Birentech     | Model 110        | In progress     | In progress   | ❌                |
| GPU        | MetaX         | MXC500           | ✅              | ✅            | ❌                |
| XPU        | Kunlunxin     | P800             | ✅              | ✅            | ❌                |
| GPU        | Vastai        | VA16             | ✅              | ✅            | ❌              |      


- [ ] Support video codec processing
- [ ] Support Multi-Instance GPUs (MIG)
- [ ] Support Flexible scheduling policies
  - [x] binpack
  - [x] spread
  - [ ] numa affinity
- [ ] integrated gpu-operator
- [ ] Rich observability support
- [ ] DRA Support
- [ ] Support Intel GPU device
- [ ] Support AMD GPU device
- [x] Support Enflame GCU device
