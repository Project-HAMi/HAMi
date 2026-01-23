# Offline-install Maunal

For some cluster that don't have external web access, you can install HAMi by the following step:

1. Refer to [README.md](../README.md) until step 'Install and Uninstall'

<<<<<<< HEAD
2. pull the following images and save them into a '.tar' file, then move it into your cluster

Image list:
```
projecthami/hami:{HAMi version} 
=======
2. copy the source of project into the master node in your cluster, placed in a path like "/root/HAMi"

3. pull the following images and save them into a '.tar' file, then move it into the master node in your cluster

Image list:
```
4pdosc/k8s-vdevice:{HAMi version} 
>>>>>>> c7a3893 (Remake this repo to HAMi)
docker.io/jettech/kube-webhook-certgen:v1.5.2
liangjw/kube-webhook-certgen:v1.1.1
registry.cn-hangzhou.aliyuncs.com/google_containers/kube-scheduler:{your kubernetes version}
```

```
<<<<<<< HEAD
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
=======
docker pull {iamge} && docker save {image_name} -o {image_name}.tar 
```

4. Load these images using docker load, tag these images with your registry, and push them into your registry

```
docker load -i {image_name}.tar
docker tag 4pdosc/k8s-vdevice:{HAMi version} {registry}/k8s-vdevice:{HAMi version} 
docker push {registry}/k8s-vdevice:{HAMi version}
```

5. edit the following field in /root/HAMi/chart/vgpu/values.yaml to your image pushed
>>>>>>> c7a3893 (Remake this repo to HAMi)

```
scheduler.kubeScheduler.image
scheduler.extender.image
scheduler.patch.image
scheduler.patch.imageNew
scheduler.devicePlugin.image
scheduler.devicePlugin.monitorimage
```

<<<<<<< HEAD
5. Execute the following command in your /root/HAMi/chart folder

```
helm install hami hami --set scheduler.kubeScheduler.imageTag={your k8s server version} -n kube-system
```

6. Verify your installation
=======
6. Execute the following command in your /root/HAMi/chart folder

```
helm install vgpu vgpu --set scheduler.kubeScheduler.imageTag={你的k8s server版本} -n kube-system
```

7. Verify your installation
>>>>>>> c7a3893 (Remake this repo to HAMi)

execute the following command
```
kubectl get pods -n kube-system
```

<<<<<<< HEAD
If you can see both the 'device-plugin' and 'scheduler' running, then HAMi is installed successfully, as the figure shown below:
=======
If you can see both the 'device-plugin' and 'schduler' running, then HAMi is installed successfully, as the figure shown below:
>>>>>>> c7a3893 (Remake this repo to HAMi)

<img src="./develop/imgs/offline_validation.png" width = "600" /> 
