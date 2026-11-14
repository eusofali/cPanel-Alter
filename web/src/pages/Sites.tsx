import { useState } from "react";
import { api } from "../api";
import { Badge, Empty, Field, FormModal, Loading, Modal, Page, useLoad, useToast } from "../ui";

type Site = {
  id: number;
  domain: string;
  aliases: string[];
  type: "php" | "static" | "proxy" | "node" | "python";
  entry: string;
  port: number;
  php_version: string;
  proxy_target: string;
  ssl: boolean;
  force_https: boolean;
  enabled: boolean;
  docroot: string;
};
type Runtimes = { node: string; python: string };
type Data = { sites: Site[]; php_versions: string[]; runtimes: Runtimes };

function SiteForm({ site, php, runtimes, onClose, onSaved }: { site?: Site; php: string[]; runtimes: Runtimes; onClose: () => void; onSaved: () => void }) {
  const [domain, setDomain] = useState(site?.domain || "");
  const [aliases, setAliases] = useState(site ? site.aliases.join(" ") : "");
  const [www, setWww] = useState(!site);
  const [type, setType] = useState<Site["type"]>(site?.type || (php.length ? "php" : "static"));
  const [version, setVersion] = useState(site?.php_version || php[0] || "");
  const [proxy, setProxy] = useState(site?.proxy_target || "http://127.0.0.1:3000");
  const [entry, setEntry] = useState(site?.entry || "");
  const [force, setForce] = useState(site?.force_https || false);

  const save = async () => {
    const list = aliases.split(/[\s,]+/).filter(Boolean);
    if (!site && www && domain.trim()) list.push("www." + domain.trim().toLowerCase());
    const body = { domain, aliases: list, type, php_version: version, proxy_target: proxy, entry, force_https: force };
    if (site) await api("PUT", `/sites/${site.id}`, body);
    else await api("POST", "/sites", body);
    onSaved();
  };

  return (
    <FormModal title={site ? `Edit ${site.domain}` : "Add website"} submit={site ? "Save changes" : "Create website"} done={site ? "Website updated" : "Website created"} onClose={onClose} action={save}>
      {!site && (
        <Field label="Domain">
          <input value={domain} onChange={(e) => setDomain(e.target.value)} placeholder="example.com" required autoFocus spellCheck={false} />
        </Field>
      )}
      {!site && (
        <label className="check">
          <input type="checkbox" checked={www} onChange={(e) => setWww(e.target.checked)} /> Also serve www.{domain || "example.com"}
        </label>
      )}
      <Field label={site ? "Aliases" : "Other aliases"} hint="Extra domain names for this site, separated by spaces.">
        <input value={aliases} onChange={(e) => setAliases(e.target.value)} spellCheck={false} />
      </Field>
      <Field label="Site type">
        <select value={type} onChange={(e) => { setType(e.target.value as Site["type"]); setEntry(""); }}>
          <option value="php" disabled={!php.length}>PHP application</option>
          <option value="static">Static files</option>
          <option value="node" disabled={!runtimes.node}>Node.js app{runtimes.node ? ` (${runtimes.node})` : " (not installed)"}</option>
          <option value="python" disabled={!runtimes.python}>Python app{runtimes.python ? ` (${runtimes.python})` : " (not installed)"}</option>
          <option value="proxy">Reverse proxy to another service</option>
        </select>
      </Field>
      {type === "php" && (
        <Field label="PHP version">
          <select value={version} onChange={(e) => setVersion(e.target.value)}>
            {php.map((v) => <option key={v}>{v}</option>)}
          </select>
        </Field>
      )}
      {type === "node" && (
        <Field label="Start file" hint="Relative to the site's app folder. Your app must listen on the PORT environment variable.">
          <input value={entry} onChange={(e) => setEntry(e.target.value)} placeholder="app.js" spellCheck={false} />
        </Field>
      )}
      {type === "python" && (
        <Field label="WSGI entry" hint="module:variable, served by gunicorn. Flask: app:app. Django: yourproject.wsgi:application.">
          <input value={entry} onChange={(e) => setEntry(e.target.value)} placeholder="app:app" spellCheck={false} />
        </Field>
      )}
      {type === "proxy" && (
        <Field label="Proxy target" hint="Where your application listens.">
          <input value={proxy} onChange={(e) => setProxy(e.target.value)} required spellCheck={false} />
        </Field>
      )}
      {site?.ssl && (
        <label className="check">
          <input type="checkbox" checked={force} onChange={(e) => setForce(e.target.checked)} /> Redirect HTTP to HTTPS
        </label>
      )}
    </FormModal>
  );
}

