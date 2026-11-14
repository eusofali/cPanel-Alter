import { useEffect, useState } from "react";
import { api } from "../api";
import { Badge, Empty, Field, fmtBytes, fmtDate, FormModal, Loading, Page, useLoad, useToast } from "../ui";

type Backup = { id: number; kind: "full" | "site" | "database"; target: string; file: string; size: number; created: number; status: string; note: string; auto: boolean };
type Data = { backups: Backup[]; sites: string[]; databases: string[]; schedule: string; keep: number; dir: string; free: number };

const KIND = { full: "Full server", site: "Website", database: "Database" };

function CreateForm({ data, onClose, onDone }: { data: Data; onClose: () => void; onDone: () => void }) {
  const [kind, setKind] = useState<Backup["kind"]>("full");
  const [target, setTarget] = useState("");
  const options = kind === "site" ? data.sites : data.databases;
  return (
    <FormModal title="Create backup" submit="Start backup" done="Backup started" onClose={onClose}
      action={async () => { await api("POST", "/backups", { kind, target: kind === "full" ? "" : target || options[0] || "" }); onDone(); }}>
      <Field label="What to back up">
        <select value={kind} onChange={(e) => { setKind(e.target.value as Backup["kind"]); setTarget(""); }}>
          <option value="full">Full server: all websites, databases and mailboxes</option>
          <option value="site">One website's files</option>
          <option value="database">One database</option>
        </select>
      </Field>
      {kind !== "full" && (
        <Field label={kind === "site" ? "Website" : "Database"}>
          {options.length === 0 ? <p className="muted">There is nothing to choose yet.</p> : (
            <select value={target || options[0]} onChange={(e) => setTarget(e.target.value)}>
              {options.map((o) => <option key={o}>{o}</option>)}
            </select>
          )}
        </Field>
      )}
      <p className="muted small">The backup runs in the background and is saved in <span className="mono">{data.dir}</span> ({fmtBytes(data.free)} free). Large sites can take several minutes.</p>
    </FormModal>
  );
}

function RestoreForm({ b, onClose, onDone }: { b: Backup; onClose: () => void; onDone: () => void }) {
  const [files, setFiles] = useState(true);
  const [databases, setDatabases] = useState(true);
  const [mail, setMail] = useState(false);
  const [sure, setSure] = useState(false);
  return (
    <FormModal title="Restore backup" submit="Restore" danger done="Restore started" onClose={onClose}
      action={async () => {
        if (!sure) throw new Error("Tick the box to confirm.");
        await api("POST", `/backups/${b.id}/restore`, { files, databases, mail });
        onDone();
      }}>
      <p>Restore <strong className="mono">{b.file}</strong> from {fmtDate(b.created)}.</p>
      {b.kind === "full" && (
        <fieldset>
          <legend>Restore</legend>
          <label className="check"><input type="checkbox" checked={files} onChange={(e) => setFiles(e.target.checked)} /> Website files (everything under /var/www)</label>
          <label className="check"><input type="checkbox" checked={databases} onChange={(e) => setDatabases(e.target.checked)} /> Databases</label>
          <label className="check"><input type="checkbox" checked={mail} onChange={(e) => setMail(e.target.checked)} /> Mailboxes</label>
        </fieldset>
      )}
      <p className="muted small">
        {b.kind === "database"
          ? "The database's current contents are replaced by the backup."
          : "Files in the backup overwrite the current ones. Files created after the backup are kept."}
        {b.kind === "full" && " Databases are replaced by their backed-up contents."}
      </p>
      <label className="check"><input type="checkbox" checked={sure} onChange={(e) => setSure(e.target.checked)} /> I understand that current data will be overwritten.</label>
    </FormModal>
  );
}

