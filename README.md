## coredump-detector

Coredump-detector is a MutatingAdmissionWebhook. It mounts a persistent volume claim to a specific directory in every container of the pod. And add a NodeSelector to the pod, so that the pod will be scheduled to a node that supports coredump. Here `supports coredump` means the kernel will generate the coredump file into the mounted persistent volume.


## How does cluster admin deploy it?

### how to deploy it outside of cluster
See INSTALL_OUT_CLUSTER.md

### how to deploy it as a k8s service
See INSTALL_AS_SERVICE.md

## How tenant use the feature

1. Declare a rwx persistent volume claim (see: https://kubernetes.io/docs/concepts/storage/persistent-volumes/)
2. When `creating` a pod, add an annotation to the pod(or pod template if you are creating a deployment or so) like this:
```yaml
apiVersion: v1
kind: Pod
metadata:
  annotations:
    "coredump.fujitsu.com/pvcname": myclaim  # here myclaim is the rwx persistent volume claim created in step one
  name: example
```
3. Check the volume and nodeSelector in the pod spec. When run `kubectl get pods foo -o yaml`, you can see MutatingAdmissionWebhook has modified the pod:
```yaml
apiVersion: v1
items:
- apiVersion: v1
  kind: Pod
  metadata:
    annotations:
      coredump.fujitsu.com/pvcname: myclaim
      kubernetes.io/psp: privileged
    namespace: default
  spec:
    nodeSelector:
      # this is new added
      coredump: "true"
    containers:
    - command:
      - sleep
      - "10000"
      image: busybox
      # this is new added
      name: example
      volumeMounts:
      - mountPath: /var/coredump
        name: myclaim-1528858878
        subPath: example/example
```

4. check the mount path inside the container:
```shell
$ kubectl exec -it example sh
$ mount |grep coredump
172.16.29.130:/nfs/example/example on /var/coredump type nfs4 (rw,relatime,vers=4.0,rsize=524288,wsize=524288,namlen=255,hard,proto=tcp,port=0,timeo=600,retrans=2,sec=sys,clientaddr=172.16.29.129,local_lock=none,addr=172.16.29.130)
```

5. Now the core files are all saved in your persistent volume claim.

### known issues
1. it can't work well with command `kubectl apply -f`. See: https://github.com/kubernetes/kubernetes/issues/64944
