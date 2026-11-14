import { useEffect } from "react";
import { api } from "../api";
import { Badge, fmtBytes, fmtDate, Loading, Page, useLoad, useToast } from "../ui";

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
  activity: { at: number; action: string; detail: string }[];
};

function Meter({ label, used, total }: { label: string; used: number; total: number }) {
  const pct = total ? Math.round((used / total) * 100) : 0;
  return (
    <div className="card stat">
      <div className="stat-label">{label}</div>
      <div className="stat-value">{pct}%</div>
      <div className="meter" role="meter" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100} aria-label={label}>
        <div className={pct > 90 ? "hot" : ""} style={{ width: `${pct}%` }} />
      </div>
      <div className="muted small">{fmtBytes(used)} of {fmtBytes(total)}</div>
    </div>
  );
}

function uptime(s?: number) {
  if (!s) return "–";
  const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60);
  return d ? `${d}d ${h}h` : `${h}h ${m}m`;
}

export default function Dashboard() {
  const { data, error, reload } = useLoad<Sys>("/system");
  const toast = useToast();
  useEffect(() => {
    const t = setInterval(reload, 15000);
    return () => clearInterval(t);
  }, [reload]);
  if (!data) return <Page title="Dashboard"><Loading error={error} /></Page>;

  const restart = async (name: string) => {
    try {
      await api("POST", `/services/${name}/restart`);
      toast(`${name} restarted`);
      reload();
    } catch (e: any) {
      toast(e.message, "error");
    }
  };
  const c = data.counts;
  return (
    <Page title="Dashboard" subtitle={`${data.hostname} · up ${uptime(data.uptime_seconds)}`}>
      <div className="grid stats">
        <Meter label="Memory" used={data.mem_used} total={data.mem_total} />
        <Meter label="Disk" used={data.disk_used} total={data.disk_total} />
        <div className="card stat">
          <div className="stat-label">Load average</div>
          <div className="stat-value">{data.load?.[0] ?? "–"}</div>
          <div className="muted small">{data.load ? `${data.load[1]} (5 min) · ${data.load[2]} (15 min)` : "not available"}</div>
        </div>
        <div className="card stat">
          <div className="stat-label">Hosted</div>
          <div className="stat-value">{c.sites} <span className="unit">sites</span></div>
          <div className="muted small">{c.databases} databases · {c.mailboxes} mailboxes · {c.zones} DNS zones</div>
        </div>
      </div>

      <div className="grid two">
        <div className="card">
          <h3>Services</h3>
          <table>
            <tbody>
              {data.services.map((s) => (
                <tr key={s.name}>
                  <td>{s.name}</td>
                  <td><Badge kind={s.status === "active" ? "ok" : s.status === "simulated" ? "off" : "warn"}>{s.status}</Badge></td>
                  <td className="right"><button className="ghost small" onClick={() => restart(s.name)}>Restart</button></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <div className="card">
          <h3>Recent activity</h3>
          {data.activity.length === 0 ? (
            <p className="muted">Nothing yet.</p>
          ) : (
            <ul className="activity">
              {data.activity.map((a, i) => (
                <li key={i}>
                  <span className="mono">{a.action}</span> <span>{a.detail}</span>
                  <time>{fmtDate(a.at)}</time>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </Page>
  );
}
