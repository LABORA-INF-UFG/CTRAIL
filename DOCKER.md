# Docker Usage Guide for CAMARA to NEF Gateway

This guide explains how to use Docker with the CAMARA to NEF Gateway project.

## Quick Start

### 1. Build and Run (Development)
```bash
# Using Docker Compose (recommended)
docker-compose up --build

# Or using make
make run
```

### 2. Development with Hot Reload
```bash
# Start development environment
make dev

# Or directly with docker-compose
docker-compose -f docker-compose.dev.yml up --build
```

### 3. Production Deployment
```bash
# Deploy to production
make prod

# Or directly with docker-compose
docker-compose -f docker-compose.prod.yml up --build -d
```

## Available Docker Files

### Main Dockerfile
- **Purpose**: Production-ready multi-stage build
- **Features**: Optimized binary, security hardening, health checks
- **Usage**: `docker build -t ctrail .`

### Dockerfile.dev
- **Purpose**: Development with hot reload and debugging
- **Features**: Air hot reload, Delve debugger, development tools
- **Usage**: Used automatically with `docker-compose.dev.yml`

### Docker Compose Files

#### docker-compose.yml
- **Purpose**: Standard deployment (compatible with free5gc)
- **Features**: Basic configuration, free5gc network integration
- **Usage**: `docker-compose up`

#### docker-compose.dev.yml
- **Purpose**: Development environment
- **Features**: Hot reload, mock NEF, development databases
- **Usage**: `docker-compose -f docker-compose.dev.yml up`

#### docker-compose.prod.yml
- **Purpose**: Production deployment
- **Features**: Resource limits, monitoring, enhanced security
- **Usage**: `docker-compose -f docker-compose.prod.yml up`

## Environment Configuration

### Required Environment Variables
```bash
# Server
SERVER_PORT=8080         # Server port (default: 8080)
LOG_LEVEL=info           # Logging level (debug, info, warn, error)

# NEF Connection
NEF_BASE_URL=http://localhost:8000  # NEF server URL
NEF_TOKEN=your-token-here           # NEF authentication token

# Security (Production)
SECURITY_REQUIRE_HTTPS=true         # Require HTTPS in production
SECURITY_RATE_LIMIT_PER_MINUTE=100  # Rate limiting
```

### Configuration Files
Copy `.env.example` to `.env` and customize:
```bash
cp .env.example .env
# Edit .env with your configuration
```

## Make Commands

The project includes a comprehensive Makefile for easy Docker management:

### Development
```bash
make dev          # Start development environment
make dev-bg       # Start development in background
make dev-logs     # View development logs
make dev-stop     # Stop development environment
make dev-clean    # Clean development (remove volumes)
```

### Production
```bash
make build        # Build production image
make run          # Run production container
make run-bg       # Run production in background
make prod         # Deploy production environment
make prod-logs    # View production logs
make prod-stop    # Stop production environment
```

### Testing
```bash
make test         # Run tests in container
make test-coverage # Run tests with coverage
```

### Utilities
```bash
make logs         # Show container logs
make ps           # Show running containers
make stop         # Stop all containers
make restart      # Restart containers
make clean        # Clean up Docker resources
make clean-all    # Clean up all Docker resources
make health       # Check container health
make stats        # Show container stats
make shell        # Access container shell
make shell-dev    # Access development container shell
```

### Monitoring
```bash
make monitor      # Start monitoring stack (Prometheus + Grafana)
make monitor-stop # Stop monitoring stack
```

## Docker Image Features

### Security Features
- Non-root user (appuser:appgroup)
- Minimal Alpine Linux base
- Security-hardened build flags
- Health checks included
- Resource limits in production

### Optimization Features
- Multi-stage builds for minimal image size
- Layer caching optimization
- .dockerignore for build efficiency
- Optimized Go build flags (-ldflags="-w -s")

### Development Features
- Hot reload with Air
- Debugging support with Delve
- Development tools included
- Volume mounts for live development

## Network Configuration

### free5gc Integration
The gateway integrates with free5gc networks:
```yaml
networks:
  free5gc-compose_privnet:
    external: true
```

### Development Networks
Isolated networks for development:
```yaml
networks:
  dev_network:
    driver: bridge
    ipam:
      config:
        - subnet: 172.25.1.0/24
```

## Health Checks

All containers include health checks:
```bash
# Check container health
docker-compose exec ctrail wget --spider http://localhost:8080/health

# Using make
make health
```

## Logs and Monitoring

### Viewing Logs
```bash
# All logs
docker-compose logs -f

# Specific service
docker-compose logs -f ctrail

# Using make
make logs
```

### Persistent Logs
Logs are persisted in volumes:
- Development: `./logs:/app/logs`
- Production: `ctrail_logs:/app/logs`

## Troubleshooting

### Common Issues

1. **Port conflicts**: Change `SERVER_PORT` in `.env`
2. **NEF connection**: Verify `NEF_BASE_URL` and network connectivity
3. **Permission issues**: Ensure proper file ownership
4. **Build failures**: Check Go module dependencies

### Debug Mode
```bash
# Start with debug logging
LOG_LEVEL=debug docker-compose up

# Access container shell
make shell
```

### Container Stats
```bash
# Monitor resource usage
make stats

# Check container processes
docker-compose top
```

## Advanced Usage

### Custom Builds
```bash
# Build with specific target
docker build --target development -t ctrail:dev .

# Build with build args
docker build -t ctrail .
```

### Volume Management
```bash
# List volumes
docker volume ls | grep ctrail

# Remove volumes
docker volume rm ctrail_logs
```

### Network Inspection
```bash
# Inspect networks
docker network ls | grep ctrail
docker network inspect ctrail_dev_network
```

## Production Checklist

Before deploying to production:

- [ ] Set `SECURITY_REQUIRE_HTTPS=true`
- [ ] Configure proper `NEF_BASE_URL`
- [ ] Set strong `NEF_TOKEN`
- [ ] Configure resource limits
- [ ] Set up log rotation
- [ ] Configure monitoring
- [ ] Test health checks
- [ ] Verify backup procedures

## Support

For more information:
- Check logs: `make logs`
- Container stats: `make stats`
- Health status: `make health`
- Shell access: `make shell`
