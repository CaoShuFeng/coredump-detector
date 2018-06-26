# deploy coredump-detector as a kubernetes service

This is a cluster admin instruction about how to deploy coredump-detector webhook as a kubernetes service.
It's recommend to read [this document](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/) before you going on.

## Prepare the node(s)
1. Make sure your container runtime could generate coredump files inside container in directory `/var/coredump`. If you are using namespace based runtime, you usually need to run command `echo "/var/coredump/core_%e_%t" > /proc/sys/kernel/core_pattern` in your node.
2. lable the node
```shell
$ kubectl label nodes nodeName coredump=true
```

## Prepare the docker image
1. build the docker image
```shell
make build-container
```

2. push the docker image to REPOSITORY
```shell
docker push caoshufeng/coredump-detector:v0.2
```
**Note that before pushing the image, you may need to tag the image with your name. e.g. `docker tag caoshufeng/coredump-detector:v0.2 <your-username>/coredump-detector:v0.2`**

## Prepare Certificates
run the following command:
```shell
$ cd gencerts
$ ./make-ca-cert.sh "coredump-detector.default.svc" # in such format: <service-name>.<namespace>.svc
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
- name: "coredump-detector.default.svc" # replace this with "*" or "<service-name>.<namespace>.svc"
  user:
    client-certificate: /path/to/client.crt  # this is the client.crt you generated before
    client-key: /path/to/client.key  # this is the client.key you generated before
```

4. start the kube-apiserver

## Create a service for coredump detector
```shell
kubectl-user create -f- <<EOF
apiVersion: v1
kind: Service
metadata:
  name: coredump-detector
spec:
  ports:
    - port: 443
      targetPort: 443
  clusterIP: <ip of the service>
  selector:
    app: coredump-detector
EOF

```

## deploy the coredump-detector
1. create the secret

```shell
kubectl create secret generic coredump-detector-certs --from-file=ca.crt=ca.crt --from-file=server.key=server.key --from-file=server.cert=server.cert
```
**note that the ca.crt,server.key and server.cert are generated before**

2. create the deployment
```shell
kubectl-user create -f- <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: coredump-detector
  labels:
    app: coredump-detector
spec:
  replicas: 2
  selector:
    matchLabels:
      app: coredump-detector
  template:
    metadata:
      labels:
        app: coredump-detector
    spec:
      volumes:
      - name: coredump-detector-certs
        secret:
          secretName: coredump-detector-certs
      containers:
      - name: coredump-detector
        image: caoshufeng/coredump-detector:v0.2 # replace this with image you pushed to your repository
        command: ["/coredump-detector", "--v=10", "--alsologtostderr", "--client-ca-file=/etc/coredump-detector/ca.crt", "--tls-cert-file=/etc/coredump-detector/server.cert", "--tls-private-key-file=/etc/coredump-detector/server.key"]
        volumeMounts:
        - name: coredump-detector-certs
          readOnly: true
          mountPath: "/etc/coredump-detector/"
        ports:
          - containerPort: 443

EOF

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
    caBundle: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSURVekNDQWp1Z0F3SUJBZ0lKQU5oUnNMbVVqR0RBTUEwR0NTcUdTSWIzRFFFQkN3VUFNQ0F4SGpBY0JnTlYKQkFNTUZUSXdMakV1TVM0eU1UZEFNVFV5T1Rnd056TTFOakFlRncweE9EQTJNalF3TWpJNU1UWmFGdzB5T0RBMgpNakV3TWpJNU1UWmFNQ0F4SGpBY0JnTlZCQU1NRlRJd0xqRXVNUzR5TVRkQU1UVXlPVGd3TnpNMU5qQ0NBU0l3CkRRWUpLb1pJaHZjTkFRRUJCUUFEZ2dFUEFEQ0NBUW9DZ2dFQkFPR1ZHSnE0S0lQODNzenREZ09PR0VvUm9XODIKby9qREZGa05TbHhmVWxTc1Q4ck5kaFFYRk02ZElQTnVFTzFldmM2b1dad0NrOWlNR3ZIWFlIazVIUkREa2ZRawpkU01zVVFvYVhSemY2SGlnV0krRjI3ZDdIdTRXWXZrYndYZDErVmNsYXFtWDkrT1hNb0N6R1F4N0dlNHVadG0rClpid0dwdHlFTndEQUpRd0R3UE1DOGxjNkdsbkFPcmZtblA5R1hLaC96ZFZQblE0N1AzelVrN3hTbGFuZktRaGYKbHdYVURvWG5LMDBJd0ozaUErY2FENVNBQjdRTnc5VGhWTWR2WVF4TzJWNEwxS3JKK2UwS0RuSDFHejRPWjl1OQp3NFhjTW1pd2Y3T1VKMXlpemQ0WCtINnpkTE1tY1M4QkRHZ2x1cXl0WVRhQnRKU1Y5aUtUeXB3aUg1VUNBd0VBCkFhT0JqekNCakRBZEJnTlZIUTRFRmdRVWE4M2ZEeFBWSG9aSmFtQWtTbkkyMm1HZ1Rid3dVQVlEVlIwakJFa3cKUjRBVWE4M2ZEeFBWSG9aSmFtQWtTbkkyMm1HZ1RieWhKS1FpTUNBeEhqQWNCZ05WQkFNTUZUSXdMakV1TVM0eQpNVGRBTVRVeU9UZ3dOek0xTm9JSkFOaFJzTG1VakdEQU1Bd0dBMVVkRXdRRk1BTUJBZjh3Q3dZRFZSMFBCQVFECkFnRUdNQTBHQ1NxR1NJYjNEUUVCQ3dVQUE0SUJBUUI2a1ZqVEZ0VXpGenJRK0VNUU8vMUhjbEhEc2FLUXF3cHMKOHY1MEExQ0cwOUhHTG1lQ2llNm9SY0twV2JCdzhEc3lJTXpMbVlsc0dPdWM1QjVVdllCN3V2RlkvNUY4b1crVApRdFZ4bjNEdDg5SWNnWnNPSEF6UDZjK2d3akdDUXR5VTMxYk1YNGo2NkdFQVljL3FxOVl3akxzSDlIV1BmMk5CCjhZRGllY0JrT1pDRVZKSDFROGlodXVvN2I2Z3A2RGhVVW1mSkRFSUlyYVZNT1c1RXE2ZTBsaHBRU09Sd3ExY0oKRnQrREZHeEFQTTVBRjJJNEZxZjBVRVpuYk9LNGlISSt5bEdJNzYrdjBxWGFIN1FMeEJxTWxZWWxlUk11KzB5VApUY2kwZ05RMnI2ZllzaG1MMG1XUDlrSnJYYk1kUkRlK1BicXZZelY5VHAyYktZRFpBTk15Ci0tLS0tRU5EIENFUlRJRklDQVRFLS0tLS0K
    service:
      namespace: default
      name: coredump-detector # kube-apiserver will use coredump-detector.default.svc to visit the MutatingWebhook, this requires the DNS set properly.
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
EOF

$ kubectl create -f MutatingWebhookConfiguration.yaml
```
