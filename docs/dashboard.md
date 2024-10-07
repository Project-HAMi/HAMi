# hami-vgpu-dashboard

- You can find the hami-vgpu-dashboard here: [https://grafana.com/grafana/dashboards/21833-hami-vgpu-dashboard](https://grafana.com/grafana/dashboards/21833-hami-vgpu-dashboard)

- This dashboard also includes some [NVIDIA DCGM metrics](https://github.com/NVIDIA/dcgm-exporter)：`kubectl create -f https://raw.githubusercontent.com/NVIDIA/dcgm-exporter/master/dcgm-exporter.yaml`

- add prometheus custom metric configuration:

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

- reload promethues:

```bash
curl -XPOST http://{promethuesServer}:{port}/-/reload
```
