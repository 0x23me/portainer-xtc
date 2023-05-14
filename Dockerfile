FROM golang:1.20-alpine as builder

RUN apk update && apk add make

WORKDIR /usr/src/app

# pre-copy/cache go.mod for pre-downloading dependencies and only redownloading them in subsequent builds if they change
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .

RUN make build

# Build main image
FROM alpine:3 
COPY --from=builder /usr/src/app/portainer-xtc /portainer-xtc
CMD ["/portainer-xtc"]
