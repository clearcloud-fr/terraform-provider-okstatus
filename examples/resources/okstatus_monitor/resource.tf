resource "okstatus_monitor" "api" {
  name                  = "Production API"
  url                   = "https://api.example.com/health"
  type                  = "http"
  interval              = 60
  regions               = ["fr-dc3", "de-fra", "nl-ams"]
  accepted_status_codes = ["200-299"]

  tags              = ["prod", "payments"]
  keyword           = "\"status\":\"ok\""
  follow_redirects  = false
  alert_channel_ids = [okstatus_alert_channel.team_discord.id]
}

# A POST with a body and a token header (the header is never shown in a plan).
resource "okstatus_monitor" "webhook" {
  name               = "Order webhook"
  url                = "https://api.example.com/orders/ping"
  http_method        = "POST"
  http_body          = jsonencode({ ping = true })
  http_body_encoding = "json"
  http_headers       = { Authorization = "Bearer ${var.probe_token}" }
}

# An API behind OAuth2 client credentials, judged on its JSON answer.
resource "okstatus_monitor" "partner_api" {
  name = "Partner API"
  url  = "https://partner.example.com/health"

  auth = {
    type          = "oauth2"
    token_url     = "https://login.example.com/oauth/token"
    client_id     = "okstatus-probe"
    client_secret = var.partner_client_secret
  }

  json_path           = "$.status"
  json_operator       = "=="
  json_expected_value = "ok"
}

# A PostgreSQL check with a read-only account (never put it in the URL).
resource "okstatus_monitor" "orders_db" {
  name                = "Orders database"
  url                 = "postgres://db.example.com:5432/orders"
  type                = "postgres"
  db_query            = "SELECT 1"
  basic_auth_user     = "okstatus_ro"
  basic_auth_password = var.orders_db_password
}

# A heartbeat: the nightly job calls the ping URL when it succeeds.
resource "okstatus_monitor" "backup" {
  name         = "Nightly backup"
  url          = "nightly-backup"
  type         = "push"
  interval     = 86400
  grace_period = 3600
}

output "backup_push_token" {
  value     = okstatus_monitor.backup.push_token
  sensitive = true
}

# A DNS check, paused until the record is published.
resource "okstatus_monitor" "spf" {
  name               = "SPF record"
  url                = "example.com"
  type               = "dns"
  dns_record_type    = "TXT"
  dns_expected_value = "v=spf1"
  paused             = true
}

# A TCP check run by one of your private agents.
data "okstatus_private_agents" "all" {}

resource "okstatus_monitor" "database" {
  name    = "Internal database"
  url     = "db.internal.example.com"
  type    = "tcp"
  port    = 5432
  regions = [data.okstatus_private_agents.all.agents[0].region]
}
