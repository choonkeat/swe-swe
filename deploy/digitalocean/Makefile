.PHONY: build

# DigitalOcean Packer build target
build:
	@echo "==> Checking prerequisites for DigitalOcean Packer build..."
	@command -v packer >/dev/null 2>&1 || { echo "ERROR: packer not found"; echo "See deploy/digitalocean/DEVELOPER.md for installation instructions"; exit 1; }
	@packer -version | grep -qE 'Packer v1\.(1[4-9]|[2-9][0-9])' || { echo "ERROR: packer version must be >= v1.14.0"; echo "See https://developer.hashicorp.com/packer/install"; exit 1; }
	@test -f ../../dist/swe-swe.linux-amd64 || { echo "ERROR: binary not found at ../../dist/swe-swe.linux-amd64"; echo "Run 'make build' at repo root first"; exit 1; }
	@test -f template.pkr.hcl || { echo "ERROR: Packer template not found"; exit 1; }
	@test -n "$$DIGITALOCEAN_API_TOKEN" || { echo "ERROR: DIGITALOCEAN_API_TOKEN environment variable not set"; echo "See DEVELOPER.md for API token setup"; exit 1; }
	@echo "✓ All prerequisites met"
	@echo ""
	@PROMPTS=$$(SWE_SWE_PASSWORD="$(SWE_SWE_PASSWORD)" ENABLE_HARDENING="$(ENABLE_HARDENING)" GIT_CLONE_URL="$(GIT_CLONE_URL)" ../../scripts/prompt-password.sh); \
	SWE_SWE_PASSWORD=$$(echo "$$PROMPTS" | sed -n '1p'); \
	ENABLE_HARDENING=$$(echo "$$PROMPTS" | sed -n '2p'); \
	GIT_CLONE_URL=$$(echo "$$PROMPTS" | sed -n '3p'); \
	echo ""; \
	if [ -z "$(REGION)" ]; then \
		read -p "region (no default; nyc1, nyc3, sfo3, lon1, sgp1, tor1, blr1, ams3, fra1): " REGION; \
	else \
		REGION="$(REGION)"; \
	fi; \
	if [ -z "$$REGION" ]; then \
		echo ""; \
		echo "ERROR: region is required"; \
		echo ""; \
		IMAGE_VERSION=$$(git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//' || date +%Y%m%d)-$$(git rev-parse --short HEAD); \
		echo "Run manually instead:"; \
		echo ""; \
		echo "  cd deploy/digitalocean && packer build \\"; \
		echo "    -var \"region=nyc3\" \\"; \
		echo "    -var \"image_version=$$IMAGE_VERSION\" \\"; \
		echo "    template.pkr.hcl"; \
		echo ""; \
		exit 1; \
	fi; \
	if [ -z "$(DROPLET_SIZE)" ]; then \
		read -p "droplet_size (default: s-2vcpu-4gb): " DROPLET_SIZE; \
		DROPLET_SIZE=$${DROPLET_SIZE:-s-2vcpu-4gb}; \
	else \
		DROPLET_SIZE="$(DROPLET_SIZE)"; \
	fi; \
	echo ""; \
	echo "==> Available swe-swe init flags:"; \
	echo ""; \
	../../dist/swe-swe.linux-amd64 init -h 2>&1; \
	echo ""; \
	if [ -z "$(INIT_FLAGS)" ]; then \
		read -p "swe-swe init flags (default: --with-docker --ssl=selfsign): " INIT_FLAGS; \
		INIT_FLAGS=$${INIT_FLAGS:---with-docker --ssl=selfsign}; \
	else \
		INIT_FLAGS="$(INIT_FLAGS)"; \
	fi; \
	if [ -z "$(IMAGE_NAME)" ]; then \
		read -p "image_name (default: swe-swe): " IMAGE_NAME; \
		IMAGE_NAME=$${IMAGE_NAME:-swe-swe}; \
	else \
		IMAGE_NAME="$(IMAGE_NAME)"; \
	fi; \
	IMAGE_VERSION=$$(git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//' || date +%Y%m%d)-$$(git rev-parse --short HEAD); \
	echo ""; \
	echo "Building with:"; \
	echo "  region: $$REGION"; \
	echo "  droplet_size: $$DROPLET_SIZE"; \
	echo "  init_flags: $$INIT_FLAGS"; \
	echo "  image_name: $$IMAGE_NAME"; \
	echo "  image_version: $$IMAGE_VERSION"; \
	echo "  enable_hardening: $$ENABLE_HARDENING"; \
	echo "  git_clone_url: $${GIT_CLONE_URL:-<skip>}"; \
	echo ""; \
	packer build \
		-var "region=$$REGION" \
		-var "droplet_size=$$DROPLET_SIZE" \
		-var "init_flags=$$INIT_FLAGS" \
		-var "image_name=$$IMAGE_NAME" \
		-var "image_version=$$IMAGE_VERSION" \
		-var "swe_swe_password=$$SWE_SWE_PASSWORD" \
		-var "enable_hardening=$$ENABLE_HARDENING" \
		-var "git_clone_url=$$GIT_CLONE_URL" \
		template.pkr.hcl