export default function Backups() {
  const { data, error, reload } = useLoad<Data>("/backups");
  const [modal, setModal] = useState<null | { kind: "create" } | { kind: "restore" | "delete"; b: Backup }>(null);
  const [schedule, setSchedule] = useState("off");
  const [keep, setKeep] = useState(7);
  const toast = useToast();
  const active = data?.backups.some((b) => b.status === "running" || b.status === "restoring");

  useEffect(() => {
    if (data) {
      setSchedule(data.schedule);
      setKeep(data.keep);
    }
  }, [data?.schedule, data?.keep]);
  useEffect(() => {
    if (!active) return;
    const t = setInterval(reload, 2500);
    return () => clearInterval(t);
  }, [active, reload]);

  const saveSchedule = async () => {
    try {
      await api("PUT", "/backups/schedule", { schedule, keep });
      toast("Backup schedule saved");
      reload();
    } catch (e: any) {
      toast(e.message, "error");
    }
  };

  return (
    <Page title="Backups" subtitle="Back up and restore websites, databases and mailboxes."
      actions={<button className="primary" onClick={() => setModal({ kind: "create" })} disabled={!data || active}>Create backup</button>}>
      {!data ? <Loading error={error} /> : (
        <>
          <div className="card schedule">
            <div>
              <h3>Automatic full backups</h3>
              <p className="muted small">Runs in the background and deletes the oldest automatic backups beyond the number you keep. Copy backups off this server regularly: a backup on the same disk does not survive a disk failure.</p>
            </div>
            <div className="schedule-form">
              <Field label="Frequency">
                <select value={schedule} onChange={(e) => setSchedule(e.target.value)}>
                  <option value="off">Off</option>
                  <option value="daily">Every day</option>
                  <option value="weekly">Every week</option>
                </select>
              </Field>
              <Field label="Keep">
                <input type="number" min={1} max={365} value={keep} onChange={(e) => setKeep(Number(e.target.value))} />
              </Field>
              <button className="ghost" onClick={saveSchedule}>Save</button>
            </div>
          </div>

          {data.backups.length === 0 ? <Empty>No backups yet. Create your first one before making big changes.</Empty> : (
            <div className="card table-wrap">
              <table>
                <thead><tr><th>Backup</th><th>Created</th><th>Size</th><th>Status</th><th /></tr></thead>
                <tbody>
                  {data.backups.map((b) => (
                    <tr key={b.id}>
                      <td>
                        <strong>{KIND[b.kind]}{b.target && `: ${b.target}`}</strong> {b.auto && <span className="badge">automatic</span>}
                        <div className="muted small mono">{b.file}</div>
                      </td>
                      <td className="nowrap">{fmtDate(b.created)}</td>
                      <td className="nowrap">{b.status === "running" ? "–" : fmtBytes(b.size)}</td>
                      <td>
                        <Badge kind={b.status === "done" ? "ok" : b.status === "failed" ? "warn" : "off"}>
                          {b.status === "running" ? "Backing up…" : b.status === "restoring" ? "Restoring…" : b.status === "done" ? "Ready" : "Failed"}
                        </Badge>
                        {b.note && <div className="muted small wrap">{b.note}</div>}
                      </td>
                      <td className="right nowrap">
                        {b.status === "done" && <a className="btn ghost small" href={`/api/backups/${b.id}/download`}>Download</a>}
                        {b.status === "done" && <button className="ghost small" disabled={active} onClick={() => setModal({ kind: "restore", b })}>Restore</button>}
                        {(b.status === "done" || b.status === "failed") && <button className="ghost small danger-text" onClick={() => setModal({ kind: "delete", b })}>Delete</button>}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <p className="muted small note">
            Moving to another server: download a backup, copy it into <span className="mono">{data.dir}</span> on the new server, and it appears here ready to restore.
          </p>
        </>
      )}
      {modal?.kind === "create" && data && <CreateForm data={data} onClose={() => setModal(null)} onDone={reload} />}
      {modal?.kind === "restore" && <RestoreForm b={modal.b} onClose={() => setModal(null)} onDone={reload} />}
      {modal?.kind === "delete" && (
        <FormModal title="Delete backup?" submit="Delete backup" danger done="Backup deleted" onClose={() => setModal(null)}
          action={async () => { await api("DELETE", `/backups/${modal.b.id}`); reload(); }}>
          <p><strong className="mono">{modal.b.file}</strong> is permanently removed from the server.</p>
        </FormModal>
      )}
    </Page>
  );
}
