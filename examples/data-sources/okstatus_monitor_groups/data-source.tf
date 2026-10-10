data "okstatus_monitor_groups" "all" {}

# Find a group created in the dashboard by its name.
locals {
  payments_group = one([for g in data.okstatus_monitor_groups.all.groups : g if g.name == "Payments"])
}

output "payments_monitor_count" {
  value = length(local.payments_group.monitor_ids)
}
