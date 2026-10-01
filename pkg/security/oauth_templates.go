package security

import (
	"bytes"
	"html/template"
	"net/http"
)

// OAuthLoginPage is the data of the login page template.
type OAuthLoginPage struct {
	Title      string
	Error      string
	Action     string // form action
	State      string // opaque, must be posted back as the "req" field
	ClientName string
	LoginHint  string
	// Extra hidden fields to post back (device flow).
	Hidden map[string]string
}

// OAuthScopeInfo is one line of the consent screen.
type OAuthScopeInfo struct {
	Name        string
	Description string
}

// OAuthConsentPage is the data of the consent page template.
type OAuthConsentPage struct {
	Title      string
	Action     string
	State      string // opaque, must be posted back as the "req" field
	ClientName string
	ClientURI  string
	LogoURI    string
	Scopes     []OAuthScopeInfo
	User       string
	Hidden     map[string]string
}

type oauthMessagePage struct {
	Title   string
	Message string
	Error   bool
}

type oauthDevicePage struct {
	Title    string
	Action   string
	Error    string
	UserCode string
}

type oauthLogoutPage struct {
	Title  string
	Action string
	Hidden map[string]string
}

type oauthTemplates struct {
	login, consent *template.Template
	base           *template.Template
}

const oauthPageCSS = `body{font-family:system-ui,sans-serif;display:flex;justify-content:center;align-items:center;min-height:100vh;margin:0;background:#f5f5f5}
.card{background:#fff;padding:2rem;border-radius:8px;box-shadow:0 2px 8px rgba(0,0,0,.15);width:340px;max-width:92vw}
h2{margin:0 0 1.25rem;font-size:1.25rem}p{color:#444;font-size:.9rem}
label{display:block;margin-bottom:.25rem;font-size:.875rem;color:#555}
input[type=text],input[type=password]{width:100%;box-sizing:border-box;padding:.5rem;border:1px solid #ccc;border-radius:4px;margin-bottom:1rem;font-size:1rem}
button{padding:.6rem 1rem;background:#0070f3;color:#fff;border:none;border-radius:4px;font-size:1rem;cursor:pointer}
button.secondary{background:#e5e5e5;color:#222}button:hover{opacity:.9}.full{width:100%}
.err{color:#d32f2f;margin-bottom:1rem;font-size:.875rem}ul{padding-left:1.2rem}li{margin:.35rem 0;font-size:.9rem}
.row{display:flex;gap:.5rem}.row button{flex:1}.logo{max-height:48px;margin-bottom:1rem}.muted{color:#777;font-size:.8rem}`

const oauthPageTemplates = `
{{define "head"}}<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>{{.Title}}</title><style>` + oauthPageCSS + `</style></head><body><div class="card">{{end}}
{{define "foot"}}</div></body></html>{{end}}
{{define "hidden"}}{{range $k, $v := .Hidden}}<input type="hidden" name="{{$k}}" value="{{$v}}">{{end}}{{end}}

{{define "login"}}{{template "head" .}}<h2>{{.Title}}</h2>{{if .ClientName}}<p>to continue to <b>{{.ClientName}}</b></p>{{end}}
{{if .Error}}<div class="err">{{.Error}}</div>{{end}}
<form method="POST" action="{{.Action}}">
<input type="hidden" name="req" value="{{.State}}">{{template "hidden" .}}
<label>Username</label><input type="text" name="username" value="{{.LoginHint}}" autofocus autocomplete="username">
<label>Password</label><input type="password" name="password" autocomplete="current-password">
<button class="full" type="submit">Sign in</button>
</form>{{template "foot"}}{{end}}

{{define "consent"}}{{template "head" .}}{{if .LogoURI}}<img class="logo" src="{{.LogoURI}}" alt="">{{end}}
<h2>{{if .ClientURI}}<a href="{{.ClientURI}}" rel="noopener noreferrer">{{.ClientName}}</a>{{else}}{{.ClientName}}{{end}} wants access</h2>
<p>Signed in as <b>{{.User}}</b>. This application will be able to:</p>
<ul>{{range .Scopes}}<li><b>{{.Name}}</b>{{if .Description}} – {{.Description}}{{end}}</li>{{end}}</ul>
<form method="POST" action="{{.Action}}">
<input type="hidden" name="req" value="{{.State}}">{{template "hidden" .}}
<p class="muted"><label><input type="checkbox" name="remember" value="1" checked> Remember this decision</label></p>
<div class="row"><button class="secondary" type="submit" name="decision" value="deny">Deny</button>
<button type="submit" name="decision" value="allow">Allow</button></div>
</form>{{template "foot"}}{{end}}

{{define "device"}}{{template "head" .}}<h2>{{.Title}}</h2><p>Enter the code shown on your device.</p>
{{if .Error}}<div class="err">{{.Error}}</div>{{end}}
<form method="POST" action="{{.Action}}"><input type="hidden" name="step" value="code">
<input type="text" name="user_code" value="{{.UserCode}}" autofocus autocomplete="off" placeholder="XXXX-XXXX">
<button class="full" type="submit">Continue</button></form>{{template "foot"}}{{end}}

{{define "message"}}{{template "head" .}}<h2>{{.Title}}</h2>{{if .Error}}<div class="err">{{.Message}}</div>{{else}}<p>{{.Message}}</p>{{end}}{{template "foot"}}{{end}}

{{define "logout"}}{{template "head" .}}<h2>{{.Title}}</h2><p>Do you want to sign out?</p>
<form method="POST" action="{{.Action}}">{{template "hidden" .}}
<div class="row"><button class="secondary" type="submit" name="confirm" value="no">Stay signed in</button>
<button type="submit" name="confirm" value="yes">Sign out</button></div></form>{{template "foot"}}{{end}}
`

func newOAuthTemplates(cfg *OAuthServerConfig) oauthTemplates {
	base := template.Must(template.New("oauth").Parse(oauthPageTemplates))
	return oauthTemplates{base: base, login: cfg.LoginTemplate, consent: cfg.ConsentTemplate}
}

func (s *OAuthServer) renderHTML(w http.ResponseWriter, status int, override *template.Template, name string, data any) {
	var buf bytes.Buffer
	var err error
	if override != nil {
		err = override.Execute(&buf, data)
	} else {
		err = s.tmpl.base.ExecuteTemplate(&buf, name, data)
	}
	if err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Pragma", "no-cache")
	h.Set("X-Frame-Options", "DENY")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src https: data:; frame-ancestors 'none'; base-uri 'none'")
	w.WriteHeader(status)
	w.Write(buf.Bytes()) //nolint:errcheck,gosec // G104: best-effort write, G705: html/template output is escaped
}

func (s *OAuthServer) renderMessage(w http.ResponseWriter, status int, title, msg string, isErr bool) {
	s.renderHTML(w, status, nil, "message", oauthMessagePage{Title: title, Message: msg, Error: isErr})
}
