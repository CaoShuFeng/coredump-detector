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
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"

	"github.com/golang/glog"
	"github.com/spf13/pflag"
	"k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

var scheme = runtime.NewScheme()
var codecs = serializer.NewCodecFactory(scheme)

func init() {
	addToScheme(scheme)
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
		reviewResponse = toAdmissionResponse(err)
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
	if _, err := w.Write(resp); err != nil {
		glog.Error(err)
	}

}

func toAdmissionResponse(err error) *v1beta1.AdmissionResponse {
	return &v1beta1.AdmissionResponse{
		Result: &metav1.Status{
			Message: err.Error(),
		},
	}
}

func mutatePod(ar v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {
	glog.V(2).Info("mutating pods")
	podResource := metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	if ar.Request == nil || ar.Request.Resource != podResource {
		glog.Errorf("expect resource to be %s", podResource)
		return nil
	}

	raw := ar.Request.Object.Raw
	pod := corev1.Pod{}
	deserializer := codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(raw, nil, &pod); err != nil {
		glog.Error(err)
		return toAdmissionResponse(err)
	}
	reviewResponse := v1beta1.AdmissionResponse{}
	reviewResponse.Allowed = true
	return &reviewResponse
}
