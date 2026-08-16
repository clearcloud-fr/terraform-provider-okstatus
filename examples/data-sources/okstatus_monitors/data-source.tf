data "okstatus_monitors" "all" {}

output "monitor_count" {
  value = data.okstatus_monitors.all.total
}

output "monitor_names" {
  value = [for m in data.okstatus_monitors.all.monitors : m.name]
}
