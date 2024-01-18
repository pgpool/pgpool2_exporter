GO     := go
GOPATH := $(firstword $(subst :, ,$(shell $(GO) env GOPATH)))
GOARCH ?= amd64

PROMU       ?= $(GOPATH)/bin/promu
pkgs         = $(shell $(GO) list ./... | grep -v /vendor/)

PREFIX                  ?= $(shell pwd)
BIN_DIR                 ?= $(shell pwd)
TARBALLS_DIR            ?= $(shell pwd)/.tarballs
DOCKER_REPO             ?= pgpool
DOCKER_IMAGE_NAME       ?= pgpool2_exporter
DOCKER_IMAGE_TAG        ?= $(subst /,-,$(shell git rev-parse --abbrev-ref HEAD))


build: promu
	@echo ">> building binaries"
	@$(PROMU) build -v --prefix $(PREFIX)
	mv pgpool2_exporter pgpool2_exporter-$(GOARCH)

crossbuild: promu
	@echo ">> building cross-platform binaries"
	@$(PROMU) crossbuild

promu:
	@GOOS=$(shell uname -s | tr A-Z a-z) \
	GOARCH=$(subst x86_64,amd64,$(patsubst i%86,386,$(shell uname -m))) \
	$(GO) install github.com/prometheus/promu

tarball: build
	@echo ">> building release tarball"
	@$(PROMU) tarball --prefix $(PREFIX) $(BIN_DIR)

tarballs: crossbuild
	@echo ">> building release tarballs"
	@$(PROMU) crossbuild tarballs

docker:
	@echo ">> building docker image"
	@docker buildx build --push --platform linux/amd64,linux/arm64 -t "$(DOCKER_REPO)/$(DOCKER_IMAGE_NAME):$(DOCKER_IMAGE_TAG)" .

.PHONY: promu build crossbuild tarball tarballs docker
