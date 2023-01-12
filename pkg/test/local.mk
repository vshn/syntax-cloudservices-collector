###
### Integration Tests
###
setup_envtest_bin = $(go_bin)/setup-envtest

clean_targets += .envtest-clean .acr-clean

ACR_DB_URL ?= "postgres://reporting:reporting@localhost/appuio-cloud-reporting-test?sslmode=disable"
acr_clone_target ?= $(work_dir)/appuio-cloud-reporting

CLOUDSCALE_CRDS_PATH ?= $(shell go list -f '{{.Dir}}' -m github.com/vshn/provider-cloudscale)/package/crds

# Prepare binary
$(setup_envtest_bin): export GOBIN = $(go_bin)
$(setup_envtest_bin): | $(go_bin)
	go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

.PHONY: test-integration
test-integration: $(setup_envtest_bin) start-acr ## Run integration tests against code
	$(setup_envtest_bin) $(ENVTEST_ADDITIONAL_FLAGS) use '$(ENVTEST_K8S_VERSION)!'
	@chmod -R +w $(go_bin)/k8s
	export KUBEBUILDER_ASSETS="$$($(setup_envtest_bin) $(ENVTEST_ADDITIONAL_FLAGS) use -i -p path '$(ENVTEST_K8S_VERSION)!')" && \
	export ACR_DB_URL="$(ACR_DB_URL)" && \
	export CLOUDSCALE_CRDS_PATH="$(CLOUDSCALE_CRDS_PATH)" && \
	go test -tags=integration -coverprofile cover.out -covermode atomic ./...

.PHONY: .envtest-clean
.envtest-clean:
	rm -f $(setup_envtest_bin)

## ACR setup
$(acr_clone_target):
	git clone https://github.com/appuio/appuio-cloud-reporting $@

.PHONY: start-acr
start-acr: $(acr_clone_target) ## Starts ACR
	pushd $(acr_clone_target) && \
	make docker-compose-up && \
	make ping-postgres && \
	PGPASSWORD=reporting createdb --username=reporting -h localhost -p 5432 appuio-cloud-reporting-test || echo "already exists, skipping createdb" && \
	export ACR_DB_URL="$(ACR_DB_URL)" && \
	go run . migrate && \
	go run . migrate --seed

.PHONY: stop-acr
stop-acr: $(acr_clone_target)
	pushd $(acr_clone_target) && \
	make docker-compose-down

.PHONY: .acr-clean
.acr-clean:
	rm -Rf $(acr_clone_target)
