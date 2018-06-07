/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/golang/glog"
	"github.com/mattbaird/jsonpatch"
	"github.com/spf13/pflag"
	"k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	k8sjson "k8s.io/apimachinery/pkg/runtime/serializer/json"
)

var scheme = runtime.NewScheme()
var codecs = serializer.NewCodecFactory(scheme)

const annotationKey = `coredump.fujitsu.com/pvcname`

var jsonSerializer *k8sjson.Serializer

func init() {
	addToScheme(scheme)
	jsonSerializer = k8sjson.NewSerializer(k8sjson.DefaultMetaFactory, scheme, scheme, false)
}

func addToScheme(scheme *runtime.Scheme) {
	corev1.AddToScheme(scheme)
	v1beta1.AddToScheme(scheme)
}

// Options contains the options passed to k8s audit collector
type Options struct {
	CertFile     string
	KeyFile      string
	ClientCAFile string
	Port         uint
}

var options = Options{
	CertFile:     "server.cert",
	KeyFile:      "server.key",
	ClientCAFile: "client.crt",
}

func (o *Options) addFlags() {
	pflag.StringVar(&o.CertFile, "tls-cert-file", o.CertFile, ""+
		"File containing the default x509 Certificate for HTTPS. (CA cert, if any, concatenated "+
		"after server cert).")
	pflag.StringVar(&o.KeyFile, "tls-private-key-file", o.KeyFile, ""+
		"File containing the default x509 private key matching --tls-cert-file.")
	pflag.StringVar(&o.ClientCAFile, "client-ca-file", o.ClientCAFile, ""+
		"A cert file for the client certificate authority")
	pflag.UintVar(&o.Port, "bind-port", 443, "The port on which to listen for.")
}

func main() {
	options.addFlags()
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	cert, err := tls.LoadX509KeyPair(options.CertFile, options.KeyFile)
	if err != nil {
		glog.Fatal(err)
	}
	certBytes, err := ioutil.ReadFile(options.ClientCAFile)
	if err != nil {
		panic("Unable to read cert.pem")
	}
	clientCertPool := x509.NewCertPool()
	ok := clientCertPool.AppendCertsFromPEM(certBytes)
	if !ok {
		glog.Fatal("failed to parse root certificate")
	}
	config := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCertPool,
	}

	http.HandleFunc("/", podHandler)

	server := &http.Server{
		Addr:      fmt.Sprintf(":%d", options.Port),
		TLSConfig: config,
	}
	err = server.ListenAndServeTLS("", "")
	if err != nil {
		glog.Fatal(err)
	}
}

func podHandler(w http.ResponseWriter, r *http.Request) {
	var body []byte
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}

	// verify the content type is accurate
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		glog.Errorf("contentType=%s, expect application/json", contentType)
		w.WriteHeader(http.StatusUnsupportedMediaType)
		io.WriteString(w, "UnsupportedMediaType: "+contentType)
		return
	}

	glog.V(2).Info(fmt.Sprintf("handling request: %s", body))
	var reviewResponse *v1beta1.AdmissionResponse
	ar := v1beta1.AdmissionReview{}
	deserializer := codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
		glog.Error(err)
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, "Failed to decode request body err: " + err.Error())
		return
	} else {
		reviewResponse = mutatePod(ar)
	}
	glog.V(2).Info(fmt.Sprintf("sending response: %v", reviewResponse))

	response := v1beta1.AdmissionReview{}
	if reviewResponse != nil {
		response.Response = reviewResponse
		response.Response.UID = ar.Request.UID
	}

	resp, err := json.Marshal(response)
	if err != nil {
		glog.Error(err)
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(resp); err != nil {
		glog.Error(err)
	}

}

func toAdmissionResponse(err error, code int32) *v1beta1.AdmissionResponse {
	return &v1beta1.AdmissionResponse{
		Result: &metav1.Status{
			Message: err.Error(),
			Code:    code,
		},
	}
}

func allowAdmissionResponse() *v1beta1.AdmissionResponse {
	return &v1beta1.AdmissionResponse{
		Allowed: true,
	}
}

