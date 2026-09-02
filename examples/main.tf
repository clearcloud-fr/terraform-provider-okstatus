terraform {
  required_providers {
    okstatus = {
      source = "clearcloud-fr/okstatus"
    }
  }
}

provider "okstatus" {
  # api_key can also come from the OKSTATUS_API_KEY environment variable.
  # api_key = "oks_live_..."
}

resource "okstatus_monitor" "api" {
  name                  = "Production API"
  url                   = "https://api.example.com/health"
  type                  = "http"
  interval              = 60
  regions               = ["fr", "de", "nl"]
  accepted_status_codes = ["200-299"]
}

resource "okstatus_monitor" "website" {
  name     = "Marketing site"
  url      = "https://example.com"
  type     = "http"
  interval = 300
}

resource "okstatus_alert_channel" "on_call" {
  type = "email"
  config = {
    email = "on-call@example.com"
  }
}

resource "okstatus_status_page" "public" {
  slug         = "example"
  title        = "Example Inc. Status"
  description  = "Live status of Example Inc.'s services."
  theme        = "auto"
  accent_color = "#00e676"
  published    = true
}

resource "okstatus_maintenance" "db_upgrade" {
  title           = "Database upgrade"
  message         = "Scheduled PostgreSQL major version upgrade."
  scheduled_start = "2026-09-01T02:00:00Z"
  scheduled_end   = "2026-09-01T04:00:00Z"
  monitor_ids     = [okstatus_monitor.api.id]
}

resource "okstatus_incident" "example" {
  title      = "Elevated API latency"
  severity   = "minor"
  monitor_id = okstatus_monitor.api.id
  status     = "investigating"
  message    = "Investigating a latency spike on the API monitor."
}

data "okstatus_org" "current" {}

output "org_plan" {
  value = data.okstatus_org.current.plan
}

data "okstatus_monitors" "all" {}

output "monitor_count" {
  value = data.okstatus_monitors.all.total
}

data "okstatus_alert_channels" "all" {}

output "alert_channel_count" {
  value = data.okstatus_alert_channels.all.total
}
