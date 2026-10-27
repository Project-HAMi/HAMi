{{/*
Return the proper image name
{{ include "common.images.image" ( dict "imageRoot" .Values.path.to.the.image "global" $) }}
*/}}
{{- define "common.images.image" -}}
{{- $registryName := .imageRoot.registry -}}
{{- $repositoryName := .imageRoot.repository -}}
{{- $tag := .imageRoot.tag | toString -}}
{{- if .global }}
    {{- if .global.imageRegistry }}
     {{- $registryName = .global.imageRegistry -}}
    {{- end -}}
{{- end -}}
{{- if .tag }}
     {{- $tag = .tag | toString -}}
{{- end -}}
{{- $image := printf "%s:%s" $repositoryName $tag -}}
{{- if $registryName }}
{{- $image = printf "%s/%s" $registryName $image -}}
{{- end -}}
{{- include "common.images.source" (dict "image" $image "global" .global) -}}
{{- end -}}

{{/* Select a mirror for known upstream images without changing the input values. */}}
{{- define "common.images.source" -}}
{{- $global := .global | default dict -}}
{{- $source := $global.imageSource | default "upstream" -}}
{{- if not (has $source (list "upstream" "cn")) -}}
{{- fail "global.imageSource must be upstream or cn" -}}
{{- end -}}
{{- $image := .image -}}
{{- if and (eq $source "cn") (not $global.imageRegistry) -}}
  {{- /* The certgen mirror has a different index digest: switch the whole reference. */ -}}
  {{- if eq $image "registry.k8s.io/ingress-nginx/kube-webhook-certgen:v1.6.9@sha256:01038e7de14b78d702d2849c3aad72fd25903c4765af63cf16aa3398f5d5f2dd" -}}
    {{- $image = "registry.cn-hangzhou.aliyuncs.com/google_containers/kube-webhook-certgen:v1.6.9@sha256:78bf4f774a27f29a5e6333c11708ac4645139e7bb2b74aa9e9c4574935f7a989" -}}
  {{- else if and (hasPrefix "registry.k8s.io/kube-scheduler:" $image) (not (contains "@" $image)) -}}
    {{- /* Keep the cluster-derived tag without adding a digest. */ -}}
    {{- $image = replace "registry.k8s.io/kube-scheduler:" "registry.cn-hangzhou.aliyuncs.com/google_containers/kube-scheduler:" $image -}}
  {{- else -}}
    {{- range $repository := list "docker.io/projecthami/hami" "docker.io/projecthami/mock-device-plugin" "ghcr.io/lupinemachines/lupine-server" "ghcr.io/lupinemachines/lupine-client" -}}
      {{- if hasPrefix (printf "%s:" $repository) $image -}}
        {{- $image = printf "m.daocloud.io/%s" $image -}}
      {{- end -}}
    {{- end -}}
  {{- end -}}
{{- end -}}
{{- $image -}}
{{- end -}}

{{/*
Return the proper Docker Image Registry Secret Names (deprecated: use common.images.renderPullSecrets instead)
{{ include "common.images.pullSecrets" ( dict "images" (list .Values.path.to.the.image1, .Values.path.to.the.image2) "global" .Values.global) }}
*/}}
{{- define "common.images.pullSecrets" -}}
  {{- $pullSecrets := list }}

  {{- if .global }}
    {{- range .global.imagePullSecrets -}}
      {{- $pullSecrets = append $pullSecrets . -}}
    {{- end -}}
  {{- end -}}

  {{- range .images -}}
    {{- range .pullSecrets -}}
      {{- $pullSecrets = append $pullSecrets . -}}
    {{- end -}}
  {{- end -}}

  {{- if (not (empty $pullSecrets)) }}
imagePullSecrets:
    {{- range $pullSecrets }}
  - name: {{ . }}
    {{- end }}
  {{- end }}
{{- end -}}