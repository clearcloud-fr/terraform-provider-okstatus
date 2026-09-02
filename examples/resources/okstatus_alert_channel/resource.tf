resource "okstatus_alert_channel" "on_call" {
  type = "email"
  config = {
    email = "on-call@example.com"
  }
}

resource "okstatus_alert_channel" "team_discord" {
  type = "discord"
  config = {
    # Backend field name, not Terraform's usual snake_case — webhookUrl, not
    # webhook_url.
    webhookUrl = "https://discord.com/api/webhooks/..."
  }
}
