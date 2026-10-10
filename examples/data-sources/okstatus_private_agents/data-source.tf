data "okstatus_private_agents" "all" {}

# Run a monitor on one agent, picked by its name.
locals {
  paris_agent = one([for a in data.okstatus_private_agents.all.agents : a if a.name == "dc-paris"])
}

resource "okstatus_monitor" "intranet" {
  name    = "Intranet"
  url     = "https://intranet.example.com"
  regions = [local.paris_agent.region]
}

output "outdated_agents" {
  value = [
    for a in data.okstatus_private_agents.all.agents : a.name
    if a.version != data.okstatus_private_agents.all.latest_version
  ]
}
