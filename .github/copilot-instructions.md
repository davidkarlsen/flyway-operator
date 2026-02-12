# GitHub Copilot Instructions for flyway-operator

## Project Overview

This is a Kubernetes operator for managing [Flyway](https://flywaydb.org/) database migrations. The operator is built using [Kubebuilder](https://book.kubebuilder.io/) and follows the Kubernetes Operator pattern.

**Key Technologies:**
- Go (see go.mod for version)
- Kubernetes Custom Resource Definitions (CRDs)
- Kubebuilder v4
- Controller-runtime
- Operator SDK

## Project Structure

- `api/v1alpha1/` - API definitions and CRD types (Migration resource)
- `internal/controller/` - Reconciliation logic and controllers
- `config/` - Kubernetes manifests, CRDs, RBAC, and samples
- `config/helm-chart/` - Helm chart for deployment
- `cmd/main.go` - Entry point for the operator

## Development Workflow

### Before Making Changes

1. **Always run** `make manifests fmt vet` before opening a PR to:
   - Update API definition changes
   - Format the code
   - Vet the code for common issues

2. **Install CRDs** before testing:
   ```sh
   make install
   ```

3. **Run the operator** locally:
   ```sh
   make run
   ```

### Building and Testing

- **Run tests:** `make test`
- **Build Docker image:** `make docker-build IMG=<registry>/flyway-operator:tag`
- **Deploy to cluster:** `make deploy IMG=<registry>/flyway-operator:tag`
- **Lint code:** Uses golangci-lint (configured in workflow)

### API Modifications

When editing API definitions in `api/v1alpha1/`:

1. Add appropriate kubebuilder markers/annotations:
   - `+kubebuilder:validation:Required` - for required fields
   - `+kubebuilder:validation:Optional` - for optional fields
   - `+kubebuilder:validation:Pattern` - for pattern validation (e.g., JDBC URLs)
   - `+kubebuilder:default` - for default values

2. Run `make manifests` to regenerate CRDs and RBAC

3. Run `make generate` to update DeepCopy methods

## Coding Standards

### Go Code Style

- Follow standard Go conventions and formatting
- Use meaningful variable and function names
- Add comments for exported types and functions
- Include license header (Apache 2.0) in new files

### Kubebuilder Patterns

- Controllers should follow the reconciliation loop pattern
- Use conditions to track resource status (see `MigrationStatus.Conditions`)
- Implement proper error handling and status updates
- Use annotations for operator metadata (prefix: `flyway-operator.davidkarlsen.com/`)

### Constants and Configuration

- Define constants in appropriate files (e.g., `migration_types.go` for API constants)
- Use the `Prefix` constant for all operator-specific annotations
- Environment variable handling uses `github.com/caitlinelfring/go-env-default`

## Testing

- Unit tests should be in `*_test.go` files
- Integration tests use Ginkgo/Gomega framework
- Controller tests are in `internal/controller/migration_controller_test.go`
- Always include tests for new features and bug fixes

## Common Tasks

### Adding a New Field to Migration CRD

1. Edit `api/v1alpha1/migration_types.go`
2. Add kubebuilder validation markers
3. Run `make manifests generate`
4. Update samples in `config/samples/`
5. Run tests to ensure compatibility

### Modifying Controller Logic

1. Edit `internal/controller/migration_controller.go`
2. Update reconciliation logic
3. Add or update RBAC markers if accessing new resources
4. Run `make manifests` to update RBAC
5. Test with `make test` and manual verification

## Dependencies

- Use `go mod tidy` to manage dependencies
- Check for security vulnerabilities before adding new dependencies
- Prefer well-maintained libraries with active communities

## Documentation

- Update `README.md` for user-facing changes
- Update `USING.md` for usage examples
- Update `INSTALLING.md` for installation changes
- Update `CONTRIBUTING.md` for development process changes

## Important Notes

- **The operator creates Kubernetes Jobs** that run the Flyway Docker image
- **Migration source** can be from ConfigMap, Secret, or Git repository
- **Database credentials** are stored in Kubernetes Secrets
- **Pausing migrations** is supported via annotations
- **Conditions** track migration status and should be updated appropriately
- **License:** Apache 2.0 - include headers in new files

## CI/CD

- GitHub Actions workflows are in `.github/workflows/`
- `build.yaml` - main build, test, and release workflow
- `chart-test.yaml` - Helm chart testing
- `codeql.yaml` - security scanning
- All PRs must pass CI checks before merging

## Helpful Commands

```sh
# Full development cycle
make manifests generate fmt vet test

# Install and run locally
make install run

# Build and deploy
make docker-build docker-push deploy IMG=<your-image>

# Clean up
make uninstall undeploy
```
