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
| Removed, private, or unavailable content | The source cannot be retrieved by the local reader. | State that no content was retrieved and ask for an accessible source. |

When an error response includes structured output, preserve any available title, canonical URL, or platform diagnostic as context. Do not treat partial metadata as the article body.
