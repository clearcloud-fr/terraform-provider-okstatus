resource "okstatus_monitor" "api" {
  name                  = "Production API"
  url                   = "https://api.example.com/health"
  type                  = "http"
  interval              = 60
  regions               = ["fr", "de", "nl"]
  accepted_status_codes = ["200-299"]
}