const isApp = (s: Site) => s.type === "node" || s.type === "python";

function AppModal({ site, onClose }: { site: Site; onClose: () => void }) {
  const { data, error, reload } = useLoad<{ logs: string; status: string }>(`/sites/${site.id}/app/logs`);
  const [busy, setBusy] = useState("");
  const [output, setOutput] = useState("");
  const toast = useToast();
  const run = async (action: "restart" | "install") => {
    setBusy(action);
    try {
      const r = await api<{ output?: string }>("POST", `/sites/${site.id}/app/${action}`);
      if (action === "install") setOutput(r.output || "Dependencies installed.");
      toast(action === "restart" ? "App restarted" : "Dependencies installed, app restarted");
      reload();
    } catch (e: any) {
      toast(e.message, "error");
    } finally {
      setBusy("");
    }
  };
  const node = site.type === "node";
  return (
    <Modal title={`${node ? "Node.js" : "Python"} app · ${site.domain}`} onClose={onClose} wide>
      <div className="modal-body">
        <div className="app-bar">
          <span>Status: <Badge kind={data?.status === "active" ? "ok" : data?.status === "simulated" ? "off" : "warn"}>{data?.status || "…"}</Badge></span>
          <span className="muted small">Code lives in <span className="mono">/var/www/{site.domain}/app</span>, start: <span className="mono">{site.entry}</span></span>
        </div>
        <div className="app-actions">
          <button className="primary" disabled={busy !== ""} onClick={() => run("restart")}>{busy === "restart" ? "Restarting…" : "Restart app"}</button>
          <button className="ghost" disabled={busy !== ""} onClick={() => run("install")}>
            {busy === "install" ? "Installing…" : node ? "Run npm install" : "Install requirements.txt"}
          </button>
          <button className="ghost" onClick={reload}>Refresh logs</button>
        </div>
        {output && <><h3>Install output</h3><pre className="logs">{output}</pre></>}
        <h3>Recent logs</h3>
        {!data ? <Loading error={error} /> : <pre className="logs">{data.logs || "No log lines yet."}</pre>}
      </div>
      <footer><button className="ghost" onClick={onClose}>Close</button></footer>
    </Modal>
  );
}

function LogsModal({ site, onClose }: { site: Site; onClose: () => void }) {
  const [kind, setKind] = useState<"access" | "error">("access");
  const { data, error, reload } = useLoad<{ log: string }>(`/sites/${site.id}/logs?kind=${kind}`);
  return (
    <Modal title={`Logs · ${site.domain}`} onClose={onClose} wide>
      <div className="modal-body">
        <div className="toolbar">
          <div className="tabs" role="tablist">
            <button role="tab" aria-selected={kind === "access"} className={kind === "access" ? "active" : ""} onClick={() => setKind("access")}>Visitors (access log)</button>
            <button role="tab" aria-selected={kind === "error"} className={kind === "error" ? "active" : ""} onClick={() => setKind("error")}>Errors</button>
          </div>
          <button className="ghost" onClick={reload}>Refresh</button>
        </div>
        {!data ? <Loading error={error} /> : <pre className="logs tall">{data.log || "No entries yet."}</pre>}
      </div>
      <footer><button className="ghost" onClick={onClose}>Close</button></footer>
    </Modal>
  );
}

