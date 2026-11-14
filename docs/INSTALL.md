# Installing hostdeck on a VPS, step by step

This guide takes a new VPS from first login to a working panel with a
website, HTTPS, a database, email and DNS.

> **Status.** The panel and installer have been tested in simulation only,
> not yet on a live server. Do the first install on a fresh VPS you can
> rebuild, and expect to adjust a step or two. If something fails, the
> [Troubleshooting](#14-troubleshooting) section shows where to look.

## 1. What you need

| Item | Requirement |
| --- | --- |
| Server | A **fresh** VPS. The installer reconfigures Nginx, MariaDB, mail, FTP, DNS and the firewall, so do not use a server that already hosts something. |
| Operating system | Ubuntu 22.04 or 24.04, or Debian 12 (64-bit, x86 or ARM). |
| Memory | 1 GB or more for everything. 512 MB works if you skip mail (`--no-mail`). |
| Disk | 10 GB or more. |
| Access | Root, or a user with `sudo`. |
| Domain | A domain name you control, to host a site and to reach the panel. |
| For email | A provider that allows outgoing port 25 (many block it by default; ask support) and lets you set reverse DNS. |

Throughout this guide replace `203.0.113.10` with your server's IP address
and `example.com` with your domain.

## 2. Connect and update the server

From your computer:

```bash
ssh root@203.0.113.10
```

On the server, bring it up to date and reboot if a new kernel was installed:

```bash
apt update && apt -y upgrade
```

Give the server a full host name (used by mail and certificates):

```bash
hostnamectl set-hostname server.example.com
```

## 3. Add swap on small servers

Building the panel needs more memory than a 1 GB server has free. If
`free -m` shows no swap, add 2 GB first:

```bash
fallocate -l 2G /swapfile && chmod 600 /swapfile && mkswap /swapfile && swapon /swapfile && echo '/swapfile none swap sw 0 0' >> /etc/fstab
```

## 4. Install the build tools

Git and Make:

```bash
apt -y install git make curl ca-certificates gnupg
```

Go (the current release, from go.dev; this picks the right file for x86 or ARM):

```bash
GO_VERSION=$(curl -fsSL 'https://go.dev/VERSION?m=text' | head -n1) && ARCH=$(dpkg --print-architecture) && curl -fsSL "https://go.dev/dl/${GO_VERSION}.linux-${ARCH}.tar.gz" | tar -C /usr/local -xz && ln -sf /usr/local/go/bin/go /usr/local/bin/go && go version
```

Node.js 22 (builds the web interface, and later runs your Node.js sites):

```bash
curl -fsSL https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key | gpg --dearmor --yes -o /usr/share/keyrings/nodesource.gpg && echo "deb [signed-by=/usr/share/keyrings/nodesource.gpg] https://deb.nodesource.com/node_22.x nodistro main" > /etc/apt/sources.list.d/nodesource.list && apt update && apt -y install nodejs && node --version
```

## 5. Download and build hostdeck

```bash
git clone https://github.com/eusofali/cPanel-Alter.git /opt/hostdeck
```

```bash
cd /opt/hostdeck && make build
```

The first build downloads dependencies and takes a few minutes on a small
server. It finishes by creating `bin/hostdeck`.

*Alternative for very small servers:* run `make build` on your own Linux
computer (or `GOOS=linux GOARCH=amd64 make build` on a Mac), copy the whole
project folder to `/opt/hostdeck` on the server with `scp -r`, and skip the
Go and Node.js installs above.

## 6. Run the installer

```bash
cd /opt/hostdeck && ./scripts/install.sh
```

Add options to leave parts out or to get more PHP versions:

| Option | Effect |
| --- | --- |
| `--no-mail` | No Postfix/Dovecot. Use on 512 MB servers or if you use an external mail service. |
| `--no-dns` | No BIND. Use if your registrar or Cloudflare hosts your DNS (the common case). |
| `--no-ftp` | No Pure-FTPd. The built-in file manager still works. |
| `--no-apps` | No Node.js or Python site support. |
| `--no-firewall` | Do not touch ufw or fail2ban. |
| `--php-repo` | Add the Ondrej Sury PHP repository so several PHP versions (7.4 to the newest) and more extensions are installable. Recommended. |

For example, a typical small site server with DNS at the registrar:

```bash
cd /opt/hostdeck && ./scripts/install.sh --no-dns --php-repo
```

What the installer does, in order: adds swap if memory is low; installs the
packages; configures Nginx, PHP-FPM, MariaDB (sized to your RAM), mail, FTP
and DNS; opens only the needed firewall ports (keeping your SSH port);
enables fail2ban; installs hostdeck as a systemd service.

When it finishes it prints the panel address and the **initial admin
password**. Write it down.

## 7. First sign-in

1. Open `https://203.0.113.10:8443` in your browser.
2. The browser warns about the certificate because it is self-signed.
   Choose *Advanced* → *Proceed*. (Step 13 replaces it with a real one.)
3. Sign in with the initial password.
4. Go to **Settings** and:
   - set **Admin email** (required before HTTPS certificates can be issued),
   - set **Server IPv4 address**,
   - change the admin password.
5. Back on the server, remove the saved initial password:

```bash
rm /var/lib/hostdeck/initial-password.txt
```

## 8. Add your first website

1. At your DNS provider, create two `A` records pointing to the server:
   `example.com` → `203.0.113.10` and `www.example.com` → `203.0.113.10`.
   Wait until `ping example.com` shows your server's IP.
2. In the panel open **Websites** → **Add website**.
3. Enter the domain and choose the type:
   - **PHP application** (WordPress, Laravel, …) and a PHP version,
   - **Static files**,
   - **Node.js app** or **Python app** (see step 9),
   - **Reverse proxy** to a service you run yourself.
4. Click **Create website**. `http://example.com` now shows a placeholder.
5. Click **Enable HTTPS** on the site's row. A free Let's Encrypt
   certificate is issued and HTTP is redirected to HTTPS. Renewal is
   automatic.
6. Upload your files with **Files** (the site's folder is
   `/var/www/example.com/public_html`) or with FTP (step 11).

## 9. Node.js and Python apps

Each app runs as its own background service and restarts automatically if
it crashes or the server reboots.

**Node.js**

1. Add a website with type **Node.js app**. Leave *Start file* as `app.js`
   or enter your own (for example `server.js` or `dist/index.js`).
2. Upload your code to `/var/www/yourdomain/app` (button **Files** on the
   site's row).
3. Your app must listen on the port given in the `PORT` environment
   variable: `app.listen(process.env.PORT)`.
4. On the site's row click **App** → **Run npm install**, which also
   restarts the app. **Recent logs** in the same window shows its output.

**Python** (Flask, Django and other WSGI apps, served by gunicorn)

1. Add a website with type **Python app** and set the *WSGI entry*:
   `app:app` for a Flask `app.py`, or `yourproject.wsgi:application` for
   Django.
2. Upload your code and a `requirements.txt` to `/var/www/yourdomain/app`.
3. Click **App** → **Install requirements.txt**. Packages go into the
   site's own virtual environment at `/var/www/yourdomain/venv`.

Each app is limited to 512 MB of memory and can only write inside its own
site folder. After changing code, click **App** → **Restart app**.

## 10. Databases and PHP extensions

**Database**

1. **Databases** → **Add database**, for example `shop_db`.
2. **Add user**, click **Generate** for a password and copy it, tick the
   database, **Create user**.
3. In your application use host `localhost`, and that database name, user
   and password.

**PHP extensions**

Open **PHP Extensions**, pick the PHP version tab and click **Install** on
what you need (for example `imagick`, `redis`, `soap`, `intl`). The list is
everything your server's package sources offer; run the installer with
`--php-repo` for the widest choice.

- **sodium** is compiled into PHP on Ubuntu and Debian. It is shown as
  *Built in, always on* and needs no install.
- **sourceguardian** is a commercial loader. Clicking **Install** downloads
  the official loader from sourceguardian.com for that PHP version and
  enables it. A loader for the very newest PHP version may not exist yet;
  the panel tells you if so. The firewall must allow outgoing HTTPS (it
  does by default).

## 11. FTP accounts

1. **FTP Accounts** → **Add account**, choose a user name, generate a
   password, and pick the home folder (for example `/example.com`).
2. In your FTP program (FileZilla, WinSCP) use: host = your server IP,
   port 21, encryption **"Require explicit FTP over TLS"**, and accept the
   certificate the first time.

## 12. Email and DNS

**DNS records for mail** (at your DNS provider, or in the panel's Zone
Editor if you use it):

| Type | Name | Value |
| --- | --- | --- |
| A | `mail` | `203.0.113.10` |
| MX | `@` | `mail.example.com` (priority 10) |
| TXT | `@` | `v=spf1 mx -all` |

Also ask your VPS provider to set **reverse DNS** of the IP to
`mail.example.com`; without it many providers reject your mail.

**Mailboxes**

1. **Email Accounts** → **Add mail domain** → `example.com`.
2. **Add mailbox**, for example `info`, generate a password, set a quota.
3. Mail program settings: IMAP `mail.example.com` port 993 (SSL/TLS),
   SMTP `mail.example.com` port 587 (STARTTLS), user name = the full
   address.

The mail server starts with a self-signed certificate, so mail programs
warn once. There is no spam filter or DKIM signing yet.

**Hosting DNS on this server** (only if you did not use `--no-dns`)

1. **Zone Editor** → **Add zone** → your domain and server IP. Starter
   records (`@`, `www`, `ns1`, `ns2`, and optionally mail) are created.
2. At your registrar, register the name servers `ns1.example.com` and
   `ns2.example.com` with your server's IP ("glue records" / "child name
   servers"), then set them as the domain's name servers.
3. Changes can take up to a day to be seen everywhere.

## 13. A real certificate for the panel (recommended)

1. Create an `A` record `panel.example.com` → `203.0.113.10` and wait for
   it to resolve.
2. Request a certificate:

```bash
certbot certonly --webroot -w /var/www/_letsencrypt -d panel.example.com --agree-tos -m you@example.com --non-interactive
```

3. Tell hostdeck to use it:

```bash
sed -i 's|^ExecStart=.*|ExecStart=/usr/local/bin/hostdeck -data /var/lib/hostdeck -listen :8443 -tls-cert /etc/letsencrypt/live/panel.example.com/fullchain.pem -tls-key /etc/letsencrypt/live/panel.example.com/privkey.pem|' /etc/systemd/system/hostdeck.service && systemctl daemon-reload && systemctl restart hostdeck
```

4. Make the panel pick up renewed certificates:

```bash
printf '#!/bin/sh\nsystemctl restart hostdeck\n' > /etc/letsencrypt/renewal-hooks/deploy/hostdeck-panel.sh && chmod +x /etc/letsencrypt/renewal-hooks/deploy/hostdeck-panel.sh
```

Now use `https://panel.example.com:8443`.

**Lock the panel to your own IP** if you have a fixed one:

```bash
ufw delete allow 8443/tcp && ufw allow from YOUR.HOME.IP.ADDRESS to any port 8443 proto tcp
```

## 14. Troubleshooting

| Problem | What to check |
| --- | --- |
| Panel does not open | `systemctl status hostdeck` and `journalctl -u hostdeck -n 50`. Check the firewall: `ufw status` must list 8443. Some providers also have a cloud firewall in their dashboard. |
| Forgot the admin password | `hostdeck set-password` on the server. |
| "Enable HTTPS" fails | The domain (and every alias) must already point to this server, and port 80 must be reachable. Admin email must be set in Settings. Details: `/var/log/letsencrypt/letsencrypt.log`. |
| Site shows 502 | PHP site: `systemctl status php8.3-fpm`. Node/Python site: **App** → **Recent logs**, usually a crash on start or the app not listening on `PORT`. |
| A change is rejected with "nginx rejected the configuration" | The panel rolled the change back. Run `nginx -t` to see the conflicting line, often a leftover file in `/etc/nginx/sites-enabled`. |
| Mail is not received | `journalctl -u postfix -n 50`; check the MX record and that port 25 is open at your provider. |
| Mail is not delivered to others | Outgoing port 25 blocked by the provider, or missing reverse DNS / SPF. |
| FTP login fails | Use explicit TLS; `journalctl -u pure-ftpd -n 50`. |
| A service is down | **Services** in the panel shows status and has Restart buttons. |

Useful locations:

| What | Where |
| --- | --- |
| Panel data (database, certificate) | `/var/lib/hostdeck` |
| Website files | `/var/www/<domain>` |
| Site Nginx configs | `/etc/nginx/hostdeck/sites` |
| Site logs | `/var/log/nginx/<domain>.access.log` and `.error.log` |
| App services | `systemctl status hostdeck-site<ID>` |

## 15. Backups

hostdeck does not make backups yet. At minimum, copy these off the server
regularly: `/var/www`, `/var/lib/hostdeck`, `/var/mail/vhosts`, and a
database dump:

```bash
mariadb-dump --all-databases | gzip > /root/all-databases-$(date +%F).sql.gz
```

## 16. Updating hostdeck

```bash
cd /opt/hostdeck && git pull && make build && install -m 755 bin/hostdeck /usr/local/bin/hostdeck && systemctl restart hostdeck
```

Your sites, databases and mail keep running while the panel restarts.

## 17. Removing hostdeck

This stops and removes the panel only. Websites, databases and mail stay
in place and keep working; remove those packages separately if you want.

```bash
systemctl disable --now hostdeck && rm /usr/local/bin/hostdeck /etc/systemd/system/hostdeck.service && systemctl daemon-reload
```