// mutatePod do the following things
// 1) check whether this is a pod creation request, if not return nil. (This is not expected to happen)
// 2) check whether the pod contains a `coredump.fujitsu.com/pvcname` annotation, if not return Allow directly.
// 3) mount the persistent volume claim to all containers in the pod
// 4) set the nodeSelector of the pod. This makes sure the pod can be scheduled to a node that support coredump.
func mutatePod(ar v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {
	glog.V(2).Info("mutating pods")

	// check the resource
	podResource := metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	if ar.Request == nil || ar.Request.Resource != podResource {
		glog.Errorf("expect resource to be %s", podResource)
		return nil
	}

	// check the verb
	if ar.Request.Operation != v1beta1.Create {
		glog.Errorf("expect operation to be %s", v1beta1.Create)
		return nil
	}

	// check pod annotation
	raw := ar.Request.Object.Raw
	pod := corev1.Pod{}
	deserializer := codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(raw, nil, &pod); err != nil {
		glog.Error(err)
		return toAdmissionResponse(err, http.StatusInternalServerError)
	}

	metadataAccessor := meta.NewAccessor()
	annots, err := metadataAccessor.Annotations(&pod)
	if err != nil {
		glog.Error(err)
		return toAdmissionResponse(err, http.StatusInternalServerError)
	}

	pvc := annots[annotationKey]
	if len(pvc) == 0 {
		// no key set, we do nothing
		return allowAdmissionResponse()
	}

	// mount the pvc to each container
	// note: this pvc meet the following requirements
	// 1) it should exist in the pod namespace
	// 2) it should be a RWX volume
	// However we don't check the existence and attribute of the pvc here.
	// When the pvc does not exist, or it could not mount in read write mode, the pod will be failed to create.
	//
	// TODO figure out how namespaced pvc work with non-namespaced static pods
	for i := range pod.Spec.InitContainers {
		if err := checkVolumeMounts(pod.Spec.InitContainers[i], pvc); err != nil {
			return toAdmissionResponse(err, http.StatusBadRequest)
		}
	}
	for i := range pod.Spec.Containers {
		if err := checkVolumeMounts(pod.Spec.Containers[i], pvc); err != nil {
			return toAdmissionResponse(err, http.StatusBadRequest)
		}
	}
	for _, volume := range pod.Spec.Volumes {
		if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName == pvc {
			return toAdmissionResponse(fmt.Errorf("%s is already in the volume list, this is not expected.", pvc), http.StatusBadRequest)
		}
	}

	newPod := pod.DeepCopy()

	// append the volume to volume list
	volumeName := fmt.Sprintf("%s-%d", pvc, time.Now().Unix())
	volume := corev1.Volume{
		volumeName,
		corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: pvc,
				ReadOnly:  false,
			},
		},
	}
	newPod.Spec.Volumes = append(newPod.Spec.Volumes, volume)

	// mount the volume to each container
	for i := range newPod.Spec.InitContainers {
		newPod.Spec.InitContainers[i].VolumeMounts = append(newPod.Spec.InitContainers[i].VolumeMounts,
			corev1.VolumeMount{
				Name:      volumeName,
				ReadOnly:  false,
				MountPath: "/var/coredump",
				SubPath:   newPod.Name + "/" + newPod.Spec.InitContainers[i].Name,
			})
	}
	for i := range newPod.Spec.Containers {
		newPod.Spec.Containers[i].VolumeMounts = append(newPod.Spec.Containers[i].VolumeMounts,
			corev1.VolumeMount{
				Name:      volumeName,
				ReadOnly:  false,
				MountPath: "/var/coredump",
				SubPath:   newPod.Name + "/" + newPod.Spec.Containers[i].Name,
			})
	}

	// set node selector:
	if newPod.Spec.NodeSelector != nil {
		newPod.Spec.NodeSelector["coredump"] = "true"
	} else {
		newPod.Spec.NodeSelector = map[string]string{"coredump": "true"}
	}

	objJS, err := runtime.Encode(jsonSerializer, newPod)
	if err != nil {
		return toAdmissionResponse(err, http.StatusInternalServerError)
	}
	patch, err := createPatch(raw, objJS)
	if err != nil {
		return toAdmissionResponse(err, http.StatusInternalServerError)
	}
	glog.V(5).Infof("Created patch :%s", patch)

	reviewResponse := v1beta1.AdmissionResponse{}
	reviewResponse.Allowed = true
	reviewResponse.Patch = patch
	return &reviewResponse
}

// checkVolumeMounts ensures that the path `/var/coredump` is not mounted with another volume.
func checkVolumeMounts(container corev1.Container, pvc string) error {
	for i := range container.VolumeMounts {
		if container.VolumeMounts[i].MountPath == "/var/coredump" {
			return fmt.Errorf("Failed to mount the volume %q to path \"/var/coredump\" in container %q, volume %q is already mounted to the path",
				pvc, container.Name, container.VolumeMounts[i].Name)
		}
	}
	return nil
}

func createPatch(oldPod, newPod []byte) ([]byte, error) {
	patchOperations, err := jsonpatch.CreatePatch(oldPod, newPod)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteString("[")
	l := len(patchOperations)
	for i, patchOperation := range patchOperations {
		buf, err := patchOperation.MarshalJSON()
		if err != nil {
			return nil, err
		}
		b.Write(buf)
		if i < l-1 {
			b.WriteString(",")
		}
	}
	b.WriteString("]")
	return b.Bytes(), nil
}
