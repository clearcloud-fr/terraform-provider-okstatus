resource "okstatus_alert_channel" "on_call" {
  type = "email"
  config = {
    email = "on-call@example.com"
  }
}

resource "okstatus_alert_channel" "team_discord" {
  type = "discord"
  config = {
    webhook_url = "https://discord.com/api/webhooks/..."
  }
}
