# Error handling

Report DigCrawl errors directly and avoid fabricating content.

| Error code or condition | Explain | Safe next action |
|---|---|---|
| `LOGIN_REQUIRED` | The local session is absent or expired. | For xiaohongshu, douyin, toutiao, threads, or instagram, explain that DigCrawl uses its own local Cookie store and ask whether to open its login window. Only after explicit approval, run `digcrawl browser login --platform <platform>` when the host can keep interactive stdin; after the user confirms login completion, send Enter to the same session. Otherwise show the command for manual use. The short command applies only to the standard user Skill profile; never guess a custom MCP profile path. Retry once after successful exit. |
| `CAPTCHA_REQUIRED` | The platform requires human verification. | Ask the user to finish the verification in the local browser; do not automate it. |
| `RATE_LIMITED` | The platform has temporarily limited requests. | Ask the user to wait and retry later. |
| `CONTENT_TYPE_MISMATCH` | The link does not match the selected reader. | Select the correct tool based on the final URL or ask for a corrected link. |
| `DOMAIN_NOT_ALLOWED` | The target is outside an operator-configured `Gateway.AllowedDomains` restriction. | If the user operates a restricted profile, ask them to approve the domain locally. |
| `BROWSER_*` | A local browser stage failed. Read the sibling `error_details` object: `stage` identifies the failed phase, `reason` is a safe stable classification (for example `timeout` or `page_closed`), and `retryable` states whether one retry is appropriate. Raw browser causes are deliberately not exposed. | Preserve those fields in the explanation. Retry once only when `retryable: true`; otherwise ask for another accessible link, a title, screenshot, or transcript. |
| `ARTIFACT_UNAVAILABLE` | The page content was read, but a requested screenshot could not be read from the local artifact store. | Ask the user to verify `Gateway.ArtifactDir` is writable, then retry. |
| Image attachment unavailable | The requested page image was not returned as an MCP attachment. | State that no image text was read. Ask the user to retry with a screenshot-capable page or provide the image directly. |
| Local ASR is disabled (`Skill.Media.DownloadMedia` or `Skill.ASR.Enabled`) | The user requested transcription, but the one-time local video-ASR opt-in is off. | Explain that enabling it permits local download of this video's media and use of the already configured ASR provider. After explicit consent, call `skill.asr.enable` with `{}`. It first checks ffmpeg and the configured provider; if that check fails it does not update the profile, so preserve the error and direct the user to complete the local dependency setup. Retry the original video request once only after successful enablement. Do not edit YAML, restart MCP, choose a provider, or request a key. |
| Other local ASR configuration error | The user requested transcription but the local media directory, ffmpeg, or configured `Skill.ASR` provider is unavailable. | Preserve the error and ask the user to fix the local dependency; never ask them to paste a key into chat. |
| `video transcript unavailable` | Media download or ASR did not produce `asr_text`. | Explain the local failure and retry only after the user fixes the configured media/ASR dependency. |
| Removed, private, or unavailable content | The source cannot be retrieved by the local reader. | State that no content was retrieved and ask for an accessible source. |

When an error response includes structured output, preserve any available title, canonical URL, or platform diagnostic as context. Do not treat partial metadata as the article body.
