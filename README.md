# Noticeboard Display

A Web application written in Go + HTMX + PocketBase to replicate the functionality of the United Seafarers Mission `GSM_Display` Windows application.

## Dependencies

- [wgo](https://github.com/bokwoon95/wgo) for live reload.

## Models

| OpenRouter Model           | Relative Cost | Rating (0 to 10) |
| -------------------------- | ------------- | ---------------- |
| anthropic/claude-haiku-5.5 | 4x            | 8                |
| z-ai/glm-5.3-flash         | 1x            | -                |
| z-ai/glm-5.3               | 7x            | -                |

Use a Flash model for high-volume, latency-sensitive, or relatively well-defined work: extraction, classification, routing, summarization, short drafting, and routine coding assistance. Use the non-Flash flagship when a task involves difficult multi-step reasoning and software engineering.
