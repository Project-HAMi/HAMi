## Grafana Dashboard

- You can load this dashboard json file [gpu-dashboard.json](./gpu-dashboard.json)

- This dashboard also includes some NVIDIA DCGM metrics:

  [dcgm-exporter](https://github.com/NVIDIA/dcgm-exporter) deploy：`kubectl create -f https://raw.githubusercontent.com/NVIDIA/dcgm-exporter/master/dcgm-exporter.yaml`

- use this prometheus custom metric configure:

```yaml
- job_name: 'kubernetes-vgpu-exporter'
    kubernetes_sd_configs:
    - role: endpoints
    relabel_configs:
    - source_labels: [__meta_kubernetes_endpoints_name]
      regex: vgpu-device-plugin-monitor
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

- reload promethues：

```bash
curl -XPOST http://{promethuesServer}:{port}/-/reload
```
