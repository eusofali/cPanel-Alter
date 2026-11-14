# Installing HMEA Panel on a VPS, step by step

This guide takes a new VPS from first login to a working panel with a
website, HTTPS, a database, email and DNS.

> **Status.** The panel and installer have been tested in simulation only,
> not yet on a live server. Do the first install on a fresh VPS you can
> rebuild, and expect to adjust a step or two. If something fails, the
> Troubleshooting section shows where to look.

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

## 3. Install with one command

```bash
apt -y install git && git clone https://github.com/eusofali/cPanel-Alter.git /opt/hmea-panel && bash /opt/hmea-panel/install.sh
```

That single command:

1. downloads HMEA Panel to `/opt/hmea-panel`,
2. adds swap if the server has little memory,
3. installs the build tools (Go and Node.js) and builds the panel, which
   takes a few minutes on a small server,
4. installs and configures Nginx, PHP-FPM, MariaDB, mail, FTP, DNS, Node.js
   and Python, sized to your server's memory,
5. opens only the needed firewall ports (keeping your SSH port) and turns
   on fail2ban,
6. starts HMEA Panel as a service that also starts after every reboot.

When it finishes it prints the panel address and the **initial admin
password**. Write it down.

### Options

Add options to leave parts out or to get more PHP versions:

| Option | Effect |
| --- | --- |
| `--no-mail` | No Postfix/Dovecot. Use on 512 MB servers or if you use an external mail service. |
| `--no-dns` | No BIND. Use if your registrar or Cloudflare hosts your DNS (the common case). |
| `--no-ftp` | No Pure-FTPd. The built-in file manager still works. |
| `--no-apps` | No Node.js or Python site support. |
| `--no-firewall` | Do not touch ufw or fail2ban. |
| `--php-repo` | Add the Ondrej Sury PHP repository so several PHP versions (7.4 to the newest) and more extensions are installable. Recommended. |

Put options at the end of the command. For example, a typical small site
server whose DNS stays at the registrar:

```bash
apt -y install git && git clone https://github.com/eusofali/cPanel-Alter.git /opt/hmea-panel && bash /opt/hmea-panel/install.sh --no-dns --php-repo
```

The installer is safe to run again, for example to add a part you left out:

```bash
bash /opt/hmea-panel/install.sh --php-repo
```

## 4. First sign-in

1. Open `https://203.0.113.10:8443` in your browser.
2. The browser warns about the certificate because it is self-signed.
   Choose *Advanced* → *Proceed*. (The section "A real certificate for the panel" below replaces it.)
3. Sign in with the initial password.
4. Go to **Settings** and:
   - set **Admin email** (required before HTTPS certificates can be issued),
   - set **Server IPv4 address**,
   - change the admin password,
   - turn on **Two-factor sign-in**: click *Set up*, scan the QR code with
     an authenticator app (Google Authenticator, Authy, 1Password, …) and
     enter the 6-digit code. If you ever lose the phone, run
     `hmea-panel disable-2fa` on the server.
5. Back on the server, remove the saved initial password:

```bash
rm /var/lib/hmea-panel/initial-password.txt
```

## 5. Add your first website

1. At your DNS provider, create two `A` records pointing to the server:
   `example.com` → `203.0.113.10` and `www.example.com` → `203.0.113.10`.
   Wait until `ping example.com` shows your server's IP.
2. In the panel open **Websites** → **Add website**.
3. Enter the domain and choose the type:
   - **PHP application** (WordPress, Laravel, …) and a PHP version,
   - **Static files**,
   - **Node.js app** or **Python app** (see the next section),
   - **Reverse proxy** to a service you run yourself.
4. Click **Create website**. `http://example.com` now shows a placeholder.
5. Click **Enable HTTPS** on the site's row. A free Let's Encrypt
   certificate is issued and HTTP is redirected to HTTPS. Renewal is
   automatic.
6. Upload your files with **Files** (the site's folder is
   `/var/www/example.com/public_html`) or with FTP.

## 6. Node.js and Python apps

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

## 7. Databases and PHP extensions

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

## 8. FTP accounts

1. **FTP Accounts** → **Add account**, choose a user name, generate a
   password, and pick the home folder (for example `/example.com`).
2. In your FTP program (FileZilla, WinSCP) use: host = your server IP,
   port 21, encryption **"Require explicit FTP over TLS"**, and accept the
   certificate the first time.

