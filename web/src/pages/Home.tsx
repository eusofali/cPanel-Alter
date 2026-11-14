import { useEffect, useState } from "react";
import { Icon } from "../icons";
import { groups } from "../tools";
import { fmtBytes, Loading, useLoad } from "../ui";

type Sys = {
  hostname: string;
  load?: string[];
  uptime_seconds?: number;
  mem_total: number;
  mem_used: number;
  disk_total: number;
  disk_used: number;
  services: { name: string; status: string }[];
  counts: Record<string, number>;
};

function uptime(s?: number) {
  if (!s) return "–";
  const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60);
  return d ? `${d} days ${h} h` : `${h} h ${m} min`;
}

function Usage({ label, used, total }: { label: string; used: number; total: number }) {
  const pct = total ? Math.round((used / total) * 100) : 0;
  return (
    <div className="usage">
      <div className="usage-head"><span>{label}</span><span>{fmtBytes(used)} / {fmtBytes(total)}</span></div>
      <div className="meter" role="meter" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100} aria-label={label}>
        <div className={pct > 90 ? "hot" : pct > 75 ? "warm" : ""} style={{ width: `${pct}%` }} />
      </div>
      <div className="muted small">{pct}% used</div>
    </div>
  );
}

function loadClosed(): string[] {
  try {
    return JSON.parse(localStorage.getItem("hmea-panel:closed") || "[]");
  } catch {
    return [];
  }
}

export default function Home() {
  const { data, error, reload } = useLoad<Sys>("/system");
  const settings = useLoad<{ server_ip: string }>("/settings");
  const [closed, setClosed] = useState<string[]>(loadClosed);
  useEffect(() => {
    const t = setInterval(reload, 20000);
    return () => clearInterval(t);
  }, [reload]);

  const toggle = (id: string) => {
    const next = closed.includes(id) ? closed.filter((x) => x !== id) : [...closed, id];
    setClosed(next);
    try {
      localStorage.setItem("hmea-panel:closed", JSON.stringify(next));
    } catch {
      /* private mode: the choice just is not remembered */
    }
  };
  const down = data?.services.filter((s) => s.status !== "active" && s.status !== "simulated") || [];

  return (
    <section className="page home">
      <div className="home-main">
        <h1 className="home-title">Tools</h1>
        {groups.map((g) => {
          const open = !closed.includes(g.id);
          return (
            <div key={g.id} className="group">
              <button className="group-head" onClick={() => toggle(g.id)} aria-expanded={open}>
                <span>{g.label}</span>
                <span className={`chev${open ? "" : " shut"}`}><Icon name="chevron" size={18} /></span>
              </button>
              {open && (
                <div className="tiles">
                  {g.tools.map((t) => (
                    <a key={t.id} className="tile" href={`#/${t.route}`}>
                      <span className={`tile-icon ${g.tone}`}><Icon name={t.icon} size={24} /></span>
                      <span className="tile-text">
                        <strong>{t.label}</strong>
                        <small>{t.desc}</small>
                      </span>
                    </a>
                  ))}
                </div>
              )}
            </div>
          );
        })}
      </div>

      <aside className="home-side" aria-label="Server overview">
        {!data ? <div className="panel"><Loading error={error} /></div> : (
          <>
            <div className="panel">
              <h2>General information</h2>
              <dl>
                <dt>Server name</dt><dd>{data.hostname}</dd>
                <dt>IP address</dt><dd>{settings.data?.server_ip || <a href="#/settings">Set in Settings</a>}</dd>
                <dt>Uptime</dt><dd>{uptime(data.uptime_seconds)}</dd>
                <dt>Load average</dt><dd>{data.load ? data.load.join(" · ") : "–"}</dd>
                <dt>Services</dt>
                <dd>
                  {down.length === 0
                    ? <span className="badge ok">All running</span>
                    : <a href="#/server" className="badge warn">{down.length} not running</a>}
                </dd>
              </dl>
            </div>
            <div className="panel">
              <h2>Statistics</h2>
              <Usage label="Disk" used={data.disk_used} total={data.disk_total} />
              <Usage label="Memory" used={data.mem_used} total={data.mem_total} />
              <ul className="counts">
                <li><a href="#/sites">Websites</a><b>{data.counts.sites}</b></li>
                <li><a href="#/databases">Databases</a><b>{data.counts.databases}</b></li>
                <li><a href="#/mail">Email accounts</a><b>{data.counts.mailboxes}</b></li>
                <li><a href="#/ftp">FTP accounts</a><b>{data.counts.ftp}</b></li>
                <li><a href="#/dns">DNS zones</a><b>{data.counts.zones}</b></li>
              </ul>
            </div>
          </>
        )}
      </aside>
    </section>
  );
}
