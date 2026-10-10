#!/bin/bash
# 在 CentOS 7 云主机上，进入拉下来的代码目录后执行：
#   sudo bash deploy.sh
# 完成后用浏览器打开 http://这台机器的IP:8080

set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "请用 root 执行：sudo bash deploy.sh"
  exit 1
fi

ROOT="$(cd "$(dirname "$0")" && pwd)"
INSTALL_DIR=/opt/qiutu
GO_VERSION=1.24.0

arch="$(uname -m)"
case "$arch" in
  x86_64) goarch=amd64 ;;
  aarch64) goarch=arm64 ;;
  *)
    echo "这台机器的架构是 ${arch}，脚本只准备了 x86_64 和 aarch64。"
    exit 1
    ;;
esac

go_is_new() {
  command -v go >/dev/null 2>&1 || return 1
  version="$(go version | awk '{print $3}' | sed 's/^go//')"
  major="$(echo "$version" | cut -d. -f1)"
  minor="$(echo "$version" | cut -d. -f2)"
  [ "$major" -gt 1 ] && return 0
  [ "$major" -eq 1 ] && [ "$minor" -ge 24 ]
}

install_go() {
  echo "正在安装 Go ${GO_VERSION}……"
  tmp="$(mktemp)"
  urls="https://mirrors.aliyun.com/golang/go${GO_VERSION}.linux-${goarch}.tar.gz https://mirrors.cloud.tencent.com/go/go${GO_VERSION}.linux-${goarch}.tar.gz https://go.dev/dl/go${GO_VERSION}.linux-${goarch}.tar.gz"
  ok=0
  for url in $urls; do
    if curl -fL --retry 2 --connect-timeout 15 -o "$tmp" "$url"; then
      ok=1
      break
    fi
  done
  if [ "$ok" -ne 1 ]; then
    echo "下载 Go 失败。请检查这台机器能不能访问外网。"
    exit 1
  fi
  rm -rf /usr/local/go
  tar -C /usr/local -xzf "$tmp"
  ln -sfn /usr/local/go/bin/go /usr/local/bin/go
  rm -f "$tmp"
}

if ! go_is_new; then
  install_go
fi

export PATH="/usr/local/go/bin:/usr/local/bin:${PATH}"
export GOPROXY="${GOPROXY:-https://goproxy.cn,https://proxy.golang.org,direct}"
export CGO_ENABLED=0
export GOOS=linux
export GOARCH="$goarch"

mkdir -p "$INSTALL_DIR/web" "$INSTALL_DIR/data"
echo "正在编译……"
(
  cd "$ROOT"
  go build -o "$INSTALL_DIR/qiutu" ./cmd/server
)
cp -a "$ROOT/web/." "$INSTALL_DIR/web/"

cat > /etc/systemd/system/qiutu.service <<'EOF'
[Unit]
Description=康复自重训练方案
After=network.target

[Service]
WorkingDirectory=/opt/qiutu
ExecStart=/opt/qiutu/qiutu -addr :8080 -db /opt/qiutu/data/qiutu.db -web /opt/qiutu/web
Restart=always
RestartSec=2

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable qiutu
systemctl restart qiutu

if systemctl is-active firewalld >/dev/null 2>&1; then
  firewall-cmd --permanent --add-port=8080/tcp
  firewall-cmd --reload
fi

ip="$(hostname -I 2>/dev/null | awk '{print $1}')"
echo
echo "部署完成。"
echo "本机打开： http://127.0.0.1:8080"
if [ -n "${ip}" ]; then
  echo "局域网打开： http://${ip}:8080"
fi
echo "如果是云主机，安全组里也要放行 TCP 8080，再用公网 IP 打开。"
echo "查看状态： systemctl status qiutu"
