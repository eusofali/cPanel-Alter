import { useState } from "react";
import { api } from "../api";
import { Badge, Empty, Field, FormModal, Loading, Page, useLoad, useToast } from "../ui";

type Job = { id: number; schedule: string; command: string; comment: string; enabled: boolean };

const PRESETS: [string, string][] = [
  ["*/5 * * * *", "Every 5 minutes"],
  ["*/15 * * * *", "Every 15 minutes"],
  ["0 * * * *", "Every hour"],
  ["0 3 * * *", "Every day at 03:00"],
  ["0 3 * * 0", "Every Sunday at 03:00"],
  ["0 3 1 * *", "First day of each month at 03:00"],
];
const describe = (s: string) => PRESETS.find((p) => p[0] === s)?.[1] || s;

function JobForm({ job, onClose, onSaved }: { job?: Job; onClose: () => void; onSaved: () => void }) {
  const [schedule, setSchedule] = useState(job?.schedule || PRESETS[2][0]);
  const [command, setCommand] = useState(job?.command || "");
  const [comment, setComment] = useState(job?.comment || "");
  const custom = !PRESETS.some((p) => p[0] === schedule);
  return (
    <FormModal title={job ? "Edit cron job" : "Add cron job"} submit={job ? "Save changes" : "Add job"} done={job ? "Cron job updated" : "Cron job added"} onClose={onClose}
      action={async () => {
        const body = { schedule, command, comment, enabled: job?.enabled ?? true };
        if (job) await api("PUT", `/cron/${job.id}`, body);
        else await api("POST", "/cron", body);
        onSaved();
      }}>
      <Field label="Description">
        <input value={comment} onChange={(e) => setComment(e.target.value)} placeholder="WordPress scheduled tasks" autoFocus />
      </Field>
      <Field label="How often">
        <select value={custom ? "custom" : schedule} onChange={(e) => setSchedule(e.target.value === "custom" ? "30 2 * * 1-5" : e.target.value)}>
          {PRESETS.map(([v, l]) => <option key={v} value={v}>{l}</option>)}
          <option value="custom">Custom schedule…</option>
        </select>
      </Field>
      {custom && (
        <Field label="Cron expression" hint="minute hour day-of-month month day-of-week">
          <input className="mono" value={schedule} onChange={(e) => setSchedule(e.target.value)} required spellCheck={false} />
        </Field>
      )}
      <Field label="Command" hint="Runs as the web user (www-data). Example: php /var/www/example.com/public_html/wp-cron.php">
        <input className="mono" value={command} onChange={(e) => setCommand(e.target.value)} required spellCheck={false} />
      </Field>
    </FormModal>
  );
}

export default function Cron() {
  const { data, error, reload } = useLoad<{ jobs: Job[] }>("/cron");
  const [modal, setModal] = useState<null | { kind: "form"; job?: Job } | { kind: "delete"; job: Job }>(null);
  const toast = useToast();
  const toggle = async (j: Job) => {
    try {
      await api("PUT", `/cron/${j.id}`, { ...j, enabled: !j.enabled });
      toast(j.enabled ? "Cron job paused" : "Cron job resumed");
      reload();
    } catch (e: any) {
      toast(e.message, "error");
    }
  };
  return (
    <Page title="Cron Jobs" subtitle="Run commands automatically on a schedule." actions={<button className="primary" onClick={() => setModal({ kind: "form" })}>Add cron job</button>}>
      {!data ? <Loading error={error} /> : data.jobs.length === 0 ? <Empty>No cron jobs yet.</Empty> : (
        <div className="card table-wrap">
          <table>
            <thead><tr><th>Job</th><th>Schedule</th><th>Status</th><th /></tr></thead>
            <tbody>
              {data.jobs.map((j) => (
                <tr key={j.id}>
                  <td>
                    {j.comment && <strong>{j.comment}</strong>}
                    <div className="mono small wrap">{j.command}</div>
                  </td>
                  <td>{describe(j.schedule)}{describe(j.schedule) !== j.schedule && <div className="muted small mono">{j.schedule}</div>}</td>
                  <td><Badge kind={j.enabled ? "ok" : "off"}>{j.enabled ? "Active" : "Paused"}</Badge></td>
                  <td className="right nowrap">
                    <button className="ghost small" onClick={() => setModal({ kind: "form", job: j })}>Edit</button>
                    <button className="ghost small" onClick={() => toggle(j)}>{j.enabled ? "Pause" : "Resume"}</button>
                    <button className="ghost small danger-text" onClick={() => setModal({ kind: "delete", job: j })}>Delete</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {modal?.kind === "form" && <JobForm job={modal.job} onClose={() => setModal(null)} onSaved={reload} />}
      {modal?.kind === "delete" && (
        <FormModal title="Delete cron job?" submit="Delete job" danger done="Cron job deleted" onClose={() => setModal(null)}
          action={async () => { await api("DELETE", `/cron/${modal.job.id}`); reload(); }}>
          <p className="mono small wrap">{modal.job.command}</p>
        </FormModal>
      )}
    </Page>
  );
}