## 9. Email and DNS

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

## 10. A real certificate for the panel (recommended)

1. Create an `A` record `panel.example.com` → `203.0.113.10` and wait for
   it to resolve.
2. Request a certificate:

```bash
certbot certonly --webroot -w /var/www/_letsencrypt -d panel.example.com --agree-tos -m you@example.com --non-interactive
```

3. Tell HMEA Panel to use it:

```bash
sed -i 's|^ExecStart=.*|ExecStart=/usr/local/bin/hmea-panel -data /var/lib/hmea-panel -listen :8443 -tls-cert /etc/letsencrypt/live/panel.example.com/fullchain.pem -tls-key /etc/letsencrypt/live/panel.example.com/privkey.pem|' /etc/systemd/system/hmea-panel.service && systemctl daemon-reload && systemctl restart hmea-panel
```

4. Make the panel pick up renewed certificates:

```bash
printf '#!/bin/sh\nsystemctl restart hmea-panel\n' > /etc/letsencrypt/renewal-hooks/deploy/hmea-panel-panel.sh && chmod +x /etc/letsencrypt/renewal-hooks/deploy/hmea-panel-panel.sh
```

Now use `https://panel.example.com:8443`.

**Lock the panel to your own IP** if you have a fixed one:

```bash
ufw delete allow 8443/tcp && ufw allow from YOUR.HOME.IP.ADDRESS to any port 8443 proto tcp
```

## 11. Troubleshooting

| Problem | What to check |
| --- | --- |
| Panel does not open | `systemctl status hmea-panel` and `journalctl -u hmea-panel -n 50`. Check the firewall: `ufw status` must list 8443. Some providers also have a cloud firewall in their dashboard. |
| Forgot the admin password | `hmea-panel set-password` on the server. |
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
| Panel data (database, certificate) | `/var/lib/hmea-panel` |
| Website files | `/var/www/<domain>` |
| Site Nginx configs | `/etc/nginx/hmea-panel/sites` |
| Site logs | `/var/log/nginx/<domain>.access.log` and `.error.log` |
| App services | `systemctl status hmea-site<ID>` |

## 12. Backups, restore and scheduled tasks

**Create a backup:** **Backups** → **Create backup**, then choose:

| Type | Contains |
| --- | --- |
| Full server | Every website folder, a dump of every database, all mailboxes, and a copy of the panel's own database |
| One website | That site's folder under `/var/www` |
| One database | A compressed SQL dump |

Backups run in the background and are stored in `/var/backups/hmea-panel`.
Use **Download** to keep a copy on your computer.

**Automatic backups:** on the same page set *Frequency* to every day or
every week and choose how many to keep; older automatic backups are
deleted for you.

> A backup on the same disk does not protect you if the server is lost.
> Download backups or copy `/var/backups/hmea-panel` to another machine
> regularly.

**Restore:** click **Restore** on a backup. For a full backup, tick what to
bring back (website files, databases, mailboxes). Files in the backup
overwrite current ones; files created later are kept. Databases are
replaced by their backed-up contents.

**Move to a new server:** install HMEA Panel there, copy the full backup
file into `/var/backups/hmea-panel` on the new server, open **Backups**
(the file appears automatically) and restore it. Then re-add the websites,
database users, mailboxes and DNS zones in the panel: a restore brings back
the *data*, while panel settings are not recreated automatically. The
backup also contains the old panel database (`panel/hmea-panel.db`) for
reference.

**Cron jobs:** **Cron Jobs** → **Add cron job**, pick how often and enter
the command, for example `php /var/www/example.com/public_html/wp-cron.php`.
Jobs run as the web user, not as root.

**Site logs:** on the **Websites** page click **Logs** on a site's row to
see its latest visitor and error log lines.

## 13. Updating HMEA Panel

```bash
bash /opt/hmea-panel/update.sh
```

This downloads the newest version, rebuilds it and restarts only the panel.
Your sites, databases and mail keep running.

## 14. Removing HMEA Panel

This stops and removes the panel only. Websites, databases and mail stay
in place and keep working; remove those packages separately if you want.

```bash
systemctl disable --now hmea-panel && rm /usr/local/bin/hmea-panel /etc/systemd/system/hmea-panel.service && systemctl daemon-reload
```
