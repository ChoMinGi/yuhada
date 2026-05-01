#!/bin/bash
# VPS 초기 설정 스크립트 — 한 번만 실행
set -e

echo "=== Yuhada VPS Setup ==="

# goose 설치 (마이그레이션용)
if ! command -v goose &>/dev/null; then
  echo "Installing goose..."
  GOOSE_URL="https://github.com/pressly/goose/releases/download/v3.24.3/goose_linux_x86_64"
  curl -fsSL "$GOOSE_URL" -o /usr/local/bin/goose
  chmod +x /usr/local/bin/goose
fi

# systemd 서비스 등록
cp /opt/yuhada/deploy/yuhada.service /etc/systemd/system/yuhada.service
systemctl daemon-reload
systemctl enable yuhada

# nginx 설정
cp /opt/yuhada/deploy/nginx-yuhada.conf /etc/nginx/sites-available/yuhada
ln -sf /etc/nginx/sites-available/yuhada /etc/nginx/sites-enabled/yuhada
rm -f /etc/nginx/sites-enabled/default
nginx -t && systemctl reload nginx

# .env 템플릿 생성 (없을 때만)
if [ ! -f /opt/yuhada/.env ]; then
  cat > /opt/yuhada/.env << 'ENVEOF'
APP_ENV=production
APP_ADDR=:8080
DB_PATH=/opt/yuhada/var/yuhada.db
SESSION_SECRET=CHANGE_ME
COOKIE_SECURE=true
ADMIN_BOOTSTRAP_EMAIL=admin@yuhada.kr
ADMIN_BOOTSTRAP_PIN=000000
ENVEOF
  chown yuhada:yuhada /opt/yuhada/.env
  chmod 600 /opt/yuhada/.env
  echo "⚠️  /opt/yuhada/.env 생성됨 — SESSION_SECRET, PIN 수정 필요!"
fi

echo "=== Setup complete ==="
