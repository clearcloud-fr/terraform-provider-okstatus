# terraform-provider-okstatus

A Terraform provider for [OKStatus](https://okstatus.eu) — manage monitors, incidents, status
pages, alert channels and maintenance windows as code, against OKStatus's public API
(`api-v1`, Max plan required for an API key).

> **Naming note**: the repo/binary is `terraform-provider-okstatus`, matching Terraform's
> provider resolution convention (`terraform-provider-<NAME>`). The **registry namespace**
> is `clearcloud-fr` (`source = "clearcloud-fr/okstatus"`), and the **Go module path**
> (`github.com/Clearcloud-Fr/terraform-provider-okstatus`) matches the GitHub org this repo
> actually lives under.

## Status

Hand-written against `terraform-plugin-framework`, built directly from OKStatus's own
`api-v1` OpenAPI spec (`https://api.okstatus.eu/api/v1/openapi.json`) rather than generated —
the schemas mirror `ok-status/apps/api/src/routes/api-v1.ts` field-for-field.

Verified in this environment: `go build`/`go vet` clean, and a full local
`terraform plan / apply / plan (no-op) / apply (update) / import / destroy` cycle against a
throwaway mock server standing in for `api-v1` — see "Local development" below to reproduce.
**Not yet exercised against a real, deployed `ok-status-backend`** — do that before trusting
this against production infrastructure.

## Resources

| Resource | Maps to |
|---|---|
| `okstatus_monitor` | `/monitors` |
| `okstatus_incident` | `/incidents` (see note below — no plain field PATCH) |
| `okstatus_status_page` | `/status-pages` |
| `okstatus_alert_channel` | `/alert-channels` |
| `okstatus_maintenance` | `/maintenances` |

## Data sources

| Data source | Maps to |
|---|---|
| `okstatus_org` | `/org` (read-only) |

## Provider configuration

```hcl
provider "okstatus" {
  endpoint = "https://api.okstatus.eu/api/v1" # optional, this is the default
  api_key  = "oks_live_..."                   # or set OKSTATUS_API_KEY
}
```

See `examples/main.tf` for a complete, `terraform validate`-checked example covering every
resource and the data source.

## `okstatus_incident` is not a normal CRUD resource

`PATCH /incidents/:id` doesn't accept arbitrary field updates — it appends a timeline entry
and moves the incident's `status`. So on this resource, `status` and `message` are both
`Required`: every `terraform apply` that touches an incident's `status` posts a new `message`
alongside it. `title`, `severity`, and `monitor_id` are set at creation and marked
`RequiresReplace` — the API has no way to change them afterwards. There's also no
`DELETE /incidents/:id`; `terraform destroy` resolves the incident (if not already resolved)
and drops it from state, but the record stays in OKStatus.

## Local development

Requires Go ≥ 1.22 (this repo's own `go.mod` pins `go 1.25.0` — resolved automatically via
Go's toolchain auto-download if your local `go` is older and `GOTOOLCHAIN=auto`, the default
since Go 1.21).

```bash
go build -o terraform-provider-okstatus .
```

Point Terraform at the local binary instead of a registry with a CLI config file:

```hcl
# dev.tfrc
provider_installation {
  dev_overrides {
    "registry.terraform.io/clearcloud-fr/okstatus" = "/absolute/path/to/this/repo"
  }
  direct {}
}
```

```bash
export TF_CLI_CONFIG_FILE=/absolute/path/to/dev.tfrc
cd examples
terraform plan   # no `terraform init` needed/possible with dev_overrides active
```

## Not implemented (yet)

- `okstatus_status_page_groups` (nested groups/monitors under a status page — `PUT
  /status-pages/:id/groups` isn't exposed by `api-v1` yet, see the OpenAPI roadmap).
- Alert channel `type`-specific config validation (the provider accepts any string map for
  `config`; `ok-status-backend` validates the actual per-type shape and will reject a bad one
  at apply time with a clear error).
- Terraform Registry publication (GPG signing, GoReleaser, `terraform-registry-manifest.json`)
  — not worth doing before this has seen real usage.
- Acceptance tests (`terraform-plugin-testing`) against a real `ok-status-backend` — the
  resources above were verified against a mock server standing in for the real API; real
  acceptance tests need real credentials and a deployed backend to run against.
