#!/usr/bin/env bash
# HMEA Panel system setup for Ubuntu 22.04/24.04 and Debian 12.
# Usage: sudo scripts/install.sh [--no-mail] [--no-dns] [--no-ftp] [--no-apps] [--no-firewall] [--php-repo]
#   --no-apps    skip Node.js and Python (only PHP, static and proxy sites)
#   --php-repo   add the Ondrej Sury PHP repository so several PHP versions
#                and more extensions can be installed side by side
set -euo pipefail

MAIL=1 DNS=1 FTP=1 APPS=1 FIREWALL=1 PHP_REPO=0
for arg in "$@"; do
  case "$arg" in
    --no-mail) MAIL=0 ;;
    --no-dns) DNS=0 ;;
    --no-ftp) FTP=0 ;;
    --no-apps) APPS=0 ;;
    --no-firewall) FIREWALL=0 ;;
    --php-repo) PHP_REPO=1 ;;
    *) echo "unknown option: $arg" >&2; exit 2 ;;
  esac
done

[ "$(id -u)" -eq 0 ] || { echo "run as root" >&2; exit 1; }
. /etc/os-release
case "$ID" in ubuntu|debian) ;; *) echo "only Ubuntu and Debian are supported" >&2; exit 1 ;; esac

HERE="$(cd "$(dirname "$0")/.." && pwd)"
[ -x "$HERE/bin/hmea-panel" ] || { echo "Not built yet. Run: bash $HERE/install.sh" >&2; exit 1; }

export DEBIAN_FRONTEND=noninteractive
RAM_MB=$(awk '/MemTotal/ {print int($2/1024)}' /proc/meminfo)
say() { printf '\n==> %s\n' "$*"; }

# --- Swap: small servers run out of memory during apt and MariaDB start-up.
if [ "$RAM_MB" -lt 2000 ] && [ "$(swapon --noheadings | wc -l)" -eq 0 ] && [ ! -e /swapfile ]; then
  say "Low memory (${RAM_MB} MB): creating a 1 GB swap file"
  fallocate -l 1G /swapfile && chmod 600 /swapfile && mkswap /swapfile >/dev/null && swapon /swapfile
  echo '/swapfile none swap sw 0 0' >> /etc/fstab
  echo 'vm.swappiness=10' > /etc/sysctl.d/99-hmea-panel.conf && sysctl -q -p /etc/sysctl.d/99-hmea-panel.conf
fi

say "Installing packages"
apt-get update -q
if [ "$PHP_REPO" -eq 1 ]; then
  if [ "$ID" = ubuntu ]; then
    apt-get install -y -q software-properties-common
    add-apt-repository -y ppa:ondrej/php
  else
    apt-get install -y -q ca-certificates curl lsb-release
    curl -fsSLo /usr/share/keyrings/deb.sury.org-php.gpg https://packages.sury.org/php/apt.gpg
    echo "deb [signed-by=/usr/share/keyrings/deb.sury.org-php.gpg] https://packages.sury.org/php/ $(lsb_release -sc) main" > /etc/apt/sources.list.d/php.list
  fi
  apt-get update -q
fi
if [ "$APPS" -eq 1 ] && ! command -v node >/dev/null; then
  # Distribution Node.js is often years old; NodeSource carries the current LTS.
  apt-get install -y -q ca-certificates curl gnupg
  curl -fsSL https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key | gpg --dearmor --yes -o /usr/share/keyrings/nodesource.gpg
  echo "deb [signed-by=/usr/share/keyrings/nodesource.gpg] https://deb.nodesource.com/node_22.x nodistro main" > /etc/apt/sources.list.d/nodesource.list
  apt-get update -q
fi
PKGS="curl tar gzip cron nginx mariadb-server certbot ssl-cert openssl php-fpm php-cli php-mysql php-curl php-gd php-mbstring php-xml php-zip php-intl php-opcache"
[ "$APPS" -eq 1 ] && PKGS="$PKGS nodejs python3 python3-venv python3-pip"
[ "$FIREWALL" -eq 1 ] && PKGS="$PKGS ufw fail2ban"
[ "$MAIL" -eq 1 ] && PKGS="$PKGS postfix dovecot-core dovecot-imapd dovecot-lmtpd"
[ "$FTP" -eq 1 ] && PKGS="$PKGS pure-ftpd"
[ "$DNS" -eq 1 ] && PKGS="$PKGS bind9 bind9-utils"
if [ "$MAIL" -eq 1 ]; then
  echo "postfix postfix/main_mailer_type select Internet Site" | debconf-set-selections
  echo "postfix postfix/mailname string $(hostname -f 2>/dev/null || hostname)" | debconf-set-selections
fi
# shellcheck disable=SC2086
apt-get install -y -q --no-install-recommends $PKGS

