package site

import (
	"fmt"
	"html"
)

const nginxVHostTemplate = `server {
    listen 80;
    {{if .IPv6Enabled}}listen [::]:80;
    {{end}}server_name {{.ServerNames}};
    {{if .SSLEnabled}}
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl;
    {{if .IPv6Enabled}}listen [::]:443 ssl;
    {{end}}server_name {{.ServerNames}};

    ssl_certificate {{.CertFile}};
    ssl_certificate_key {{.KeyFile}};
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;
    {{end}}

    {{if eq .Framework "proxy"}}
    client_max_body_size 260m;

    location / {
        proxy_pass http://127.0.0.1:{{.ProxyPort}};
        proxy_http_version 1.1;
        proxy_buffering off;
        proxy_request_buffering off;
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
    {{else if eq .Framework "static"}}
    root {{.EffectiveRoot}};
    index index.html index.htm;
    charset utf-8;

    location / {
        try_files $uri $uri/ =404;
    }

    location = /favicon.ico { access_log off; log_not_found off; }
    location = /robots.txt  { access_log off; log_not_found off; }

    access_log /var/log/nginx/{{.Domain}}-access.log;
    error_log  /var/log/nginx/{{.Domain}}-error.log error;

    location ~ /\.(?!well-known).* {
        deny all;
    }
    {{else}}
    root {{.EffectiveRoot}};
    index index.php index.html index.htm;
    charset utf-8;

    location / {
        {{if or (eq .Framework "laravel") (eq .Framework "ci4")}}
        try_files $uri $uri/ /index.php?$query_string;
        {{else if eq .Framework "wordpress"}}
        try_files $uri $uri/ /index.php?$args;
        {{else if eq .Framework "ci3"}}
        try_files $uri $uri/ /index.php/$args;
        {{else if eq .Framework "symfony"}}
        try_files $uri /index.php$is_args$args;
        {{else}}
        try_files $uri $uri/ /index.php?$query_string;
        {{end}}
    }

    location = /favicon.ico { access_log off; log_not_found off; }
    location = /robots.txt  { access_log off; log_not_found off; }

    access_log /var/log/nginx/{{.Domain}}-access.log;
    error_log  /var/log/nginx/{{.Domain}}-error.log error;

    location ~ \.php$ {
        include snippets/fastcgi-php.conf;
        fastcgi_pass unix:{{.PHPSocket}};
        fastcgi_param SCRIPT_FILENAME $realpath_root$fastcgi_script_name;
        include fastcgi_params;
    }

    location ~ /\.(?!well-known).* {
        deny all;
    }
    {{end}}
}
`

// renderStaticDefaultHTML menghasilkan halaman index.html bawaan untuk virtual host baru.
func renderStaticDefaultHTML(domain, rootPath string) string {
	escDomain := html.EscapeString(domain)
	escRoot := html.EscapeString(rootPath)
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="id">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>%[1]s — Situs Siap Digunakan</title>
  <style>
    :root {
      color-scheme: light dark;
      --bg: #f8fafc;
      --card: #ffffff;
      --border: #e2e8f0;
      --text: #0f172a;
      --muted: #475569;
      --accent: #2563eb;
      --ok-bg: #dcfce7;
      --ok-fg: #166534;
      --code-bg: #f1f5f9;
    }
    @media (prefers-color-scheme: dark) {
      :root {
        --bg: #0b1120;
        --card: #111827;
        --border: #1f2937;
        --text: #f8fafc;
        --muted: #94a3b8;
        --accent: #60a5fa;
        --ok-bg: rgba(34, 197, 94, 0.16);
        --ok-fg: #86efac;
        --code-bg: #1e293b;
      }
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      display: flex;
      align-items: center;
      justify-content: center;
      padding: 24px;
      background: var(--bg);
      color: var(--text);
      font-family: system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      line-height: 1.6;
    }
    .card {
      width: 100%%;
      max-width: 640px;
      background: var(--card);
      border: 1px solid var(--border);
      border-radius: 16px;
      padding: 32px;
      box-shadow: 0 12px 32px -12px rgba(15, 23, 42, 0.14);
    }
    .badge {
      display: inline-flex;
      align-items: center;
      gap: 8px;
      padding: 4px 12px;
      border-radius: 9999px;
      background: var(--ok-bg);
      color: var(--ok-fg);
      font-size: 13px;
      font-weight: 600;
      margin-bottom: 16px;
    }
    .dot {
      width: 8px;
      height: 8px;
      border-radius: 9999px;
      background: currentColor;
    }
    h1 {
      margin: 0 0 10px;
      font-size: clamp(1.5rem, 3vw, 2rem);
      letter-spacing: -0.02em;
      word-break: break-word;
    }
    .domain { color: var(--accent); }
    p {
      margin: 0 0 20px;
      color: var(--muted);
      font-size: 15px;
    }
    .meta {
      display: grid;
      gap: 10px;
      border: 1px solid var(--border);
      border-radius: 12px;
      padding: 16px;
      margin-bottom: 20px;
    }
    .row {
      display: flex;
      flex-wrap: wrap;
      justify-content: space-between;
      gap: 8px;
      font-size: 14px;
    }
    .label { color: var(--muted); }
    code {
      font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
      font-size: 13px;
      background: var(--code-bg);
      padding: 2px 7px;
      border-radius: 6px;
      word-break: break-all;
    }
    .hint {
      font-size: 13.5px;
      color: var(--muted);
      border-top: 1px solid var(--border);
      padding-top: 16px;
      margin: 0;
    }
  </style>
</head>
<body>
  <main class="card">
    <div class="badge"><span class="dot"></span>Domain &amp; Virtual Host Aktif</div>
    <h1>Selamat Datang di <span class="domain">%[1]s</span></h1>
    <p>Domain PowerDNS dan virtual host Nginx telah aktif dan siap melayani permintaan web Anda.</p>
    <div class="meta">
      <div class="row"><span class="label">Domain</span><code>%[1]s</code></div>
      <div class="row"><span class="label">Document Root</span><code>%[2]s</code></div>
      <div class="row"><span class="label">Web Server</span><code>Nginx · Static HTML</code></div>
    </div>
    <p class="hint">Untuk mengganti halaman default ini, unggah berkas situs Anda atau sunting <code>%[2]s/index.html</code>.</p>
  </main>
</body>
</html>
`, escDomain, escRoot)
}
