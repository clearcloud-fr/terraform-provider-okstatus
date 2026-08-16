resource "okstatus_monitor" "db" {
  name = "Primary database"
  url  = "tcp://db.example.com:5432"
  type = "tcp"
}

resource "okstatus_maintenance" "db_upgrade" {
  title           = "Database upgrade"
  message         = "Scheduled PostgreSQL major version upgrade."
  scheduled_start = "2026-09-01T02:00:00Z"
  scheduled_end   = "2026-09-01T04:00:00Z"
  monitor_ids     = [okstatus_monitor.db.id]
}
