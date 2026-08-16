resource "okstatus_monitor" "api" {
  name = "Production API"
  url  = "https://api.example.com/health"
  type = "http"
}

resource "okstatus_incident" "example" {
  title      = "Elevated API latency"
  severity   = "minor"
  monitor_id = okstatus_monitor.api.id
  status     = "investigating"
  message    = "Investigating a latency spike on the API monitor."
}
