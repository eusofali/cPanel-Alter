import { useState } from "react";
import { api } from "../api";
import { Badge, fmtDate, Loading, Page, useLoad, useToast } from "../ui";

type Sys = {
  services: { name: string; status: string }[];
  activity: { at: number; action: string; detail: string }[];
};

export default function Server() {
  const { data, error, reload } = useLoad<Sys>("/system");
  const [busy, setBusy] = useState("");
  const toast = useToast();

  const act = async (name: string, action: string) => {
    setBusy(name);
    try {
      await api("POST", `/services/${name}/${action}`);
      toast(`${name}: ${action} done`);
      reload();
    } catch (e: any) {
      toast(e.message, "error");
    } finally {
      setBusy("");
    }
  };

  return (
    <Page title="Server" subtitle="Service status and a record of changes made through the panel." icon="server"
      actions={<button className="ghost" onClick={reload}>Refresh</button>}>
      {!data ? <Loading error={error} /> : (
        <div className="grid two">
          <div className="card table-wrap">
            <h3 className="card-title">Services</h3>
            <table>
              <thead><tr><th>Service</th><th>Status</th><th /></tr></thead>
              <tbody>
                {data.services.map((s) => (
                  <tr key={s.name}>
                    <td className="mono">{s.name}</td>
                    <td><Badge kind={s.status === "active" ? "ok" : s.status === "simulated" ? "off" : "warn"}>{s.status}</Badge></td>
                    <td className="right nowrap">
                      <button className="ghost small" disabled={busy === s.name} onClick={() => act(s.name, "reload")}>Reload</button>
                      <button className="ghost small" disabled={busy === s.name} onClick={() => act(s.name, "restart")}>Restart</button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className="card table-wrap">
            <h3 className="card-title">Activity log</h3>
            {data.activity.length === 0 ? <p className="muted pad">Nothing yet.</p> : (
              <table>
                <thead><tr><th>When</th><th>Action</th><th>Details</th></tr></thead>
                <tbody>
                  {data.activity.map((a, i) => (
                    <tr key={i}>
                      <td className="nowrap small muted">{fmtDate(a.at)}</td>
                      <td className="mono small">{a.action}</td>
                      <td className="small wrap">{a.detail}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </div>
      )}
    </Page>
  );
}
