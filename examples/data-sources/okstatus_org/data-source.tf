data "okstatus_org" "current" {}

output "org_plan" {
  value = data.okstatus_org.current.plan
}
