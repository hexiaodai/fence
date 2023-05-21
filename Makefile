SHELL := /bin/bash

VERSION := $(shell git describe --tags --abbrev=8)
ifeq ($(VERSION),)
	VERSION = 0.0.0-dev
endif

REGISTRY_SERVER_ADDRESS ?= registry-1.docker.io
REPOSITORY ?= hejianmin

.PHONY: build helm.build image.build image.build.proxy
build: helm.build image.build image.build.proxy

helm.build:
	@helm package charts -d dist --debug --version $(VERSION) --app-version $(VERSION)
	@helm -n fence template dist/fence-$(VERSION).tgz > deploy/fence.yaml

image.build:
	@docker login $(REGISTRY_SERVER_ADDRESS)
	@! ( docker buildx ls | grep fence-builder ) && \
	docker buildx create --use --platform=linux/amd64,linux/arm64 --name fence-builder ;\
	docker buildx build \
		--builder fence-builder \
		--tag $(REGISTRY_SERVER_ADDRESS)/$(REPOSITORY)/fence:$(VERSION) \
		--platform=linux/amd64,linux/arm64 \
		--push \
		-f tools/docker/fence/Dockerfile \
		.

image.build.proxy:
	@docker login
	@! ( docker buildx ls | grep fence-proxy-builder ) && \
	docker buildx create --use --platform=linux/amd64,linux/arm64 --name fence-proxy-builder ;\
	docker buildx build \
		--builder fence-proxy-builder \
		--tag $(REGISTRY_SERVER_ADDRESS)/$(REPOSITORY)/fence-proxy:$(VERSION) \
		--platform=linux/amd64,linux/arm64 \
		--push \
		-f tools/docker/fence-proxy/Dockerfile \
		.
