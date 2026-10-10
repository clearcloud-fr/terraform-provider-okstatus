resource "okstatus_monitor_group" "payments" {
  name        = "Payments"
  description = "Everything on the checkout path"
  tags        = ["prod"]

  # Attached to every monitor that joins the group.
  alert_channel_ids = [okstatus_alert_channel.team_discord.id]

  monitor_ids = [
    okstatus_monitor.api.id,
    okstatus_monitor.database.id,
  ]
}
