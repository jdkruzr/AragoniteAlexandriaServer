# Development

## Requirements

- Go 1.25
- Docker with Compose v2 for integration work
- Optional: Helm 3 and Terraform 1.7+

## Checks

```bash
go test ./...
go test -race ./...
docker compose -f deploy/compose/compose.yml config --quiet
helm lint deploy/helm/aragonite-alexandria-server --set database.url=postgres://example
terraform -chdir=deploy/terraform/aws fmt -check
terraform -chdir=deploy/terraform/aws validate
```

Tests should use synthetic data. Do not copy the live UltraBridge databases or
source roots into this repository, its CI environment, or ordinary developer
workspaces.

## Porting from UltraBridge

- Port one behavioral vertical slice at a time.
- Copy the smallest proven package plus its tests and fixtures.
- Replace filesystem/database dependencies at the package boundary before
  mounting its routes.
- Preserve copyright/license headers and record the port in `NOTICE`.
- Never introduce an import of `github.com/sysop/ultrabridge`.