export default function Sites() {
  const { data, error, reload } = useLoad<Data>("/sites");
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<Site | null>(null);
  const [deleting, setDeleting] = useState<Site | null>(null);
  const [removeFiles, setRemoveFiles] = useState(false);
  const [busy, setBusy] = useState(0);
  const [app, setApp] = useState<Site | null>(null);
  const [logs, setLogs] = useState<Site | null>(null);
  const toast = useToast();

  const run = async (site: Site, fn: () => Promise<unknown>, done: string) => {
    setBusy(site.id);
    try {
      await fn();
      toast(done);
      reload();
    } catch (e: any) {
      toast(e.message, "error");
    } finally {
      setBusy(0);
    }
  };
  const toggle = (s: Site) =>
    run(s, () => api("PUT", `/sites/${s.id}`, { ...s, enabled: !s.enabled }), s.enabled ? `${s.domain} disabled` : `${s.domain} enabled`);

  return (
    <Page title="Websites" subtitle="Domains on this server: PHP, static, Node.js and Python sites." actions={<button className="primary" onClick={() => setAdding(true)}>Add website</button>}>
      {!data ? (
        <Loading error={error} />
      ) : data.sites.length === 0 ? (
        <Empty>No websites yet. Add your first domain to get started.</Empty>
      ) : (
        <div className="card table-wrap">
          <table>
            <thead>
              <tr><th>Domain</th><th>Type</th><th>HTTPS</th><th>Status</th><th /></tr>
            </thead>
            <tbody>
              {data.sites.map((s) => (
                <tr key={s.id}>
                  <td>
                    <a href={`${s.ssl ? "https" : "http"}://${s.domain}`} target="_blank" rel="noreferrer noopener"><strong>{s.domain}</strong></a>
                    {s.aliases.length > 0 && <div className="muted small">{s.aliases.join(", ")}</div>}
                  </td>
                  <td>
                    {s.type === "php" ? `PHP ${s.php_version}` : s.type === "proxy" ? <span className="mono small">{s.proxy_target}</span> : s.type === "node" ? "Node.js" : s.type === "python" ? "Python" : "Static"}
                    {isApp(s) && <div className="muted small mono">{s.entry} · port {s.port}</div>}
                  </td>
                  <td>
                    {s.ssl ? <Badge kind="ok">Let's Encrypt</Badge> : (
                      <button className="ghost small" disabled={busy === s.id || !s.enabled} onClick={() => run(s, () => api("POST", `/sites/${s.id}/ssl`), `Certificate issued for ${s.domain}`)}>
                        {busy === s.id ? "Requesting…" : "Enable HTTPS"}
                      </button>
                    )}
                  </td>
                  <td><Badge kind={s.enabled ? "ok" : "off"}>{s.enabled ? "Live" : "Disabled"}</Badge></td>
                  <td className="right nowrap">
                    <a className="btn ghost small" href={`#/files`} onClick={() => sessionStorage.setItem("hmea-panel:path", `/${s.domain}/${isApp(s) ? "app" : "public_html"}`)}>Files</a>
                    {isApp(s) && <button className="ghost small" onClick={() => setApp(s)}>App</button>}
                    <button className="ghost small" onClick={() => setLogs(s)}>Logs</button>
                    <button className="ghost small" onClick={() => setEditing(s)}>Edit</button>
                    <button className="ghost small" disabled={busy === s.id} onClick={() => toggle(s)}>{s.enabled ? "Disable" : "Enable"}</button>
                    {s.ssl && <button className="ghost small" onClick={() => run(s, () => api("DELETE", `/sites/${s.id}/ssl`), "HTTPS turned off")}>HTTPS off</button>}
                    <button className="ghost small danger-text" onClick={() => { setRemoveFiles(false); setDeleting(s); }}>Delete</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {logs && <LogsModal site={logs} onClose={() => setLogs(null)} />}
      {app && <AppModal site={app} onClose={() => setApp(null)} />}
      {adding && data && <SiteForm runtimes={data.runtimes} php={data.php_versions} onClose={() => setAdding(false)} onSaved={reload} />}
      {editing && data && <SiteForm site={editing} runtimes={data.runtimes} php={data.php_versions} onClose={() => setEditing(null)} onSaved={reload} />}
      {deleting && (
        <FormModal title={`Delete ${deleting.domain}?`} submit="Delete website" danger done="Website deleted" onClose={() => setDeleting(null)}
          action={async () => { await api("DELETE", `/sites/${deleting.id}${removeFiles ? "?files=1" : ""}`); reload(); }}>
          <p>The site stops being served immediately. Databases, mailboxes and DNS records are not touched.</p>
          <label className="check">
            <input type="checkbox" checked={removeFiles} onChange={(e) => setRemoveFiles(e.target.checked)} /> Also delete all files in <span className="mono">/var/www/{deleting.domain}</span>. This cannot be undone.
          </label>
        </FormModal>
      )}
    </Page>
  );
}
