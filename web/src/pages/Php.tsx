import { useState } from "react";
import { api } from "../api";
import { Empty, Loading, Page, useLoad, useToast } from "../ui";

type Ext = { name: string; installed: boolean };
type Data = { versions: { version: string; extensions: Ext[] }[] };

export default function Php() {
  const { data, error, reload } = useLoad<Data>("/php");
  const [tab, setTab] = useState("");
  const [filter, setFilter] = useState("");
  const [busy, setBusy] = useState("");
  const toast = useToast();

  if (!data) return <Page title="PHP"><Loading error={error} /></Page>;
  if (data.versions.length === 0)
    return <Page title="PHP"><Empty>No PHP-FPM version is installed on this server.</Empty></Page>;

  const current = data.versions.find((v) => v.version === tab) || data.versions[0];
  const shown = current.extensions.filter((e) => e.name.includes(filter.trim().toLowerCase()));
  const installed = current.extensions.filter((e) => e.installed).length;

  const toggle = async (ext: Ext) => {
    setBusy(ext.name);
    try {
      await api("POST", `/php/${current.version}/extensions`, { name: ext.name, install: !ext.installed });
      toast(`${ext.name} ${ext.installed ? "removed from" : "installed for"} PHP ${current.version}`);
      await reload();
    } catch (e: any) {
      toast(e.message, "error");
    } finally {
      setBusy("");
    }
  };

  return (
    <Page title="PHP" subtitle="Install or remove extensions for each PHP version. PHP-FPM restarts after every change.">
      <div className="toolbar">
        <div className="tabs" role="tablist">
          {data.versions.map((v) => (
            <button key={v.version} role="tab" aria-selected={v.version === current.version} className={v.version === current.version ? "active" : ""} onClick={() => setTab(v.version)}>
              PHP {v.version}
            </button>
          ))}
        </div>
        <input type="search" placeholder="Filter extensions" value={filter} onChange={(e) => setFilter(e.target.value)} aria-label="Filter extensions" />
      </div>
      <p className="muted small">{installed} of {current.extensions.length} available extensions installed.</p>
      {shown.length === 0 ? (
        <Empty>No extension matches “{filter}”.</Empty>
      ) : (
        <div className="ext-grid">
          {shown.map((e) => (
            <div key={e.name} className={`card ext${e.installed ? " on" : ""}`}>
              <div>
                <strong>{e.name}</strong>
                <div className="muted small">{e.installed ? "Installed" : "Not installed"}</div>
              </div>
              <button className={e.installed ? "ghost small" : "primary small"} disabled={busy !== ""} onClick={() => toggle(e)}>
                {busy === e.name ? "Working…" : e.installed ? "Remove" : "Install"}
              </button>
            </div>
          ))}
        </div>
      )}
    </Page>
  );
}
