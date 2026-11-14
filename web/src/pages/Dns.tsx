import { useState } from "react";
import { api } from "../api";
import { Empty, Field, FormModal, Loading, Page, useLoad } from "../ui";

type Zone = { id: number; domain: string; serial: number; records: number };
type Rec = { id: number; name: string; type: string; ttl: number; prio: number; value: string };
type ZoneDetail = { id: number; domain: string; serial: number; records: Rec[] };

const TYPES = ["A", "AAAA", "CNAME", "MX", "TXT", "NS", "SRV", "CAA"];
const HINTS: Record<string, string> = {
  A: "IPv4 address, e.g. 203.0.113.10",
  AAAA: "IPv6 address, e.g. 2001:db8::1",
  CNAME: "Full host name, e.g. example.com",
  MX: "Full host name of the mail server, e.g. mail.example.com",
  TXT: "Text, e.g. v=spf1 mx -all",
  NS: "Full host name of the name server",
  SRV: "weight port target, e.g. 5 5060 sip.example.com",
  CAA: 'flag tag "value", e.g. 0 issue "letsencrypt.org"',
};

function RecordForm({ zone, rec, onClose, onSaved }: { zone: ZoneDetail; rec?: Rec; onClose: () => void; onSaved: () => void }) {
  const [f, setF] = useState<Omit<Rec, "id">>(rec || { name: "", type: "A", ttl: 3600, prio: 10, value: "" });
  const set = (patch: Partial<Rec>) => setF((x) => ({ ...x, ...patch }));
  const hasPrio = f.type === "MX" || f.type === "SRV";
  return (
    <FormModal title={rec ? "Edit record" : `Add record to ${zone.domain}`} submit={rec ? "Save changes" : "Add record"} done={rec ? "Record updated" : "Record added"} onClose={onClose}
      action={async () => {
        if (rec) await api("PUT", `/dns/records/${rec.id}`, f);
        else await api("POST", `/dns/zones/${zone.id}/records`, f);
        onSaved();
      }}>
      <div className="cols">
        <Field label="Type">
          <select value={f.type} onChange={(e) => set({ type: e.target.value })}>{TYPES.map((t) => <option key={t}>{t}</option>)}</select>
        </Field>
        <Field label="TTL (seconds)">
          <input type="number" min={60} max={604800} value={f.ttl} onChange={(e) => set({ ttl: Number(e.target.value) })} required />
        </Field>
      </div>
      <Field label="Name" hint={`Use @ for ${zone.domain} itself, or a subdomain such as www.`}>
        <input value={f.name} onChange={(e) => set({ name: e.target.value })} placeholder="@" autoFocus spellCheck={false} />
      </Field>
      {hasPrio && (
        <Field label="Priority">
          <input type="number" min={0} max={65535} value={f.prio} onChange={(e) => set({ prio: Number(e.target.value) })} required />
        </Field>
      )}
      <Field label="Value" hint={HINTS[f.type]}>
        <input value={f.value} onChange={(e) => set({ value: e.target.value })} required spellCheck={false} />
      </Field>
    </FormModal>
  );
}

