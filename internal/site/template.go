package site

const nginxVHostTemplate = `server {
    listen 80;
    listen [::]:80;
    server_name {{.Domain}};
    {{if .SSLEnabled}}
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl;
    listen [::]:443 ssl;
    server_name {{.Domain}};

    ssl_certificate {{.CertFile}};
    ssl_certificate_key {{.KeyFile}};
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;
    {{end}}

    {{if eq .Framework "proxy"}}
    location / {
        proxy_pass http://127.0.0.1:{{.ProxyPort}};
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
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
