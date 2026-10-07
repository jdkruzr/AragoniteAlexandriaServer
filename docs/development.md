# Development

## Requirements

- Go 1.25
- Docker with Compose v2 for integration work
- Optional: Helm 3 and Terraform 1.7+

## Checks

```bash
go test ./...
go test -race ./...
ALEXANDRIA_RUNTIME_PASSWORD=ci-only-not-a-deployment-secret ALEXANDRIA_OBJECT_ACCESS_KEY=ci-only ALEXANDRIA_OBJECT_SECRET_KEY=ci-only-secret docker compose -f deploy/compose/compose.yml config --quiet
helm lint deploy/helm/aragonite-alexandria-server --set database.url=postgres://example
terraform -chdir=deploy/terraform/aws fmt -check
terraform -chdir=deploy/terraform/aws validate
```

Tests should use synthetic data. Do not copy the live UltraBridge databases or
source roots into this repository, its CI environment, or ordinary developer
workspaces.

## Alexandria client interop

`cmd/alexandria-lab` runs the real library runtime behind UltraBridge
assetlab's command-line contract, so the Alexandria client's Kotlin HTTP tests
can drive this server. Use disposable fixtures only:

```bash
go build -o /tmp/alexandria-lab ./cmd/alexandria-lab
export FORESTREAD_TEST_SERVER=/tmp/alexandria-lab \
  ALEXANDRIA_LAB_DATABASE_URL='postgres://postgres:...@127.0.0.1:55432/postgres?sslmode=disable' \
  ALEXANDRIA_LAB_S3_ENDPOINT=http://127.0.0.1:58333
cd ../AragoniteAlexandria
../rhizome/client-kotlin/gradlew -p core/reader -PrhizomeCheckout=../rhizome test \
  --tests 'com.aragonite.alexandria.core.reader.RestorePublicationTest' \
  --tests 'com.aragonite.alexandria.core.reader.ReaderHttpInteropTest'
```

Each `--db` path maps to its own database `alexandria_lab_<hash>` and object
prefix; they accumulate in the disposable cluster. `ReaderHttpInteropTest`
cases that need legacy writer-only sync or open the server's SQLite file do
not apply to this server.

## Porting from UltraBridge

- Port one behavioral vertical slice at a time.
- Copy the smallest proven package plus its tests and fixtures.
- Replace filesystem/database dependencies at the package boundary before
  mounting its routes.
- Preserve copyright/license headers and record the port in `NOTICE`.
- Never introduce an import of `github.com/sysop/ultrabridge`.
