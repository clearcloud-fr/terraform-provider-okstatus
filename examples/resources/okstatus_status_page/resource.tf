resource "okstatus_status_page" "public" {
  slug         = "example"
  title        = "Example Inc. Status"
  description  = "Live status of Example Inc.'s services."
  theme        = "auto"
  accent_color = "#00e599"
  published    = true

  # Monitors shown on the page, by section, in display order.
  # Leave `sections` out to arrange them in the dashboard instead;
  # `sections = []` empties the page.
  sections = [
    { name = "Website", monitor_ids = [okstatus_monitor.website.id] },
    { name = "API", monitor_ids = [okstatus_monitor.api.id, okstatus_monitor.db.id] },
  ]
}
