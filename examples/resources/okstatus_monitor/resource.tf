resource "okstatus_monitor" "api" {
  name     = "Production API"
  url      = "https://api.example.com/health"
  type     = "http"
  interval = 60
}
