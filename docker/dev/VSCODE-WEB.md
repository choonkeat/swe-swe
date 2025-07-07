# VSCode Web Service

The VSCode web service has been integrated into the docker-compose.yml configuration.

## Usage

1. Start the services:
   ```bash
   docker compose up -d vscode
   ```

2. Access VSCode in your browser:
   - URL: `http://vscode.<your-domain>:7000`
   - Password: Set in `.env` file as `VSCODE_PASSWORD` (default: `changeme`)

## Features

- Full VSCode functionality in the browser
- Project files mounted at `/home/coder/project`
- Persistent configuration and extensions
- Integrated with Traefik for authentication
- Resource limits: 2 CPUs and 2GB memory (max)

## Configuration

Environment variables in `.env`:
- `VSCODE_PASSWORD`: Authentication password
- `TZ`: Timezone (default: UTC)

## Security

- Protected by Traefik basic auth middleware
- Change the default password in production
- Consider using HTTPS in production environments