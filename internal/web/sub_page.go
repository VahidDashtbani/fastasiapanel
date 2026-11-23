package web

import (
	"fmt"
	"strings"
	"time"
)

func formatBytesEN(n int64) string {
	if n < 0 {
		n = 0
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for val := n / unit; val >= unit; val /= unit {
		div *= unit
		exp++
	}
	suffixes := []string{"KB", "MB", "GB", "TB", "PB"}
	if exp >= len(suffixes) {
		exp = len(suffixes) - 1
	}
	return fmt.Sprintf("%.2f %s", float64(n)/float64(div), suffixes[exp])
}

func renderSubStatusPage(subID, title string, up, down, total, expireSec int64, rawContent, subURL string) string {
	used := up + down
	rem := total - used
	if rem < 0 {
		rem = 0
	}
	pct := 0
	if total > 0 {
		pct = int((used * 100) / total)
		if pct > 100 {
			pct = 100
		}
	}

	status := "Active"
	badgeColor := "#6ee7b7"
	badgeBg := "rgba(16,185,129,.16)"
	badgeBorder := "rgba(16,185,129,.35)"
	if total > 0 && used >= total {
		status = "Quota Exhausted"
		badgeColor = "#fda4af"
		badgeBg = "rgba(244,63,94,.16)"
		badgeBorder = "rgba(244,63,94,.35)"
	}

	expStr := "Never"
	if expireSec > 0 {
		expStr = time.Unix(expireSec, 0).UTC().Format("2006-01-02 15:04 UTC")
	}

	totStr := "Unlimited"
	remStr := "Unlimited"
	if total > 0 {
		totStr = formatBytesEN(total)
		remStr = formatBytesEN(rem)
	}

	heading := escHTML(strings.TrimSpace(title))
	if heading == "" {
		heading = "Subscription Details"
	}

	safePayload := strings.ReplaceAll(rawContent, "</script>", "<\/script>")

	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="en" dir="ltr"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>`)
	b.WriteString(heading)
	b.WriteString(`</title><style>`)
	b.WriteString(`:root{--bg:#070b12;--card:#111827;--card2:#0d1320;--line:rgba(148,163,184,.18);--txt:#f1f5f9;--mut:#94a3b8;--accent:#10b981;--sky:#38bdf8}`)
	b.WriteString(`*{box-sizing:border-box}body{margin:0;min-height:100vh;padding:24px 16px 56px;background:radial-gradient(900px 440px at 80% -5%,rgba(56,189,248,.14),transparent 60%),radial-gradient(700px 400px at 15% 0%,rgba(16,185,129,.12),transparent 60%),var(--bg);color:var(--txt);font-family:system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif}`)
	b.WriteString(`.wrap{max-width:760px;margin:0 auto}.card{background:var(--card);border:1px solid var(--line);border-radius:18px;padding:22px;box-shadow:0 20px 48px rgba(0,0,0,.45)}`)
	b.WriteString(`.head{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:16px;flex-wrap:wrap}.head h1{margin:0;font-size:19px;font-weight:700}`)
	fmt.Fprintf(&b, `.badge{padding:6px 12px;border-radius:999px;font-size:12px;font-weight:700;color:%s;background:%s;border:1px solid %s}`, badgeColor, badgeBg, badgeBorder)
	b.WriteString(`.usage-row{display:flex;justify-content:space-between;font-size:13px;color:var(--mut);margin-bottom:8px;gap:8px;flex-wrap:wrap}.usage-row strong{color:var(--txt)}`)
	b.WriteString(`.bar{height:10px;background:rgba(148,163,184,.14);border-radius:999px;overflow:hidden;margin-bottom:18px}`)
	fmt.Fprintf(&b, `.bar>span{display:block;height:100%%;width:%d%%;background:linear-gradient(90deg,var(--sky),var(--accent))}`, pct)
	b.WriteString(`.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(160px,1fr));gap:10px;margin-bottom:16px}`)
	b.WriteString(`.item{background:var(--card2);border:1px solid var(--line);border-radius:12px;padding:12px}.item small{display:block;color:var(--mut);font-size:11px;margin-bottom:4px;text-transform:uppercase;letter-spacing:.04em}.item strong{font-size:14px;word-break:break-word}`)
	b.WriteString(`.urlbox{background:#060911;border:1px solid var(--line);border-radius:12px;padding:11px 13px;font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace;font-size:12px;word-break:break-all;color:#7dd3fc;margin-bottom:14px}`)
	b.WriteString(`.btns{display:flex;gap:8px;flex-wrap:wrap;margin-bottom:16px}button,a.btn{border:1px solid var(--line);border-radius:10px;padding:9px 14px;font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer;text-decoration:none;display:inline-flex;align-items:center;justify-content:center;gap:6px;transition:.15s ease}`)
	b.WriteString(`.primary{background:var(--accent);color:#032216;border-color:transparent;font-weight:700}.sec{background:rgba(148,163,184,.1);color:var(--txt)}.sec:hover{border-color:var(--sky)}`)
	b.WriteString(`.section-title{font-size:14px;font-weight:700;margin:18px 0 10px;display:flex;align-items:center;justify-content:space-between}.cfg-list{display:flex;flex-direction:column;gap:10px}`)
	b.WriteString(`.cfg-item{background:var(--card2);border:1px solid var(--line);border-radius:12px;padding:12px 14px}.cfg-top{display:flex;align-items:center;justify-content:space-between;gap:10px;margin-bottom:8px;flex-wrap:wrap}.cfg-name{font-size:13px;font-weight:700;color:#e2e8f0}`)
	b.WriteString(`.cfg-uri{font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace;font-size:11.5px;color:#cbd5e1;word-break:break-all;background:#060911;border:1px solid var(--line);border-radius:8px;padding:9px 10px}`)
	b.WriteString(`.qr-box{display:none;margin-top:16px;padding:16px;border-radius:14px;background:var(--card2);border:1px solid var(--line);text-align:center}.qr-box.open{display:block}`)
	b.WriteString(`.qr-title{font-size:13px;font-weight:700;margin-bottom:10px;color:#e2e8f0}.qr-canvas-wrap{display:inline-block;padding:12px;background:#ffffff;border-radius:12px}`)
	b.WriteString(`.qr-sub{margin-top:10px;font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace;font-size:11px;color:var(--mut);word-break:break-all}`)
	b.WriteString(`</style></head><body><div class="wrap"><div class="card"><div class="head"><h1>`)
	b.WriteString(heading)
	b.WriteString(`</h1><span class="badge">`)
	b.WriteString(escHTML(status))
	b.WriteString(`</span></div><div class="usage-row"><span>Used: <strong>`)
	b.WriteString(escHTML(formatBytesEN(used)))
	b.WriteString(`</strong></span><span>Total Quota: <strong>`)
	b.WriteString(escHTML(totStr))
	b.WriteString(`</strong></span></div><div class="bar"><span style="width:`)
	fmt.Fprintf(&b, "%d%%", pct)
	b.WriteString(`"></span></div><div class="grid">`)
	fmt.Fprintf(&b, `<div class="item"><small>Remaining</small><strong>%s</strong></div>`, escHTML(remStr))
	fmt.Fprintf(&b, `<div class="item"><small>Expires</small><strong>%s</strong></div>`, escHTML(expStr))
	fmt.Fprintf(&b, `<div class="item"><small>Upload / Download</small><strong>%s / %s</strong></div>`, escHTML(formatBytesEN(up)), escHTML(formatBytesEN(down)))
	fmt.Fprintf(&b, `<div class="item"><small>Subscription ID</small><strong>%s</strong></div>`, escHTML(subID))
	b.WriteString(`</div><div class="urlbox" id="subUrlText">`)
	b.WriteString(escHTML(subURL))
	b.WriteString(`</div><div class="btns">`)
	b.WriteString(`<button type="button" class="primary" id="copySubBtn">Copy Subscription</button>`)
	b.WriteString(`<button type="button" class="sec" id="copyAllBtn">Copy All Configs</button>`)
	b.WriteString(`<button type="button" class="sec" id="qrSubBtn">Subscription QR</button>`)
	b.WriteString(`<a class="btn sec" href="?raw=1">Raw Output</a></div>`)
	b.WriteString(`<div class="qr-box" id="qrBox"><div class="qr-title" id="qrTitle">QR Code</div><div class="qr-canvas-wrap" id="qrVisual"></div><div class="qr-sub" id="qrPayloadText"></div></div>`)
	b.WriteString(`<div class="section-title"><span>Decoded Configs</span><span id="cfgCount" style="color:var(--mut);font-size:12px;">0</span></div><div class="cfg-list" id="cfgList"></div></div></div>`)
	b.WriteString(`<script id="rawSubPayload" type="application/json">`)
	b.WriteString(safePayload)
	b.WriteString(`</script><script>`)
	b.WriteString(`(function(){`)
	b.WriteString(`function decodeRaw(text){var raw=(text||"").trim();if(!raw)return[];var lines=raw.split(/?
/).map(function(s){return s.trim();}).filter(Boolean);var hasUri=lines.some(function(l){return /^(vless|vmess|trojan|ss|ssr|hysteria|hysteria2|tuic|wireguard):\/\//i.test(l);});if(!hasUri&&lines.length===1){try{var norm=lines[0].replace(/-/g,"+").replace(/_/g,"/");while(norm.length%4)norm+="=";lines=atob(norm).split(/?
/).map(function(s){return s.trim();}).filter(Boolean);}catch(_){}}return lines;}`)
	b.WriteString(`function parseConfigLabel(uri,idx){var h=uri.indexOf("#");if(h!==-1&&h<uri.length-1){try{var t=decodeURIComponent(uri.slice(h+1)).trim();if(t)return t;}catch(_){}}var sc=uri.split("://")[0]||"Config";return sc.toUpperCase()+" #"+(idx+1);}`)
	b.WriteString(`function buildOfflineQrSvg(text,size){var bytes=[];try{var enc=new TextEncoder().encode(text);for(var i=0;i<enc.length;i++)bytes.push(enc[i]);}catch(_){for(var j=0;j<text.length;j++)bytes.push(text.charCodeAt(j)&255);}var n=25;if(bytes.length>32)n=29;if(bytes.length>64)n=33;if(bytes.length>110)n=37;if(bytes.length>170)n=41;var grid=Array.from({length:n},function(){return Array(n).fill(0);});var locked=Array.from({length:n},function(){return Array(n).fill(false);});function pf(r0,c0){for(var r=-1;r<=7;r++){for(var c=-1;c<=7;c++){var rr=r0+r,cc=c0+c;if(rr<0||cc<0||rr>=n||cc>=n)continue;var d=(r>=0&&r<=6&&(c===0||c===6))||(c>=0&&c<=6&&(r===0||r===6))||(r>=2&&r<=4&&c>=2&&c<=4);grid[rr][cc]=d?1:0;locked[rr][cc]=true;}}}pf(0,0);pf(0,n-7);pf(n-7,0);for(var k=8;k<n-8;k++){var bit=(k%2===0)?1:0;if!locked[6][k][k]=bit;locked[6][k]=true;}if!locked[k][6][6]=bit;locked[k][6]=true;}}var bits=[bytes.length&255,(bytes.length>>8)&255];for(var bi=0;bi<bytes.length;bi++)bits.push(bytes[bi]);while(bits.length<16)bits.push(236,17);var stream=[];for(var m=0;m<bits.length;m++){for(var sh=7;sh>=0;sh--)stream.push((bits[m]>>sh)&1);}var pos=0;for(var col=n-1;col>0;col-=2){if(col===6)col--;for(var row=0;row<n;row++){for(var d2=0;d2<2;d2++){var c2=col-d2;if(locked[row][c2])continue;var v=stream.length?stream[pos%stream.length]:((row+c2)%2);pos++;if(((row+c2)%2)===0)v^=1;grid[row][c2]=v;}}}var pad=2,cell=size/(n+pad*2),rects=[];for(var r3=0;r3<n;r3++){for(var c3=0;c3<n;c3++){if(grid[r3][c3]){rects.push('<rect x="'+((c3+pad)*cell).toFixed(2)+'" y="'+((r3+pad)*cell).toFixed(2)+'" width="'+cell.toFixed(2)+'" height="'+cell.toFixed(2)+'"/>');}}}return '<svg xmlns="http://www.w3.org/2000/svg" width="'+size+'" height="'+size+'" viewBox="0 0 '+size+' '+size+'" fill="#000000">'+rects.join("")+'</svg>';}`)
	b.WriteString(`function copyText(text,btn,doneLabel){if(!text)return;if(navigator.clipboard&&navigator.clipboard.writeText){navigator.clipboard.writeText(text).catch(function(){});}else{var ta=document.createElement("textarea");ta.value=text;document.body.appendChild(ta);ta.select();try{document.execCommand("copy");}catch(_){}document.body.removeChild(ta);}var orig=btn.textContent;btn.textContent=doneLabel||"Copied!";setTimeout(function(){btn.textContent=orig;},1500);}`)
	b.WriteString(`var rawEl=document.getElementById("rawSubPayload");var configs=decodeRaw(rawEl?rawEl.textContent:"");var cfgListEl=document.getElementById("cfgList");var cfgCountEl=document.getElementById("cfgCount");if(cfgCountEl)cfgCountEl.textContent=String(configs.length);`)
	b.WriteString(`var qrBox=document.getElementById("qrBox"),qrTitle=document.getElementById("qrTitle"),qrVisual=document.getElementById("qrVisual"),qrPayloadText=document.getElementById("qrPayloadText");`)
	b.WriteString(`function showQr(label,payload){if(!payload)return;qrTitle.textContent=label;qrVisual.innerHTML=buildOfflineQrSvg(payload,196);qrPayloadText.textContent=payload;qrBox.classList.add("open");qrBox.scrollIntoView({behavior:"smooth",block:"nearest"});}`)
	b.WriteString(`if(!configs.length){cfgListEl.innerHTML='<div class="cfg-item" style="color:var(--mut)">No decoded configs available.</div>';}else{configs.forEach(function(uri,i){var card=document.createElement("div");card.className="cfg-item";var top=document.createElement("div");top.className="cfg-top";var titleSpan=document.createElement("span");titleSpan.className="cfg-name";titleSpan.textContent=parseConfigLabel(uri,i);var actions=document.createElement("div");actions.style.display="flex";actions.style.gap="6px";var copyBtn=document.createElement("button");copyBtn.type="button";copyBtn.className="sec";copyBtn.textContent="Copy";copyBtn.addEventListener("click",function(){copyText(uri,copyBtn,"Copied!");});var qrBtn=document.createElement("button");qrBtn.type="button";qrBtn.className="sec";qrBtn.textContent="QR";qrBtn.addEventListener("click",function(){showQr(parseConfigLabel(uri,i),uri);});actions.appendChild(copyBtn);actions.appendChild(qrBtn);top.appendChild(titleSpan);top.appendChild(actions);var uriBox=document.createElement("div");uriBox.className="cfg-uri";uriBox.textContent=uri;card.appendChild(top);card.appendChild(uriBox);cfgListEl.appendChild(card);});}`)
	b.WriteString(`document.getElementById("copySubBtn").addEventListener("click",function(){copyText(document.getElementById("subUrlText").textContent.trim(),this,"Copied!");});`)
	b.WriteString(`document.getElementById("copyAllBtn").addEventListener("click",function(){copyText(configs.join("
"),this,"All Copied!");});`)
	b.WriteString(`document.getElementById("qrSubBtn").addEventListener("click",function(){showQr("Subscription QR",document.getElementById("subUrlText").textContent.trim());});`)
	b.WriteString(`})();</script></body></html>`)
	return b.String()
}
