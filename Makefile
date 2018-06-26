# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

IMAGE = caoshufeng/coredump-detector
TAG = v0.2

build:
	CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o coredump-detector .

build-container: build
	docker build --no-cache -t $(IMAGE):$(TAG) .
test:
	go test

.PHONY: build
