package browser

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/go-rod/rod"
)

func isFeishuDocument(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return (host == "feishu.cn" || strings.HasSuffix(host, ".feishu.cn") || host == "feishu.com" || strings.HasSuffix(host, ".feishu.com")) && (strings.HasPrefix(u.Path, "/wiki/") || strings.HasPrefix(u.Path, "/docx/"))
}

// Feishu virtualizes blocks. Read bounded successive rendered windows and
// restore the original scroll position. No hidden API, login or content gate
// is bypassed; a partial result remains explicitly partial.
const feishuDocumentScript = `async () => {
 const scroller=document.querySelector('.bear-web-x-container');
 if (!scroller || !document.querySelector('[data-block-type="page"]')) return null;
 const initial=scroller.scrollTop;const seen=new Map();const start=performance.now();
 let bytes=0,steps=0,reachedEnd=false,truncated=false;
 try {
  scroller.scrollTop=0;await new Promise(r=>setTimeout(r,180));
  for(;steps<48 && performance.now()-start<12000;steps++) {
   const blocks=[...document.querySelectorAll('[data-block-type="page"] [data-block-id]')];
   for(const e of blocks) {
    if(e.querySelector('[data-block-id]'))continue;
    const id=e.getAttribute('data-record-id')||e.getAttribute('data-block-id');
    const html=e.outerHTML;const text=e.innerText||'';
    const prior=seen.get(id);const nextBytes=bytes-(prior?prior.html.length:0)+html.length;
    if(html.length>65536 || nextBytes>2*1024*1024){truncated=true;continue;}
    seen.set(id,{html,text,order:Number(e.getAttribute('data-block-id'))||seen.size});bytes=nextBytes;
   }
   if(scroller.scrollTop+scroller.clientHeight>=scroller.scrollHeight-4) {reachedEnd=true;break;}
   scroller.scrollTop=Math.min(scroller.scrollTop+Math.max(200,scroller.clientHeight*0.8),scroller.scrollHeight);
   await new Promise(r=>setTimeout(r,180));
  }
 } finally {scroller.scrollTop=initial;await new Promise(r=>setTimeout(r,180));}
 const values=[...seen.values()].sort((a,b)=>a.order-b.order);
 return {html:values.map(x=>x.html).join('\n'),text:values.map(x=>x.text).join('\n'),blocks:values.length,steps:steps+1,reached_end:reachedEnd,scope:'rendered_document',truncated};
}`

func readFeishuDocument(page *rod.Page) (string, string, map[string]any) {
	r, err := page.Eval(feishuDocumentScript)
	if err != nil || r == nil {
		return "", "", map[string]any{"scope": "rendered_document", "reached_end": false, "reason": "operation_unavailable"}
	}
	var result struct {
		Truncated  bool   `json:"truncated"`
		HTML       string `json:"html"`
		Text       string `json:"text"`
		Blocks     int    `json:"blocks"`
		Steps      int    `json:"steps"`
		ReachedEnd bool   `json:"reached_end"`
	}
	if json.Unmarshal([]byte(r.Value.JSON("", "")), &result) != nil || result.Blocks == 0 {
		return "", "", map[string]any{"scope": "rendered_document", "reached_end": false, "reason": "document_not_rendered"}
	}
	return result.HTML, result.Text, map[string]any{"scope": "rendered_document", "reached_end": result.ReachedEnd, "blocks": result.Blocks, "steps": result.Steps, "truncated": result.Truncated}
}
