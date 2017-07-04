FROM golang:1.8

WORKDIR /go/src/app
COPY . .

RUN go get -u github.com/golang/dep/cmd/dep
RUN dep ensure
RUN go test
RUN go tool vet .

RUN go-wrapper install

CMD ["go-wrapper", "run"]
