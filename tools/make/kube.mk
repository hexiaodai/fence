##@ Kubernetes Development

.PHONY: kube.generate
kube.generate: ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
kube.generate:
	@$(LOG_TARGET)
	@tools/bin/controller-gen object:headerFile="$(ROOT_DIR)/tools/boilerplate/boilerplate.go.txt" paths="$(ROOT_DIR)/api/..."
	@tools/bin/controller-gen crd paths="$(ROOT_DIR)/api/..." output:crd:dir="$(ROOT_DIR)/deploy/charts/crds"
