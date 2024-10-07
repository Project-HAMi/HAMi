# hami-vgpu-dashboard

- 你可以在此找到 hami-vgpu-dashboard：[https://grafana.com/grafana/dashboards/21833-hami-vgpu-dashboard](https://grafana.com/grafana/dashboards/21833-hami-vgpu-dashboard)

- 此 dashboard 还包括一部分 [NVIDIA DCGM 监控指标](https://github.com/NVIDIA/dcgm-exporter)： `kubectl create -f https://raw.githubusercontent.com/NVIDIA/dcgm-exporter/master/dcgm-exporter.yaml`

- 添加 prometheus 自定义的监控项:

```yaml
- job_name: 'kubernetes-hami-exporter'
    kubernetes_sd_configs:
    - role: endpoints
    relabel_configs:
    - source_labels: [__meta_kubernetes_endpoints_name]
      regex: hami-.*
      replacement: $1
      action: keep
    - source_labels: [__meta_kubernetes_pod_node_name]
      regex: (.*)
      target_label: node_name
      replacement: ${1}
      action: replace
    - source_labels: [__meta_kubernetes_pod_host_ip]
      regex: (.*)
      target_label: ip
      replacement: $1
      action: replace
- job_name: 'kubernetes-dcgm-exporter'
    kubernetes_sd_configs:
    - role: endpoints
    relabel_configs:
    - source_labels: [__meta_kubernetes_endpoints_name]
      regex: dcgm-exporter
      replacement: $1
      action: keep
    - source_labels: [__meta_kubernetes_pod_node_name]
      regex: (.*)
      target_label: node_name
      replacement: ${1}
      action: replace
    - source_labels: [__meta_kubernetes_pod_host_ip]
      regex: (.*)
      target_label: ip
      replacement: $1
      action: replace
```

- 热加载 promethues 配置：

```bash
curl -XPOST http://{promethuesServer}:{port}/-/reload
```
