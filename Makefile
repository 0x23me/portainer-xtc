build:
	CCGO_ENABLEDGO=0 go build -v -ldflags="-extldflags=-static" -o portainer-xtc main.go

image: build
	docker build -t soupdiver/portainer-xtc:latest .

.PHONY: build image
