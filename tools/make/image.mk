# This is a wrapper to build and push docker image
#
# All make targets related to docker image are defined in this file.

.PHONY: image.build.%
image.build.%:
	@$(LOG_TARGET)
	@docker login
	$(eval COMMAND := $(word 1,$(subst ., ,$*)))
	$(eval IMAGE_NAME := $(COMMAND))
	@$(call log, "Building image $(IMAGE_NAME):$(VERSION) in linux/amd64 and linux/arm64")
	@$(call log, "Creating image tag $(REGISTRY)/$(IMAGE_NAME):$(VERSION) in linux/amd64 and linux/arm64")
	@! ( docker buildx ls | grep $(IMAGE_NAME)-builder ) && \
	docker buildx create --use --platform=linux/amd64,linux/arm64 --name $(IMAGE_NAME)-builder ; \
	docker buildx build \
		--builder $(IMAGE_NAME)-builder \
		--tag $(REGISTRY)/$(IMAGE_NAME):$(VERSION) \
		--platform=linux/amd64,linux/arm64 \
		--file tools/docker/$(IMAGE_NAME)/Dockerfile \
		--push \
		.

##@ Image

.PHONY: image.release
image.release: ## Push docker images to registry.
image.release: image.build.fence image.build.fence-proxy
