terraform {
  required_providers {
    okstatus = {
      source = "clearcloud-fr/okstatus"
    }
  }
}

provider "okstatus" {
  # api_key can also come from the OKSTATUS_API_KEY environment variable.
  # api_key = "oks_live_..."
}
