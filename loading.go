package main

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httputil"
	"strings"
)

const generationHeader = "X-Anything-Generate"

// A completed loading document can replace itself while reading a fetch stream.
// Replay the original request so a form POST isn't turned into a GET or repeated.
func loadingPage(w http.ResponseWriter, r *http.Request) {
	snapshot := r.Clone(r.Context())
	// Let fetch send credentials; never put them in an inline script.
	snapshot.Header.Del("Cookie")
	snapshot.Header.Del("Authorization")
	raw, err := httputil.DumpRequest(snapshot, true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	encoded, _ := json.Marshal(raw) // []byte becomes a safe base64 string.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	io.WriteString(w, strings.Replace(loadingHTML, "REQUEST_DATA", string(encoded), 1))
}

func originalRequest(w http.ResponseWriter, r *http.Request) (*http.Request, error) {
	// The envelope includes headers as well as the separately bounded form body.
	original, err := http.ReadRequest(bufio.NewReader(http.MaxBytesReader(w, r.Body, 2<<20)))
	if err != nil {
		return nil, err
	}
	for _, key := range []string{"Cookie", "Authorization"} {
		original.Header.Del(key)
		if values := r.Header.Values(key); len(values) > 0 {
			original.Header[key] = append([]string(nil), values...)
		}
	}
	original.RemoteAddr = r.RemoteAddr
	return original.WithContext(r.Context()), nil
}

const loadingHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Generating page…</title>
<style>
body{margin:0;min-height:100vh;display:grid;place-items:center;background:#f8f8f8;color:#333;font:17px system-ui,sans-serif}
main{text-align:center;padding:2rem;max-width:40rem}p{color:#666;font-size:14px;overflow-wrap:anywhere}
.spinner{width:26px;height:26px;margin:0 auto 20px;border:3px solid #ddd;border-top-color:#555;border-radius:50%;animation:spin 1s linear infinite}
@keyframes spin{to{transform:rotate(360deg)}}
@media(prefers-reduced-motion:reduce){.spinner{animation:none}}
</style></head><body><main>
<div class="spinner" aria-hidden="true"></div>
<div id="message" role="status">Generating page…</div>
<p id="detail">This may take a moment.</p>
<noscript>This loading screen requires JavaScript.</noscript>
</main><script>
addEventListener("load", () => setTimeout(async () => {
  const message = document.getElementById("message"), detail = document.getElementById("detail");
  const began = Date.now();
  let received = 0, writing = false, pending = "", inspected = 0;
  const timer = setInterval(() => {
    detail.textContent = Math.floor((Date.now()-began)/1000)+"s elapsed"+(received ? " · HTML is arriving" : "");
  }, 1000);
  function append(text, finished = false) {
    if (writing) { document.write(text); return; }
    pending += text;
    if (!finished && Date.now()-inspected < 200) return;
    inspected = Date.now();
    // Keep the loader visible through long CSS/head output. Omitted body tags
    // are valid too: parse an inert copy to detect body markup.
    const body = new DOMParser().parseFromString(pending, "text/html").body;
    const ready = Array.from(body.childNodes).some(n =>
      n.nodeType === 3 ? n.textContent.trim() : n.nodeType === 1 && !["SCRIPT","STYLE","TEMPLATE"].includes(n.nodeName));
    if (ready || finished) {
      clearInterval(timer);
      writing = true;
      document.open();
      document.write(pending);
      pending = "";
    }
  }
  try {
    const bytes = Uint8Array.from(atob(REQUEST_DATA), c => c.charCodeAt(0));
    const response = await fetch(location.href, {
      method:"POST", credentials:"same-origin",
      headers:{"X-Anything-Generate":"1", "Content-Type":"message/http"}, body:bytes
    });
    if (!response.ok) throw new Error(await response.text());
    const reader = response.body.getReader(), decoder = new TextDecoder();
    let records = "", complete = false;
    function consume(text) {
      records += text;
      let end;
      while ((end = records.indexOf("\n")) >= 0) {
        const event = JSON.parse(records.slice(0, end));
        records = records.slice(end+1);
        if (event.error) throw new Error(event.error);
        if (event.html !== undefined) append(event.html);
        if (event.done) complete = true;
      }
    }
    for (;;) {
      const {value, done} = await reader.read();
      if (done) break;
      received += value.length;
      message.textContent = "Building page…";
      consume(decoder.decode(value, {stream:true}));
    }
    consume(decoder.decode());
    if (!complete || records.trim()) throw new Error("The HTML stream ended unexpectedly.");
    append("", true);
    document.close();
  } catch (error) {
    if (writing) {
      document.close();
      const alert = document.createElement("p");
      alert.setAttribute("role", "alert");
      alert.textContent = "Page generation stopped: "+error.message;
      alert.style.cssText = "position:fixed;bottom:0;left:0;right:0;z-index:2147483647;margin:0;padding:1rem;background:white;color:#333;font:14px system-ui";
      document.body.append(alert);
    }
    else {
      document.querySelector(".spinner").remove();
      message.textContent = "The page could not be generated.";
      detail.textContent = error.message;
    }
  } finally { clearInterval(timer); }
}, 0));
</script></body></html>`
