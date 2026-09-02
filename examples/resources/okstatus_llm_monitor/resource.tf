resource "okstatus_llm_monitor" "openai_canary" {
  name         = "OpenAI canary"
  url          = "https://api.openai.com/v1/chat/completions"
  llm_provider = "openai-compatible"
  api_key      = var.openai_api_key
  model        = "gpt-4o-mini"

  prompt         = "Reply with only the word: PONG"
  content_equals = "PONG"
  ttft_ms_max    = 1500

  region   = "fr"
  interval = 300
}
