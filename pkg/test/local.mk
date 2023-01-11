###
### Integration Tests
###
setup_envtest_bin = $(go_bin)/setup-envtest

clean_targets += .envtest-clean

# Prepare binary
$(setup_envtest_bin): export GOBIN = $(go_bin)
$(setup_envtest_bin): | $(go_bin)
	go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

.PHONY: test-integration
test-integration: $(setup_envtest_bin) ## Run integration tests against code
	$(setup_envtest_bin) $(ENVTEST_ADDITIONAL_FLAGS) use '$(ENVTEST_K8S_VERSION)!'
	@chmod -R +w $(go_bin)/k8s
	export KUBEBUILDER_ASSETS="$$($(setup_envtest_bin) $(ENVTEST_ADDITIONAL_FLAGS) use -i -p path '$(ENVTEST_K8S_VERSION)!')" && \
	go test -tags=integration -coverprofile cover.out -covermode atomic ./...

.PHONY: .envtest-clean
.envtest-clean:
	rm -f $(setup_envtest_bin)
