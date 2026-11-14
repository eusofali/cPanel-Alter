# hostdeck

A lightweight web hosting control panel for Ubuntu 22.04/24.04 and Debian 12:
one Go binary (about 13 MB on disk, under 20 MB of RAM when idle) with an
embedded React UI and a SQLite database. No PHP, Perl or database server is
needed for the panel itself.

| Area | What it manages | Backed by |
| --- | --- | --- |
| Websites | Domains, aliases, PHP / static / reverse-proxy sites, enable/disable | Nginx |
| HTTPS | Free certificates, HTTP→HTTPS redirect, automatic renewal | certbot (Let's Encrypt) |
| PHP | A version per site, install/remove any extension apt offers | PHP-FPM, apt |
| Databases | Databases, users, passwords, grants, sizes | MariaDB |
| Files | Browse, upload, edit, rename, delete, download under `/var/www` | built in |
| FTP | Accounts locked to one folder, TLS required | Pure-FTPd |
| Email | Mail domains, mailboxes with quota, forwarders, catch-all | Postfix, Dovecot |
| DNS | Zones and A/AAAA/CNAME/MX/TXT/NS/SRV/CAA records | BIND 9 |

## Try it without a server

```bash
make dev
```

Opens on http://127.0.0.1:8080. In `-dev` mode nothing on the machine is
changed: config files are written under `data/devroot` and commands are only
logged. The generated admin password is in `data/initial-password.txt`.

## Install on a server

Use a fresh VPS. The installer reconfigures Nginx, MariaDB, Postfix, Dovecot,
Pure-FTPd, BIND and the firewall.

```bash
make build
```

```bash
sudo scripts/install.sh
```

Options: `--no-mail`, `--no-dns`, `--no-ftp` (skip what you do not need; on a
512 MB server skip mail), `--no-firewall`, and `--php-repo` (adds the Ondrej
Sury repository for several PHP versions side by side and more extensions).

The panel listens on `https://SERVER:8443` with a self-signed certificate.
To use a real one, pass `-tls-cert` and `-tls-key` in
`/etc/systemd/system/hostdeck.service`. Reset a lost password with
`sudo hostdeck set-password`.

## Built for small servers

- Every PHP site has its own FPM pool in `ondemand` mode: idle sites use no
  PHP workers at all.
- The installer sizes MariaDB to the machine (64 MB buffer pool below 1.5 GB
  RAM, performance schema off) and adds a 1 GB swap file below 2 GB RAM.
- The panel runs with `GOMEMLIMIT=64MiB`; mail, DNS and FTP are optional.

## Security model

- One admin account: bcrypt password, 12-hour sessions stored hashed,
  `HttpOnly` + `SameSite=Strict` cookies, login throttling per IP, CSRF
  header check, strict Content-Security-Policy, TLS by default.
- Every name that reaches a config file or command is validated against a
  strict pattern; commands are run without a shell.
- Nginx, PHP-FPM and BIND configs are checked (`nginx -t`, `php-fpm -t`,
  `named-checkzone`) and rolled back if rejected.
- PHP sites are confined with `open_basedir` to their own folder, and
  `exec`, `shell_exec`, `system`, `passthru`, `proc_open`, `popen` are
  disabled. Edit `poolTmpl` in `internal/app/sites.go` if an app needs them.
- The file manager resolves symlinks and refuses anything outside `/var/www`.
- The installer enables ufw (only the needed ports, keeping your SSH port)
  and fail2ban, binds MariaDB to localhost, makes BIND authoritative-only,
  and requires TLS for FTP, IMAP and mail submission.

Known limits: the panel runs as root (it has to edit system config), all
sites run as `www-data` (isolation is `open_basedir`, not separate Unix
users), there is no two-factor login yet, and no DKIM signing or spam
filtering for mail. Restrict port 8443 to your own IP if you can:
`sudo ufw delete allow 8443/tcp` then `sudo ufw allow from YOUR_IP to any port 8443 proto tcp`.

## Development

```bash
make test
```

Layout: `cmd/hostdeck` (entry point), `internal/app` (API and services, one
file per area), `internal/sys` (the only code that touches the host),
`web` (React UI), `scripts` (installer, systemd unit).
