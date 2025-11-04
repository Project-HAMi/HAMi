# Offline-install Maunal

For some cluster that don't have external web access, you can install HAMi by the following step:

1. Refer to [README.md](../README.md) until step 'Install and Uninstall'

2. pull the following images and save them into a '.tar' file, then move it into your cluster

Image list:
```
projecthami/hami:{HAMi version} 
docker.io/jettech/kube-webhook-certgen:v1.5.2
liangjw/kube-webhook-certgen:v1.1.1
registry.cn-hangzhou.aliyuncs.com/google_containers/kube-scheduler:{your kubernetes version}
```

```
docker pull {image} && docker save {image_name} -o {image_name}.tar 
```

3. Load these images using docker load, tag these images with your registry, and push them into your registry

```
docker load -i {HAMi_image}.tar
docker tag projecthami/hami:{HAMi version} {your_inner_registry}/hami:{HAMi version} 
docker push {your_inner_registry}/hami:{HAMi version}
docker tag docker.io/jettech/kube-webhook-certgen:v1.5.2 {your inner_registry}/kube-webhook-certgen:v1.5.2
docker push {your inner_registry}/kube-webhook-certgen:v1.5.2
docker tag liangjw/kube-webhook-certgen:v1.1.1 {your_inner_registry}/kube-webhook-certgen:v1.1.1
docker tag registry.cn-hangzhou.aliyuncs.com/google_containers/kube-scheduler:{your kubernetes version} {your_inner_registry}/kube-scheduler:{your kubernetes version}
docker push {your_inner_registry}/kube-scheduler:{your kubernetes version}  
```

4. Download the charts folder from [github](https://github.com/Project-HAMi/HAMi/tree/master/charts), place it into ${CHART_PATH} inside cluster, then edit the following fields in ${CHART_PATH}/hami/values.yaml. 

```
scheduler.kubeScheduler.image
scheduler.extender.image
scheduler.patch.image
scheduler.patch.imageNew
scheduler.devicePlugin.image
scheduler.devicePlugin.monitorimage
```

5. Execute the following command in your /root/HAMi/chart folder

```
helm install hami hami --set scheduler.kubeScheduler.imageTag={your k8s server version} -n kube-system
```

6. Verify your installation

execute the following command
```
kubectl get pods -n kube-system
```

If you can see both the 'device-plugin' and 'scheduler' running, then HAMi is installed successfully, as the figure shown below:

<img src="./develop/imgs/offline_validation.png" width = "600" /> 
