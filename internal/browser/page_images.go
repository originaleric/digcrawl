package browser

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-rod/rod"
	"github.com/originaleric/digcrawl/runtime/policy"
)

// Only image elements in the leased document are eligible. No credentials or
// inline bytes are returned through browser metadata or the MCP wire.
const pageImageCandidates = `() => {
 const root = document.querySelector('[data-block-type="page"]') || document.querySelector('.docx, [role="main"], main, article') || document.body;
 const seen = new Set();
 return [...root.querySelectorAll('img')].map(img => {
  const url = img.currentSrc || img.src; const r = img.getBoundingClientRect();
  return {url, area: img.naturalWidth * img.naturalHeight, visible: r.width > 0 && r.height > 0, loaded: img.complete && img.naturalWidth > 0};
 }).filter(x => x.visible && x.url && !seen.has(x.url) && seen.add(x.url))
 .sort((a,b) => b.area-a.area).slice(0,256);
}`

// Fetch in the existing document, with its existing session/CORS policy.
// Streams and encoding are bounded before allocation of the complete payload.
const pageImageBytes = `async (url, limit) => {
 const controller = new AbortController(); const timer = setTimeout(() => controller.abort(), 8000);
 try {
  if (![...document.images].some(i => (i.currentSrc || i.src) === url)) return {error:'not_loaded'};
  const crossOrigin = new URL(url, location.href).origin !== location.origin;
  const fetchImage = credentials => fetch(url, {credentials, redirect:'error', signal:controller.signal});
  let r;
  if (crossOrigin) {
   // Public CORS-* CDNs reject credentialed fetches. Do not send cookies first.
   // Session-only resources get at most one fallback, inside the same deadline.
   try { r = await fetchImage('omit'); }
   catch (error) { if(controller.signal.aborted) throw error; }
   if (!r || r.status === 401 || r.status === 403) {
    if (r && r.body) await r.body.cancel();
    r = await fetchImage('include');
   }
  } else { r = await fetchImage('include'); }
  if (!r.ok) return {error: r.status === 401 || r.status === 403 ? 'access_denied' : 'fetch_failed'};
  if (!r.body) return {error:'fetch_failed'};
  if (Number(r.headers.get('content-length') || 0) > limit) { await r.body.cancel(); return {error:'too_large'}; }
  const reader = r.body.getReader(); const chunks = []; let size = 0;
  while (true) {
   const {done, value} = await reader.read(); if(done) break;
   size += value.byteLength; if(size > limit) { await reader.cancel(); return {error:'too_large'}; }
   chunks.push(value);
  }
  const bytes = new Uint8Array(size); let offset = 0;
  for (const part of chunks) {bytes.set(part,offset);offset += part.length;}
  let binary = ''; for(let i=0;i<bytes.length;i+=8192) binary += String.fromCharCode(...bytes.subarray(i,i+8192));
  return {data:btoa(binary)};
 } catch (_) { return {error:'fetch_failed'}; } finally {clearTimeout(timer);}
}`

func capturePageImages(page *rod.Page, maxItems int, maxBytes int64, domains []string) ([]map[string]string, int) {
	result, err := page.Eval(pageImageCandidates)
	if err != nil {
		return []map[string]string{{"source": "page_images", "error": "fetch_failed"}}, 0
	}
	var candidates []struct {
		URL    string `json:"url"`
		Loaded bool   `json:"loaded"`
	}
	if err := json.Unmarshal([]byte(result.Value.JSON("", "")), &candidates); err != nil {
		return []map[string]string{{"source": "page_images", "error": "fetch_failed"}}, 0
	}
	skipped := 0
	if len(candidates) > maxItems {
		skipped = len(candidates) - maxItems
		candidates = candidates[:maxItems]
	}
	images := make([]map[string]string, 0, len(candidates))
	for i, candidate := range candidates {
		input := map[string]string{"source": "page_image"}
		raw := candidate.URL
		if strings.HasPrefix(raw, "blob:") {
			input["source"] = "page_blob_image"
		} else {
			input["url"] = raw
		}
		switch {
		case !candidate.Loaded:
			input["error"] = "not_loaded"
		case strings.HasPrefix(raw, "blob:"):
			// The blob must belong to this document's origin. Never fetch another origin.
			origin, e := page.Eval(`() => location.origin`)
			if e != nil || !strings.HasPrefix(raw, "blob:"+origin.Value.Str()+"/") {
				input["error"] = "access_denied"
			}
		default:
			if _, e := validateURL(raw); e != nil {
				input["error"] = "unsupported_format"
			} else if e := policy.ValidateURLDomain(raw, domains, nil); e != nil {
				input["error"] = "access_denied"
			}
		}
		if input["error"] == "" {
			r, e := page.Eval(pageImageBytes, raw, maxBytes)
			var got map[string]string
			if e != nil || json.Unmarshal([]byte(r.Value.JSON("", "")), &got) != nil {
				input["error"] = "fetch_failed"
				if page.GetContext().Err() != nil {
					input["error"] = "budget_exhausted"
				}
			} else if got["error"] != "" {
				input["error"] = got["error"]
			} else {
				if int64(base64.StdEncoding.DecodedLen(len(got["data"]))) > maxBytes+2 {
					input["error"] = "too_large"
					images = append(images, input)
					continue
				}
				data, decodeErr := base64.StdEncoding.DecodeString(got["data"])
				mime := http.DetectContentType(data)
				if decodeErr != nil || int64(len(data)) > maxBytes {
					input["error"] = "too_large"
				} else if !supportedOCRImage(mime) {
					input["error"] = "unsupported_format"
				} else {
					input["data"] = "data:" + mime + ";base64," + got["data"]
				}
			}
		}
		// Distinguish blob images without exposing ephemeral blob URLs to a provider.
		if input["url"] == "" {
			input["source"] += "_" + string(rune('A'+i))
		}
		images = append(images, input)
	}
	return images, skipped
}

func supportedOCRImage(mime string) bool {
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/bmp":
		return true
	}
	return false
}
