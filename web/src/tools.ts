export type Tool = { id: string; label: string; desc: string; route: string; icon: string; keywords?: string };
export type Group = { id: string; label: string; tone: string; tools: Tool[] };

/** Every tool the panel offers, grouped the way the home screen shows them. */
export const groups: Group[] = [
  {
    id: "domains", label: "Domains", tone: "blue",
    tools: [
      { id: "sites", label: "Websites", desc: "Add domains and choose how they are served", route: "sites", icon: "globe", keywords: "domain addon alias subdomain nginx vhost proxy access error logs" },
      { id: "ssl", label: "SSL/TLS", desc: "Free HTTPS certificates and redirects", route: "sites", icon: "lock", keywords: "https certificate lets encrypt" },
      { id: "dns", label: "Zone Editor", desc: "DNS zones and records", route: "dns", icon: "network", keywords: "dns a cname mx txt ns record nameserver bind" },
    ],
  },
  {
    id: "files", label: "Files", tone: "amber",
    tools: [
      { id: "files", label: "File Manager", desc: "Browse, upload and edit site files", route: "files", icon: "folder", keywords: "upload edit public_html" },
      { id: "backups", label: "Backups", desc: "Back up and restore sites, databases and mail", route: "backups", icon: "archive", keywords: "backup restore download schedule archive migrate" },
      { id: "ftp", label: "FTP Accounts", desc: "Logins locked to a single folder", route: "ftp", icon: "transfer", keywords: "ftps upload deploy" },
    ],
  },
  {
    id: "databases", label: "Databases", tone: "teal",
    tools: [
      { id: "databases", label: "Databases", desc: "Create and remove MariaDB databases", route: "databases", icon: "database", keywords: "mysql mariadb sql" },
      { id: "dbusers", label: "Database Users", desc: "Logins, passwords and privileges", route: "databases", icon: "users", keywords: "mysql user grant password" },
    ],
  },
  {
    id: "email", label: "Email", tone: "rose",
    tools: [
      { id: "mail", label: "Email Accounts", desc: "Mailboxes with quotas", route: "mail", icon: "mail", keywords: "mailbox imap smtp postfix dovecot" },
      { id: "forwarders", label: "Forwarders", desc: "Forward or catch-all addresses", route: "mail", icon: "forward", keywords: "alias catch-all redirect" },
    ],
  },
  {
    id: "software", label: "Software", tone: "violet",
    tools: [
      { id: "apps", label: "Node.js & Python Apps", desc: "Run an app as a managed service", route: "sites", icon: "code", keywords: "node nodejs npm python flask django gunicorn wsgi express" },
      { id: "php", label: "PHP Extensions", desc: "Install extensions for each PHP version", route: "php", icon: "puzzle", keywords: "php module version imagick redis sodium sourceguardian loader" },
    ],
  },
  {
    id: "server", label: "Server", tone: "slate",
    tools: [
      { id: "cron", label: "Cron Jobs", desc: "Run commands on a schedule", route: "cron", icon: "clock", keywords: "schedule task crontab timer" },
      { id: "services", label: "Services", desc: "Status and restart of server software", route: "server", icon: "server", keywords: "restart nginx mariadb status" },
      { id: "activity", label: "Activity Log", desc: "Recent changes made in the panel", route: "server", icon: "activity", keywords: "audit history log" },
      { id: "settings", label: "Settings", desc: "Admin email and server address", route: "settings", icon: "settings", keywords: "preferences ip email" },
      { id: "password", label: "Password & Security", desc: "Admin password and two-factor sign-in", route: "settings", icon: "shield", keywords: "login security 2fa two-factor totp authenticator" },
    ],
  },
];

export const allTools = groups.flatMap((g) => g.tools.map((t) => ({ ...t, group: g.label, tone: g.tone })));

export function searchTools(query: string) {
  const s = query.trim().toLowerCase();
  if (!s) return [];
  return allTools.filter((t) => `${t.label} ${t.desc} ${t.group} ${t.keywords || ""}`.toLowerCase().includes(s));
}

export function routeInfo(route: string) {
  return allTools.find((t) => t.route === route);
}