# --- Nginx
say "Configuring Nginx"
mkdir -p /etc/nginx/hmea-panel/sites /var/www/_letsencrypt
cat > /etc/nginx/conf.d/hmea-panel.conf <<'CONF'
server_tokens off;
include /etc/nginx/hmea-panel/sites/*.conf;
CONF
# Requests for names no site claims get no answer instead of the first site.
cat > /etc/nginx/sites-available/default <<'CONF'
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name _;
    location ^~ /.well-known/acme-challenge/ { root /var/www/_letsencrypt; }
    location / { return 444; }
}
CONF
chown www-data:www-data /var/www
nginx -t && systemctl reload nginx
mkdir -p /etc/letsencrypt/renewal-hooks/deploy
printf '#!/bin/sh\nsystemctl reload nginx\n' > /etc/letsencrypt/renewal-hooks/deploy/hmea-panel-nginx.sh
chmod +x /etc/letsencrypt/renewal-hooks/deploy/hmea-panel-nginx.sh

# --- PHP-FPM: the stock pool also starts workers only when needed.
for pool in /etc/php/*/fpm/pool.d/www.conf; do
  [ -e "$pool" ] || continue
  sed -i -E 's/^pm = .*/pm = ondemand/; s/^;?pm.process_idle_timeout = .*/pm.process_idle_timeout = 20s/' "$pool"
  ver=$(echo "$pool" | cut -d/ -f4)
  systemctl restart "php${ver}-fpm"
done

# --- MariaDB: local only, sized for the machine.
say "Configuring MariaDB"
if [ "$RAM_MB" -lt 1500 ]; then BUFFER=64M; elif [ "$RAM_MB" -lt 4000 ]; then BUFFER=256M; else BUFFER=1G; fi
cat > /etc/mysql/mariadb.conf.d/99-hmea-panel.cnf <<CONF
[mysqld]
bind-address = 127.0.0.1
skip-name-resolve
performance_schema = OFF
innodb_buffer_pool_size = $BUFFER
innodb_log_buffer_size = 8M
key_buffer_size = 8M
max_connections = 50
table_open_cache = 400
CONF
systemctl restart mariadb

# --- Mail: Postfix receives and sends, Dovecot stores (LMTP) and authenticates.
if [ "$MAIL" -eq 1 ]; then
  say "Configuring Postfix and Dovecot"
  getent group vmail >/dev/null || groupadd -g 5000 vmail
  id vmail >/dev/null 2>&1 || useradd -u 5000 -g vmail -d /var/mail/vhosts -s /usr/sbin/nologin vmail
  mkdir -p /var/mail/vhosts /etc/postfix/hmea-panel
  chown vmail:vmail /var/mail/vhosts && chmod 770 /var/mail/vhosts
  for f in vdomains vmailbox valias; do touch "/etc/postfix/hmea-panel/$f"; postmap "/etc/postfix/hmea-panel/$f"; done
  touch /etc/dovecot/hmea-panel-users && chown root:dovecot /etc/dovecot/hmea-panel-users && chmod 640 /etc/dovecot/hmea-panel-users

  postconf -e \
    "virtual_mailbox_domains = hash:/etc/postfix/hmea-panel/vdomains" \
    "virtual_mailbox_maps = hash:/etc/postfix/hmea-panel/vmailbox" \
    "virtual_alias_maps = hash:/etc/postfix/hmea-panel/valias" \
    "virtual_transport = lmtp:unix:private/dovecot-lmtp" \
    "smtpd_sasl_type = dovecot" \
    "smtpd_sasl_path = private/auth" \
    "smtpd_sasl_auth_enable = yes" \
    "smtpd_tls_security_level = may" \
    "smtpd_tls_auth_only = yes" \
    "smtp_tls_security_level = may" \
    "smtpd_helo_required = yes" \
    "disable_vrfy_command = yes" \
    "smtpd_recipient_restrictions = permit_mynetworks, permit_sasl_authenticated, reject_unauth_destination" \
    "message_size_limit = 26214400"
  postconf -M submission/inet="submission inet n - y - - smtpd"
  postconf -P "submission/inet/syslog_name=postfix/submission" \
    "submission/inet/smtpd_tls_security_level=encrypt" \
    "submission/inet/smtpd_sasl_auth_enable=yes" \
    "submission/inet/smtpd_recipient_restrictions=permit_sasl_authenticated,reject"

  sed -i 's/^!include auth-system.conf.ext/#!include auth-system.conf.ext/' /etc/dovecot/conf.d/10-auth.conf
  cat > /etc/dovecot/conf.d/99-hmea-panel.conf <<'CONF'
protocols = imap lmtp
ssl = required
disable_plaintext_auth = yes
auth_mechanisms = plain login
first_valid_uid = 5000
mail_location = maildir:~/Maildir
mail_plugins = $mail_plugins quota
protocol imap {
  mail_plugins = $mail_plugins imap_quota
}
plugin {
  quota = maildir:User quota
}
passdb {
  driver = passwd-file
  args = scheme=BLF-CRYPT username_format=%u /etc/dovecot/hmea-panel-users
}
userdb {
  driver = passwd-file
  args = username_format=%u /etc/dovecot/hmea-panel-users
}
service auth {
  unix_listener /var/spool/postfix/private/auth {
    mode = 0660
    user = postfix
    group = postfix
  }
}
service lmtp {
  unix_listener /var/spool/postfix/private/dovecot-lmtp {
    mode = 0600
    user = postfix
    group = postfix
  }
}
CONF
  systemctl restart dovecot postfix
