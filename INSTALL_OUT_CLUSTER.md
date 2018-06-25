# deploy coredump-detector outside of the cluster

This is a cluster admin instruction about how to deploy coredump-detector webhook outside of kubernetes cluster.
It's recommend to read [this document](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/) before you going on.

## Prepare the node(s)
1. Make sure your container runtime could generate coredump files inside container in directory `/var/coredump`. If you are using namespace based runtime, you usually need to run command `echo "/var/coredump/core_%e_%t" > /proc/sys/kernel/core_pattern` in your node.
2. lable the node
```shell
$ kubectl label nodes nodeName coredump=true
```

## Prepare Certificates
run the following command:
```shell
$ cd gencerts
$ ./make-ca-cert.sh <ip of the host where the coredump-detector is deployed, and kube-apiserver will use this ip to access coredump-detector>
$ ls output
ca.crt  client.crt  client.key  server.cert  server.key
```
For more information about certificates: https://kubernetes.io/docs/concepts/cluster-administration/certificates/

## Config the kube-apiserver
1. enable the MutatingAdmissionWebhook admission controller, add the follow options to kube-apiserver
```
--enable-admission-plugins=...,MutatingAdmissionWebhook,... --admission-control-config-file=/etc/kubernetes/admission-config.yaml
```
2. in the file /etc/kubernetes/admission-config.yaml, you should have:
```yaml
apiVersion: apiserver.k8s.io/v1alpha1
kind: AdmissionConfiguration
plugins:
- name: MutatingAdmissionWebhook
  configuration:
    apiVersion: apiserver.config.k8s.io/v1alpha1
    kind: WebhookAdmission
    kubeConfigFile: /etc/kubernetes/webhook_admission_config.yaml
# other admission configs
# ...
```
3. in the file /etc/kubernetes/webhook_admission_config.yaml, you should have:
```yaml
apiVersion: v1
kind: Config
users:
- name: "10.167.133.33" # replace this with "*" or `ip of the host where the coredump-detector is deployed`
  user:
    client-certificate: /path/to/client.crt  # this is the client.crt you generated before
    client-key: /path/to/client.key  # this is the client.key you generated before
```

4. start the kube-apiserver

# deploy the coredump-detector
1. build the binary
```shell
$ make build
```

2. run the webhook
```
# the ca.crt, server.cert, server.key are files generated before
# you should only run this in the host with ip you specified when preparing certificates
./coredump-detector -v=10 --alsologtostderr --client-ca-file gencerts/output/ca.crt --tls-cert-file gencerts/output/server.cert --tls-private-key-file gencerts/output/server.key
```

# make MutatingWebhookConfiguration object in kube-apiserver
```shell
$ cat <<EOF > MutatingWebhookConfiguration.yaml
apiVersion: admissionregistration.k8s.io/v1beta1
kind: MutatingWebhookConfiguration
metadata:
  name: coredump
webhooks:
- clientConfig:
    # replace this with your caBundle
    # use this command to get it:
    # cat gencerts/output/ca.crt | base64 | tr '\n' '\0' 
    caBundle: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSURYRENDQWtTZ0F3SUJBZ0lKQUxaSmo5RGxFMCtYTUEwR0NTcUdTSWIzRFFFQkN3VUFNQ014SVRBZkJnTlYKQkFNTUdERXdMakUyTnk0eE16TXVNek5BTVRVeU9EZzFORE16T1RBZUZ3MHhPREEyTVRNd01UUTFNemxhRncweQpPREEyTVRBd01UUTFNemxhTUNNeElUQWZCZ05WQkFNTUdERXdMakUyTnk0eE16TXVNek5BTVRVeU9EZzFORE16Ck9UQ0NBU0l3RFFZSktvWklodmNOQVFFQkJRQURnZ0VQQURDQ0FRb0NnZ0VCQU1BbjlLd2EvRVplUm9VcjhIaDcKLys2R3p2MllNdTljUEtuZ3JIYnM0aTVLcXhrRXdIT094QTdRdVdMUnRoTFlSay9xcWdJNUJyRDByMUJFVEFPdApGd1ZoMlFOV2hJYVEvUUFaaCtEWFFiM3V5RkFOUlpkTTNJZ0FNZDM3VUZLbFh0MDhPMzJ4eUFtOUhKa0VCbGJOCndhWXpnR01sYUZnZnFQajlWdGFYRVhjK3Jxd2p4MjFvM29lWkVCaEg3czMvMjFsS2ZycURORWt1NWpLeXdYSTcKY1JQK0JWR3JLaWphd1V0RGxZTktqeVo0allVdlRCMFR6YmVDYTNLT21IcTF4bUozeXJVUTcwdGFuOGs5VXRSdgpHa1dFYW5zM2dsK3psR0Q2dzA2NWpJWDkyeXdscEs3ajY2WUlBSGJySUdrMUJyUDJVSDR0emtyZy95SFFhc3VvCnJDa0NBd0VBQWFPQmtqQ0JqekFkQmdOVkhRNEVGZ1FVb3NSQUEzTmFIRm1nNDFhT0RQU3R0SHdFeDZJd1V3WUQKVlIwakJFd3dTb0FVb3NSQUEzTmFIRm1nNDFhT0RQU3R0SHdFeDZLaEo2UWxNQ014SVRBZkJnTlZCQU1NR0RFdwpMakUyTnk0eE16TXVNek5BTVRVeU9EZzFORE16T1lJSkFMWkpqOURsRTArWE1Bd0dBMVVkRXdRRk1BTUJBZjh3CkN3WURWUjBQQkFRREFnRUdNQTBHQ1NxR1NJYjNEUUVCQ3dVQUE0SUJBUUJVd2x0Z25INnVGUEZhQXFWQ2hneDkKSUhBR2RLSzM0SUVaQlZIVDhqYnVxRXJFTUdVbFZ0V21LSkIyQ29COXEyZUdRMUdDSlBOKzRhRFFORzRjOENpdQpzOVNMZFJ4bXYyRnBIMkRMQTNNZGtQTThra0xMcWRyK1BzNklUei92NEUwK3FMK3lQZE50SjdacVozMjRIeEZmCnFEVzJaUFJWRm96cW5wUVZoRHMyNEJWdklCTnpkbno0K0ZNSzRVVjUyblRaZTh2S0hGVzBLdGI0bWU4Q29NRWEKY05ENElZTzF6S2hJNWF2a3NPbG1hdzE2ZW1wNEs3aUFBSTRueFFWSi84UVZJOGRLY2NINmxBQTJmQ3AvOVdEVQorV1lRV1BRRjl1c2o0MVVMYitIMWtUcjdJcUNIVHREQVU2SERRcFdyWXkrOUpQR243Zmt1YXUvb2lXV2MySGZGCi0tLS0tRU5EIENFUlRJRklDQVRFLS0tLS0K
    url: https://<ip where coredump is deployed>/
  failurePolicy: Ignore
  name: coredump.fujitsu.com
  rules:
  - apiGroups:
    - ""
    apiVersions:
    - v1
    operations:
    - CREATE
    resources:
    - pods

$ kubectl create -f MutatingWebhookConfiguration.yaml
```
