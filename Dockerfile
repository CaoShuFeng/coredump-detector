FROM golang:1.10

WORKDIR /go/src
ADD coredump-detector /go/src/coredump-detector/
CMD [ "/go/src/coredump-detector/coredump-detector", "-v=10", "--alsologtostderr", "--client-ca-file", "/etc/secret-volume/ca", "--tls-cert-file", "/etc/secret-volume/servercert", "--tls-private-key-file", "/etc/secret-volume/secretkey", "--bind-port", "8000" ]
