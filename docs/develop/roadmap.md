# roadmap

Heterogeneous AI Computing device to support

| Production  | manufactor | Type        |MemoryIsolation | CoreIsolation | MultiCard support |
|-------------|------------|-------------|-----------|---------------|-------------------|
| GPU         | NVIDIA     | All         | ✅              | ✅            | ✅                |
| MLU         | Cambricon  | 370, 590    | ✅              | ✅            | ❌                |
| GCU         | Enflame    | S60         | ✅              | ✅            | ❌                |
| DCU         | Hygon      | Z100, Z100L | ✅              | ✅            | ❌                |
| Ascend      | Huawei     | 910B        | ✅              | ✅            | ❌                |
| GPU         | iluvatar   | All         | ✅              | ✅            | ❌                |
| DPU         | Teco       | Checking    | In progress     | In progress   | ❌                |


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