fi

# --- FTP: virtual users only, chrooted, TLS required.
if [ "$FTP" -eq 1 ]; then
  say "Configuring Pure-FTPd"
  C=/etc/pure-ftpd/conf
  echo yes > $C/ChrootEveryone
  echo yes > $C/NoAnonymous
  echo no > $C/PAMAuthentication
  echo no > $C/UnixAuthentication
  echo /etc/pure-ftpd/pureftpd.pdb > $C/PureDB
  echo 2 > $C/TLS
  echo "40000 40100" > $C/PassivePortRange
  id -u www-data > $C/MinUID
  ln -sf ../conf/PureDB /etc/pure-ftpd/auth/50pure
  if [ ! -e /etc/ssl/private/pure-ftpd.pem ]; then
    openssl req -x509 -nodes -newkey rsa:2048 -days 1825 -subj "/CN=$(hostname)" \
      -keyout /etc/ssl/private/pure-ftpd.pem -out /etc/ssl/private/pure-ftpd.pem 2>/dev/null
    chmod 600 /etc/ssl/private/pure-ftpd.pem
  fi
  touch /etc/pure-ftpd/pureftpd.passwd && chmod 600 /etc/pure-ftpd/pureftpd.passwd
  pure-pw mkdb /etc/pure-ftpd/pureftpd.pdb -f /etc/pure-ftpd/pureftpd.passwd
  systemctl restart pure-ftpd
fi

# --- DNS: authoritative only, never an open resolver.
if [ "$DNS" -eq 1 ]; then
  say "Configuring BIND"
  mkdir -p /etc/bind/hmea-panel
  [ -e /etc/bind/hmea-panel/zones.conf ] || echo "// Managed by hmea-panel." > /etc/bind/hmea-panel/zones.conf
  grep -q hmea-panel/zones.conf /etc/bind/named.conf.local || echo 'include "/etc/bind/hmea-panel/zones.conf";' >> /etc/bind/named.conf.local
  grep -q 'recursion no' /etc/bind/named.conf.options || sed -i '0,/options {/s//options {\n\trecursion no;\n\tallow-transfer { none; };\n\tversion "none";/' /etc/bind/named.conf.options
  named-checkconf && systemctl restart named
fi

# --- Firewall and brute-force protection.
if [ "$FIREWALL" -eq 1 ]; then
  say "Configuring firewall and fail2ban"
  SSH_PORT=$(sshd -T 2>/dev/null | awk '/^port / {print $2; exit}')
  ufw allow "${SSH_PORT:-22}/tcp" >/dev/null
  ufw allow 80/tcp >/dev/null && ufw allow 443/tcp >/dev/null && ufw allow 8443/tcp >/dev/null
  [ "$MAIL" -eq 1 ] && for p in 25 587 993; do ufw allow $p/tcp >/dev/null; done
  [ "$FTP" -eq 1 ] && { ufw allow 21/tcp >/dev/null; ufw allow 40000:40100/tcp >/dev/null; }
  [ "$DNS" -eq 1 ] && { ufw allow 53/tcp >/dev/null; ufw allow 53/udp >/dev/null; }
  ufw --force enable >/dev/null
  {
    printf '[DEFAULT]\nbantime = 1h\nfindtime = 10m\nmaxretry = 5\n\n[sshd]\nenabled = true\n'
    [ "$MAIL" -eq 1 ] && printf '\n[postfix]\nenabled = true\n\n[dovecot]\nenabled = true\n'
    [ "$FTP" -eq 1 ] && printf '\n[pure-ftpd]\nenabled = true\n'
  } > /etc/fail2ban/jail.d/hmea-panel.conf
  systemctl restart fail2ban || echo "warning: fail2ban did not start; check 'journalctl -u fail2ban'"
fi

# --- The panel itself.
say "Installing HMEA Panel"
install -m 755 "$HERE/bin/hmea-panel" /usr/local/bin/hmea-panel
install -m 644 "$HERE/scripts/hmea-panel.service" /etc/systemd/system/hmea-panel.service
mkdir -p /var/lib/hmea-panel /var/backups/hmea-panel && chmod 700 /var/lib/hmea-panel /var/backups/hmea-panel
systemctl daemon-reload
systemctl enable hmea-panel
systemctl restart hmea-panel
sleep 2

IP=$(hostname -I | awk '{print $1}')
echo
echo "HMEA Panel is running at https://${IP}:8443 (self-signed certificate: your browser will warn once)."
if [ -e /var/lib/hmea-panel/initial-password.txt ]; then
  echo "Initial admin password: $(cat /var/lib/hmea-panel/initial-password.txt)"
  echo "Change it under Settings, then delete /var/lib/hmea-panel/initial-password.txt."
fi
