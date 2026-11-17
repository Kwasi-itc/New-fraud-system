package providerproduct

import (
	_ "embed"
	"net/http"
	"os"
	"path/filepath"
)

//go:embed openapi.yaml
var specification []byte

func registerDocumentation(mux *http.ServeMux) {
	mux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.Write(specification)
	})
	mux.HandleFunc("GET /docs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'")
		w.Write([]byte(providerDocs))
	})
	mux.HandleFunc("GET /assets/provider-docs.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		w.Write([]byte(`window.onload=()=>SwaggerUIBundle({url:'./openapi.yaml',dom_id:'#swagger-ui',deepLinking:true,persistAuthorization:false,validatorUrl:null});`))
	})
	for _, name := range []string{"swagger-ui.css", "swagger-ui-bundle.js"} {
		mux.HandleFunc("GET /assets/"+name, func(w http.ResponseWriter, r *http.Request) {
			dir := os.Getenv("PROVIDER_SWAGGER_ASSETS")
			if dir == "" {
				dir = "/app/swagger"
			}
			http.ServeFile(w, r, filepath.Join(dir, name))
		})
	}
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "./docs", http.StatusTemporaryRedirect)
	})
}

const providerDocs = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Screening Provider API</title><link rel="stylesheet" href="./assets/swagger-ui.css"></head><body><h1>Screening Provider API</h1><p>Synchronous source matching. Use a provider client credential. Matches are review candidates, not confirmed identities. Dataset access is explicit.</p><div id="swagger-ui"></div><script src="./assets/swagger-ui-bundle.js"></script><script src="./assets/provider-docs.js"></script></body></html>`
