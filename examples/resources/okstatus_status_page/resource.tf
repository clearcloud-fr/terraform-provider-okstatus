resource "okstatus_status_page" "public" {
  slug         = "example"
  title        = "Example Inc. Status"
  description  = "Live status of Example Inc.'s services."
  theme        = "auto"
  accent_color = "#00e676"
  published    = true
}
