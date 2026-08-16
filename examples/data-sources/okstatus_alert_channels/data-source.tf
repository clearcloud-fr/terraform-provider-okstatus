data "okstatus_alert_channels" "all" {}

output "alert_channel_count" {
  value = data.okstatus_alert_channels.all.total
}

output "has_discord_channel" {
  value = contains([for c in data.okstatus_alert_channels.all.channels : c.type], "discord")
}
