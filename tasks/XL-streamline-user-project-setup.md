# XL: Streamline User Project Setup DX

## Problem

Currently, users who want to use swe-swe to develop their own projects must:

1. Clone the entire swe-swe repository
2. Understand swe-swe's internal structure
3. Manage both their project AND the swe-swe repo
4. Set `WORKSPACE_DIR` environment variable correctly
5. Navigate to docker compose configs buried in swe-swe's repo structure

This creates unnecessary friction and poor developer experience for the primary use case.

## Current User Flow (Problematic)
```bash
git clone https://github.com/choonkeat/swe-swe.git
cd swe-swe
# Create .env file with API keys
WORKSPACE_DIR=/path/to/my/project make docker-compose-dev-up
```

## Desired User Flow (Streamlined)
```bash
cd my-project
curl -sSL get.swe-swe.com | bash  # or: swe-swe init
swe-swe up  # or: docker-compose up
```

## Implementation Plan

### Phase 1: Docker Images & Templates
- [ ] Create Makefile targets for building Docker images for all services
- [ ] Set up automated publishing to Docker Hub via GitHub Actions
- [ ] Create standalone docker-compose template in `/templates/user-project/`
- [ ] Template should mount current directory as workspace by default
- [ ] Include .env.example template with required environment variables

### Phase 2: Init Command
- [ ] Add `init` subcommand to main.go that:
  - Downloads docker-compose.yml to current directory
  - Downloads .env.example as .env
  - Provides setup instructions
- [ ] Add `up`/`down` subcommands that wrap docker-compose operations
- [ ] Make current directory the default workspace (not `../../helloworld`)

### Phase 3: Install Script
- [ ] Create `/scripts/install.sh` that:
  - Detects OS/architecture
  - Downloads appropriate swe-swe binary
  - Runs `swe-swe init` automatically
- [ ] Host script at get.swe-swe.com or similar domain
- [ ] Add one-liner installation to documentation

### Phase 4: CI/CD Pipeline
- [ ] GitHub Actions workflow to:
  - Build multi-arch Docker images on releases
  - Push to Docker Hub
  - Build and publish binaries
  - Update install script with latest versions

### Phase 5: Documentation Update
- [ ] Rewrite "Using swe-swe to Develop Your Own Project" section
- [ ] Remove git clone requirement
- [ ] Show new streamlined flow
- [ ] Add troubleshooting section for Docker/networking issues

## Technical Details

### Docker Images Needed
- `swe-swe/claude` - Claude Code agent container
- `swe-swe/goose` - Goose agent container  
- `swe-swe/vscode` - VSCode web container
- `swe-swe/claude-code-webui` - Alternative Claude interface
- `swe-swe/claudia` - Another Claude interface
- `swe-swe/traefik` - Reverse proxy with proper config

### Template Structure
```
templates/user-project/
├── docker-compose.yml     # Uses pre-built images from Docker Hub
├── .env.example          # Template with required variables
└── README.md            # Setup instructions
```

### New CLI Commands
```bash
swe-swe init              # Bootstrap current directory
swe-swe up               # Start services
swe-swe down             # Stop services
swe-swe logs             # View service logs
swe-swe status           # Check service status
```

## Success Criteria

- [ ] User can set up swe-swe in their project with single command
- [ ] No need to clone swe-swe repository
- [ ] No need to understand swe-swe internals
- [ ] Services automatically work on user's current project
- [ ] Clear error messages and troubleshooting guidance

## Estimated Effort
**Size: XL** - This is a significant architectural change affecting:
- Build system and Docker workflows
- CLI interface and commands
- Documentation and user onboarding
- CI/CD pipeline setup
- Multiple service configurations

**Timeline:** 2-3 weeks for full implementation

## Dependencies
- Docker Hub account for publishing images
- Domain/hosting for install script
- GitHub Actions for automation