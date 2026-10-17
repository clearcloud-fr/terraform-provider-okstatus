# terraform-provider-okstatus

A Terraform provider for [OKStatus](https://okstatus.eu) — manage monitors, monitor groups,
incidents, status pages, alert channels and maintenance windows as code, against OKStatus's public API
(`api-v1`, Max plan required for an API key).

> **Naming note**: the repo/binary is `terraform-provider-okstatus`, matching Terraform's
> provider resolution convention (`terraform-provider-<NAME>`). The **registry namespace**
> is `clearcloud-fr` (`source = "clearcloud-fr/okstatus"`), and the **Go module path**
> (`github.com/Clearcloud-Fr/terraform-provider-okstatus`) matches the GitHub org this repo
> actually lives under.

## Status

Published to the Terraform Registry as
[`clearcloud-fr/okstatus`](https://registry.terraform.io/providers/clearcloud-fr/okstatus/latest).

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
| `okstatus_monitor` | every check type but `llm` — `/monitors`, and `/monitors/:id/alert-channels` when `alert_channel_ids` is set |
| `okstatus_llm_monitor` | `/monitors` (type `llm`) — LLM canary checks: prompt + TTFT/content assertions, single region |
| `okstatus_monitor_group` | `/monitor-groups` — folders of the Monitors page, with their monitors |
| `okstatus_incident` | `/incidents` (see note below — no plain field PATCH) |
| `okstatus_status_page` | `/status-pages` |
| `okstatus_alert_channel` | `/alert-channels` |
| `okstatus_maintenance` | `/maintenances` |

## Data sources

| Data source | Maps to |
|---|---|
| `okstatus_org` | `/org` (read-only) |
| `okstatus_monitors` | `/monitors` — every monitor in the org, plus `total`; pages through the API internally |
| `okstatus_alert_channels` | `/alert-channels` — every alert channel in the org, plus `total` |
| `okstatus_monitor_groups` | `/monitor-groups` — every group with the ids of its monitors |
| `okstatus_private_agents` | `/private-agents` (read-only) — each agent with the `region` value that runs a monitor on it |

## Monitor settings added in v1.6

`okstatus_monitor` gained `tags`, `paused`, `muted`, `keyword`, `follow_redirects`, `port`,
`dns_record_type`, `dns_expected_value`, the HTTP request options (`http_method`,
`http_headers`, `http_body`, `http_body_encoding`, `ignore_tls`) and `alert_channel_ids` — the settings the dashboard
and the MCP connector already had. Two rules worth knowing:

- **Left out means untouched.** None of them has a default: an attribute absent from the
  configuration keeps whatever the monitor has. Upgrading from v1.5 therefore plans no change,
  even on monitors tagged, paused or given a keyword in the dashboard. To remove a value, say
  so: `tags = []`, `keyword = ""`, `paused = false`.
- **`alert_channel_ids` is all or nothing.** Set, it is the complete list of the monitor's
  channels. Absent, Terraform does not look at the monitor's channels at all. A monitor that
  joins an `okstatus_monitor_group` receives the group's channels: repeat them on the monitor
  if you manage both.

- **Credentials are write-only.** `http_headers`, `basic_auth_user` / `basic_auth_password`
  and the `auth` block (bearer, API key, cookie, OAuth2, mTLS) are never returned by the API,
  so the state holds what Terraform last sent: an edit made in the dashboard is not seen as
  drift, and they are sent again only when they change. Removing one from the configuration
  removes it from the monitor. Secrets are marked sensitive. Never put `user:password@` in
  `url` (refused at `plan`): the API stores credentials apart from the target.

`type` now covers every check type but `llm` (which keeps its own resource): `grpc`, `push`,
`group`, `postgres`, `mysql`, `mongodb`, `smtp`, `imap`, `redis`, with their settings
(`db_query`, `grace_period` and the computed `push_token`, `child_monitor_ids`), plus
`retries`, `severity_3xx`, `keyword_invert`, the JSON assertion and `dns_server`. Changing
`type` or `grace_period` replaces the monitor.

## Response time alerts (v1.7)

`latency_threshold_ms` turns on the response time alert: the monitor turns `degraded` (never
`down`) after `max(2, retries + 1)` slow checks in a row from one region. `latency_public = false`
keeps that slowness off public status pages, and `latency_anomaly = true` (Max and
Enterprise) alerts when the response time leaves its learned usual range.

Unlike the v1.6 settings, `latency_threshold_ms` belongs to Terraform: left out, the alert is
off, and a threshold set in the dashboard shows up in the next `plan` as a change to remove it.
Copy it into the configuration first. `latency_public` and `latency_anomaly` follow the v1.6
rule: left out, untouched.

`regions` also accepts `private:<agent id>` (one private agent), which
`data.okstatus_private_agents` provides as `region`.

This needs the OKStatus API that ships these fields (October 2026): against an older API the
new attributes are silently ignored by the server and `apply` reports an inconsistent result.

## Provider configuration

```hcl
provider "okstatus" {
  endpoint = "https://api.okstatus.eu/api/v1" # optional, this is the default
  api_key  = "oks_live_..."                   # or set OKSTATUS_API_KEY
}
```

See `examples/main.tf` for a complete, `terraform validate`-checked example covering every
resource and the data source.

## Documentation

`docs/` is generated with [`tfplugindocs`](https://github.com/hashicorp/terraform-plugin-docs)
straight from each resource's Go schema (`MarkdownDescription` fields) plus the per-resource
example `.tf` files under `examples/resources/<name>/resource.tf`,
`examples/data-sources/<name>/data-source.tf` and `examples/provider/provider.tf` — that
per-resource layout is what tfplugindocs actually reads; `examples/main.tf` is a separate,
hand-maintained full example and isn't consumed by the generator. The registry pulls `docs/`
automatically from the repo on each published tag, so this needs to stay in sync by hand:
regenerate after any schema or example change, before tagging a release.

```bash
go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@latest generate
```

Requires Go ≥ 1.25 (same as building the provider — `GOTOOLCHAIN=auto`, the default, fetches
it if your local `go` is older enough to still understand the 3-component `go` directive
itself; a `go` older than 1.21 can't bootstrap that automatically and needs a manual newer
toolchain on `PATH` first).

## Releasing

The repository has GitHub **immutable releases** enabled: once a release is published its
tag and assets are frozen forever, and a tag name can never be reused (even after deleting
the release). Any release created or published **from the GitHub UI** is therefore empty
and dead, and burns its tag — that is how `v1.4.0`, `v1.5.0` and `v1.5.1` were lost.

The only thing to do is push a tag. Nothing else, no UI:

```bash
git tag v1.5.2
git push origin v1.5.2
```

The `Release` workflow (GoReleaser) builds every platform, creates the release as a draft,
attaches the zips, checksums, manifest and signature, then publishes it itself. About five
minutes later the release is live on GitHub and the Terraform Registry picks it up on its
next sync (Settings → Resync on the registry if you are in a hurry).

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
- Enrolling a private agent (it hands out a credential; done in the dashboard) and
  pausing or muting a whole group.
- Alert channel `type`-specific config validation (the provider accepts any string map for
  `config`; `ok-status-backend` validates the actual per-type shape and will reject a bad one
  at apply time with a clear error).
- Acceptance tests (`terraform-plugin-testing`) against a real `ok-status-backend` — the
  resources above were verified against a mock server standing in for the real API; real
  acceptance tests need real credentials and a deployed backend to run against.