function ZoneView({ id, onBack }: { id: number; onBack: () => void }) {
  const { data, error, reload } = useLoad<ZoneDetail>(`/dns/zones/${id}`);
  const [modal, setModal] = useState<null | { kind: "form"; rec?: Rec } | { kind: "delete"; rec: Rec }>(null);
  return (
    <Page title={data?.domain || "Zone"} subtitle={data ? `Serial ${data.serial}` : undefined}
      actions={<>
        <button className="ghost" onClick={onBack}>← All zones</button>
        <button className="primary" onClick={() => setModal({ kind: "form" })} disabled={!data}>Add record</button>
      </>}>
      {!data ? <Loading error={error} /> : (
        <div className="card table-wrap">
          <table>
            <thead><tr><th>Name</th><th>Type</th><th>Value</th><th>TTL</th><th /></tr></thead>
            <tbody>
              {data.records.map((r) => (
                <tr key={r.id}>
                  <td className="mono">{r.name}</td>
                  <td><span className="badge">{r.type}</span></td>
                  <td className="mono small wrap">{r.type === "MX" || r.type === "SRV" ? `${r.prio} ` : ""}{r.value}</td>
                  <td>{r.ttl}</td>
                  <td className="right nowrap">
                    <button className="ghost small" onClick={() => setModal({ kind: "form", rec: r })}>Edit</button>
                    <button className="ghost small danger-text" onClick={() => setModal({ kind: "delete", rec: r })}>Delete</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {modal?.kind === "form" && data && <RecordForm zone={data} rec={modal.rec} onClose={() => setModal(null)} onSaved={reload} />}
      {modal?.kind === "delete" && (
        <FormModal title="Delete record?" submit="Delete record" danger done="Record deleted" onClose={() => setModal(null)}
          action={async () => { await api("DELETE", `/dns/records/${modal.rec.id}`); reload(); }}>
          <p><span className="mono">{modal.rec.name} {modal.rec.type} {modal.rec.value}</span> will stop resolving.</p>
        </FormModal>
      )}
    </Page>
  );
}

export default function Dns() {
  const { data, error, reload } = useLoad<{ zones: Zone[]; server_ip: string }>("/dns/zones");
  const [open, setOpen] = useState(0);
  const [modal, setModal] = useState<null | { kind: "add" } | { kind: "delete"; zone: Zone }>(null);
  const [domain, setDomain] = useState("");
  const [ip, setIp] = useState("");
  const [mail, setMail] = useState(true);

  if (open) return <ZoneView id={open} onBack={() => { setOpen(0); reload(); }} />;
  return (
    <Page title="DNS" subtitle="Zones served by BIND on this server. Point the domain's name servers here at your registrar."
      actions={<button className="primary" onClick={() => { setDomain(""); setIp(data?.server_ip || ""); setModal({ kind: "add" }); }}>Add zone</button>}>
      {!data ? <Loading error={error} /> : data.zones.length === 0 ? <Empty>No DNS zones yet.</Empty> : (
        <div className="card table-wrap">
          <table>
            <thead><tr><th>Zone</th><th>Records</th><th>Serial</th><th /></tr></thead>
            <tbody>
              {data.zones.map((z) => (
                <tr key={z.id}>
                  <td><button className="link" onClick={() => setOpen(z.id)}><strong>{z.domain}</strong></button></td>
                  <td>{z.records}</td>
                  <td className="mono small">{z.serial}</td>
                  <td className="right nowrap">
                    <button className="ghost small" onClick={() => setOpen(z.id)}>Manage records</button>
                    <button className="ghost small danger-text" onClick={() => setModal({ kind: "delete", zone: z })}>Delete</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {modal?.kind === "add" && (
        <FormModal title="Add DNS zone" submit="Create zone" done="Zone created" onClose={() => setModal(null)}
          action={async () => { const r = await api<{ id: number }>("POST", "/dns/zones", { domain, ip, mail }); reload(); setOpen(r.id); }}>
          <Field label="Domain">
            <input value={domain} onChange={(e) => setDomain(e.target.value)} required autoFocus spellCheck={false} placeholder="example.com" />
          </Field>
          <Field label="Server IPv4 address" hint="Used for the starter records. Set a default in Settings.">
            <input value={ip} onChange={(e) => setIp(e.target.value)} required spellCheck={false} placeholder="203.0.113.10" />
          </Field>
          <label className="check">
            <input type="checkbox" checked={mail} onChange={(e) => setMail(e.target.checked)} /> Add mail records (MX, mail host, SPF)
          </label>
        </FormModal>
      )}
      {modal?.kind === "delete" && (
        <FormModal title={`Delete zone ${modal.zone.domain}?`} submit="Delete zone" danger done="Zone deleted" onClose={() => setModal(null)}
          action={async () => { await api("DELETE", `/dns/zones/${modal.zone.id}`); reload(); }}>
          <p>All {modal.zone.records} records are removed and this server stops answering for the domain.</p>
        </FormModal>
      )}
    </Page>
  );
}
