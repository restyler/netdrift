# Configuration Management Guide

## Overview

This project uses a secure configuration management approach that separates templates from production configurations containing sensitive data.

**Template files use `template-*.json` naming and are tracked in git.**  
**Actual config files use `*.json` naming and are kept local only.**

## Configuration Files

### Template Files (Tracked in Git)
- `configs/template-docker.json` - Basic Docker template
- `configs/template-us.json` - US development template  
- `configs/template-production.json` - Complete production template with examples
- `docker-compose.prod.yml` - Production compose template
- `docker-compose.test.yml` - Testing compose template

### Local Files (NOT Tracked in Git)
- `configs/docker-us.json` - Your US proxy configuration
- `configs/docker-eu.json` - Your EU proxy configuration  
- `configs/local.json` - Local development config
- `configs/production.json` - Production configuration
- `docker-compose.yml` - Local development compose

## Setup Instructions

### 1. Create Your Local Configuration

Copy a template and customize it:

```bash
# For Docker deployment with US proxies
cp configs/template-production.json configs/docker-us.json

# For local development
cp configs/template-us.json configs/local.json

# Edit with your actual proxy credentials
nano configs/docker-us.json
```

### 2. Update Proxy URLs

Replace template URLs with your actual proxies:

```json
{
  "upstream_proxies": [
    {
      "url": "http://207.229.93.67:1025",
      "enabled": true,
      "weight": 1
    },
    {
      "url": "http://username:password@proxy.example.com:3128",
      "enabled": true,
      "weight": 1
    }
  ]
}
```

### 3. Deploy Using Explicit Compose Files

**Recommended approach:**
```bash
# Production
make docker-prod-up

# Testing  
make docker-test-up
```

**Alternative approach:**
```bash
# Copy and customize
cp docker-compose.prod.yml docker-compose.yml
# Edit PROXY_CONFIG environment variable to point to your config
nano docker-compose.yml
# Run
docker compose up -d
```

## Security Best Practices

### ✅ DO:
- Keep sensitive configs local (automatically ignored by git)
- Copy from `template-*.json` to create your `*.json` configs
- Use descriptive names like `docker-us.json`, `docker-eu.json`
- Use the explicit compose files in `Makefile` commands
- Document your local config setup for team members

### ❌ DON'T:
- Commit files with real proxy credentials to git
- Use the same passwords across environments
- Share configuration files containing credentials
- Edit template files directly (create copies instead)

## File Naming Convention

| Pattern | Purpose | Tracked |
|---------|---------|---------|
| `configs/template-*.json` | Templates/examples | ✅ Yes |
| `configs/*.json` | Actual configurations | ❌ No |
| `docker-compose.yml` | Local development | ❌ No |

### Examples:
```bash
# Templates (tracked in git)
configs/template-docker.json
configs/template-us.json
configs/template-production.json

# Your actual configs (local only)
configs/docker-us.json
configs/docker-eu.json
configs/local.json
configs/production.json
```

## Troubleshooting

### Config Not Loading
- Check `PROXY_CONFIG` environment variable in compose file
- Verify file exists in `configs/` directory
- Ensure you copied from template and customized it
- Check Docker logs: `docker logs netdrift-proxy`

### 502 Errors
- Verify upstream proxy URLs are correct and accessible
- Test proxies directly: `curl -x http://proxy:port https://httpbin.org/ip`
- Check proxy authentication if required

### Template vs Config Confusion
- **Templates**: `template-*.json` (tracked, safe to share)
- **Configs**: `*.json` (local only, contain secrets)
- Always copy templates to create your configs 