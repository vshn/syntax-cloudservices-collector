## These are some common variables for Make

PROJECT_ROOT_DIR = .
PROJECT_NAME ?= billing-collector-cloudservices
PROJECT_OWNER ?= vshn

## BUILD:go
BIN_FILENAME ?= $(PROJECT_NAME)

work_dir ?= $(PWD)/.work
go_bin ?= $(work_dir)/bin
$(go_bin):
	@mkdir -p $@

## BUILD:docker
DOCKER_CMD ?= docker

IMG_TAG ?= latest
# Image URL to use all building/pushing image targets
CONTAINER_IMG ?= ghcr.io/$(PROJECT_OWNER)/$(PROJECT_NAME):$(IMG_TAG)

# TEST:integration
ENVTEST_ADDITIONAL_FLAGS ?= --bin-dir "$(go_bin)"
# See https://storage.googleapis.com/kubebuilder-tools/ for list of supported K8s versions
ENVTEST_K8S_VERSION = 1.24.x

DOCKER_IMAGE_GOOS = linux
DOCKER_IMAGE_GOARCH = amd64
