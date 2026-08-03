# Error handling

Report DigCrawl errors directly and avoid fabricating content.

| Error code or condition | Explain | Safe next action |
|---|---|---|
| `LOGIN_REQUIRED` | The local session is absent or expired. | Ask the user to complete the platform login locally, then retry. |
| `CAPTCHA_REQUIRED` | The platform requires human verification. | Ask the user to finish the verification in the local browser; do not automate it. |
| `RATE_LIMITED` | The platform has temporarily limited requests. | Ask the user to wait and retry later. |
| `CONTENT_TYPE_MISMATCH` | The link does not match the selected reader. | Select the correct tool based on the final URL or ask for a corrected link. |
| `DOMAIN_NOT_ALLOWED` | The target is outside an operator-configured `Gateway.AllowedDomains` restriction. | If the user operates a restricted profile, ask them to approve the domain locally. |
| `ARTIFACT_UNAVAILABLE` | The page content was read, but a requested screenshot could not be read from the local artifact store. | Ask the user to verify `Gateway.ArtifactDir` is writable, then retry. |
| Skill OCR/ASR/Summary configuration error | The user requested enhancement but the local `Skill` provider, media directory, or API key is not configured. | Ask the user to update their copied local profile; never ask them to paste a key into chat. |
| `video transcript unavailable` | Media download or ASR did not produce `asr_text`. | Explain the local failure and retry only after the user fixes the configured media/ASR dependency. |
| Local video-summary error | The optional local OpenAI-compatible `Skill.Summary` fallback is disabled, lacks a key, or returned invalid content. | By default, summarize the returned Markdown and `asr_text` in the host Agent. Use the local fallback only after the user enables its BYOK configuration. |
| Removed, private, or unavailable content | The source cannot be retrieved by the local reader. | State that no content was retrieved and ask for an accessible source. |

When an error response includes structured output, preserve any available title, canonical URL, or platform diagnostic as context. Do not treat partial metadata as the article body.
