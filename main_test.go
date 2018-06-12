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
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sclock "k8s.io/apimachinery/pkg/util/clock"
)

type testCase struct {
	requestBody      string
	request          v1beta1.AdmissionReview
	expectStatus     int
	expectedResponse v1beta1.AdmissionReview
	expectedError    string
}

var patchType = v1beta1.PatchTypeJSONPatch

var testCases []testCase = []testCase{
	{
		// invalid request body
		requestBody:   "invalid request body",
		expectStatus:  http.StatusBadRequest,
		expectedError: "Failed to decode request body err",
	},
	{
		// no annotation set, do nothing
		request: v1beta1.AdmissionReview{
			Request: &v1beta1.AdmissionRequest{
				UID:       "fake uuid",
				Resource:  metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
				Operation: v1beta1.Create,
				Object: runtime.RawExtension{
					Object: &corev1.Pod{},
				},
			},
		},
		expectStatus: http.StatusOK,
		expectedResponse: v1beta1.AdmissionReview{
			Response: &v1beta1.AdmissionResponse{
				UID:     "fake uuid",
				Allowed: true,
			},
		},
	},
	{
		// invalid operation, we do nothing
		request: v1beta1.AdmissionReview{
			Request: &v1beta1.AdmissionRequest{
				UID:       "fake uuid",
				Resource:  metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
				Operation: v1beta1.Update,
				Object: runtime.RawExtension{
					Object: &corev1.Pod{},
				},
			},
		},
		expectStatus:     http.StatusOK,
		expectedResponse: v1beta1.AdmissionReview{},
	},
	{
		// invalid object, we do nothing
		request: v1beta1.AdmissionReview{
			Request: &v1beta1.AdmissionRequest{
				UID:       "fake uuid",
				Resource:  metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "services"},
				Operation: v1beta1.Create,
				Object: runtime.RawExtension{
					Object: &corev1.Service{},
				},
			},
		},
		expectStatus:     http.StatusOK,
		expectedResponse: v1beta1.AdmissionReview{},
	},
	{
		// test pvc has been mounted successfully.
		request: v1beta1.AdmissionReview{
			Request: &v1beta1.AdmissionRequest{
				UID:       "fake uuid",
				Resource:  metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
				Operation: v1beta1.Create,
				Object: runtime.RawExtension{
					Object: &corev1.Pod{
						TypeMeta: metav1.TypeMeta{
							APIVersion: "v1",
							Kind:       "Pod",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name: "pod1",
							Annotations: map[string]string{
								"coredump.fujitsu.com/pvcname": "pvc1",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "container1",
								},
							},
						},
					},
				},
			},
		},
		expectStatus: http.StatusOK,
		expectedResponse: v1beta1.AdmissionReview{
			Response: &v1beta1.AdmissionResponse{
				UID:       "fake uuid",
				Allowed:   true,
				PatchType: &patchType,
				Patch:     []byte(`[{"op":"add","path":"/spec/containers/0/volumeMounts","value":[{"mountPath":"/var/coredump","name":"pvc1-1033798960","subPath":"pod1/container1"}]},{"op":"add","path":"/spec/nodeSelector","value":{"coredump":"true"}},{"op":"add","path":"/spec/volumes","value":[{"name":"pvc1-1033798960","persistentVolumeClaim":{"claimName":"pvc1"}}]}]`),
			},
		},
	},
	{
		// test pvc has been mounted successfully to init containers
		request: v1beta1.AdmissionReview{
			Request: &v1beta1.AdmissionRequest{
				UID:       "fake uuid",
				Resource:  metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
				Operation: v1beta1.Create,
				Object: runtime.RawExtension{
					Object: &corev1.Pod{
						TypeMeta: metav1.TypeMeta{
							APIVersion: "v1",
							Kind:       "Pod",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name: "pod1",
							Annotations: map[string]string{
								"coredump.fujitsu.com/pvcname": "pvc1",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "container1",
								},
							},
							InitContainers: []corev1.Container{
								{
									Name: "container1",
								},
							},
						},
					},
				},
			},
		},
		expectStatus: http.StatusOK,
		expectedResponse: v1beta1.AdmissionReview{
			Response: &v1beta1.AdmissionResponse{
				UID:       "fake uuid",
				Allowed:   true,
				PatchType: &patchType,
				Patch:     []byte(`[{"op":"add","path":"/spec/containers/0/volumeMounts","value":[{"mountPath":"/var/coredump","name":"pvc1-1033798960","subPath":"pod1/container1"}]},{"op":"add","path":"/spec/initContainers/0/volumeMounts","value":[{"mountPath":"/var/coredump","name":"pvc1-1033798960","subPath":"pod1/container1"}]},{"op":"add","path":"/spec/nodeSelector","value":{"coredump":"true"}},{"op":"add","path":"/spec/volumes","value":[{"name":"pvc1-1033798960","persistentVolumeClaim":{"claimName":"pvc1"}}]}]`),
			},
		},
	},
}

func TestPodHandler(t *testing.T) {
	clock = k8sclock.NewFakeClock(time.Unix(1033798960, 0))
	for i, tc := range testCases {
		var objJS []byte
		var err error
		if len(tc.requestBody) == 0 {
			// prepare AdmissionReview
			objJS, err = runtime.Encode(jsonSerializer, &tc.request)
			if err != nil {
				t.Fatalf("test[%d] fatal: %v", i, err)
			}
		} else {
			objJS = []byte(tc.requestBody)
		}

		request := httptest.NewRequest("POST", "http://example.com/foo", bytes.NewReader(objJS))
		request.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		podHandler(w, request)

		resp := w.Result()
		body, _ := ioutil.ReadAll(resp.Body)

		assert.Equal(t, tc.expectStatus, resp.StatusCode, "Invalid http status code returned")
		if resp.StatusCode == http.StatusOK {
			// decode the body
			response := v1beta1.AdmissionReview{}
			deserializer := codecs.UniversalDeserializer()
			_, _, err := deserializer.Decode(body, nil, &response)
			fmt.Printf("%s\n", body)
			require.Nil(t, err, "When response status is 200, the body should be an admission review")
			assert.Equal(t, tc.expectedResponse, response, fmt.Sprintf("test %d: unexpected response", i))
		} else {
			// check error mesage
			assert.Contains(t, string(body[:]), tc.expectedError)
		}
	}
}
