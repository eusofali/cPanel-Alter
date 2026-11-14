import { useState } from "react";
import { api } from "../api";
import { Empty, Field, FormModal, Loading, Page, PasswordInput, useLoad } from "../ui";

type Mailbox = { id: number; address: string; local: string; quota_mb: number };
type Alias = { id: number; source: string; destination: string };
type Domain = { id: number; domain: string; mailboxes: Mailbox[]; aliases: Alias[] };

type ModalState =
  | null
  | { kind: "domain" }
  | { kind: "mailbox"; domain: Domain; box?: Mailbox }
  | { kind: "alias"; domain: Domain }
  | { kind: "delete"; title: string; text: string; path: string };

function MailboxForm({ domain, box, onClose, onSaved }: { domain: Domain; box?: Mailbox; onClose: () => void; onSaved: () => void }) {
  const [local, setLocal] = useState("");
  const [password, setPassword] = useState("");
  const [quota, setQuota] = useState(box?.quota_mb || 1024);
  return (
    <FormModal title={box ? `Edit ${box.address}` : `Add mailbox at ${domain.domain}`} submit={box ? "Save changes" : "Create mailbox"} done={box ? "Mailbox updated" : "Mailbox created"} onClose={onClose}
      action={async () => {
        if (box) await api("PUT", `/mail/mailboxes/${box.id}`, { password, quota_mb: quota });
        else await api("POST", "/mail/mailboxes", { domain_id: domain.id, local, password, quota_mb: quota });
        onSaved();
      }}>
      {!box && (
        <Field label="Address">
          <div className="row">
            <input value={local} onChange={(e) => setLocal(e.target.value)} required autoFocus spellCheck={false} placeholder="info" />
            <span className="suffix">@{domain.domain}</span>
          </div>
        </Field>
      )}
      <Field label={box ? "New password" : "Password"} hint={box ? "Leave empty to keep the current password." : "Copy it now; it is not shown again."}>
        <PasswordInput value={password} onChange={setPassword} required={!box} />
      </Field>
      <Field label="Quota (MB)">
        <input type="number" min={10} value={quota} onChange={(e) => setQuota(Number(e.target.value))} required />
      </Field>
    </FormModal>
  );
}

export default function Mail() {
  const { data, error, reload } = useLoad<{ domains: Domain[] }>("/mail");
  const [modal, setModal] = useState<ModalState>(null);
  const [domain, setDomain] = useState("");
  const [source, setSource] = useState("");
  const [dest, setDest] = useState("");
  const close = () => setModal(null);

  return (
    <Page title="Email" subtitle="Mailboxes and forwarders, delivered by Postfix and Dovecot (IMAP 993, SMTP 587)."
      actions={<button className="primary" onClick={() => { setDomain(""); setModal({ kind: "domain" }); }}>Add mail domain</button>}>
      {!data ? <Loading error={error} /> : data.domains.length === 0 ? <Empty>No mail domains yet. Add a domain, then create mailboxes for it.</Empty> : (
        data.domains.map((d) => (
          <div key={d.id} className="card section">
            <div className="section-head">
              <h3>{d.domain}</h3>
              <div className="actions">
                <button className="ghost small" onClick={() => setModal({ kind: "mailbox", domain: d })}>Add mailbox</button>
                <button className="ghost small" onClick={() => { setSource(""); setDest(""); setModal({ kind: "alias", domain: d }); }}>Add forwarder</button>
                <button className="ghost small danger-text" onClick={() => setModal({ kind: "delete", title: `Delete mail domain ${d.domain}?`, text: "All its mailboxes and forwarders stop receiving mail. Stored messages stay on disk.", path: `/mail/domains/${d.id}` })}>Delete domain</button>
              </div>
            </div>
            {d.mailboxes.length + d.aliases.length === 0 ? <p className="muted">No mailboxes or forwarders yet.</p> : (
              <div className="table-wrap">
                <table>
                  <thead><tr><th>Address</th><th>Kind</th><th>Details</th><th /></tr></thead>
                  <tbody>
                    {d.mailboxes.map((m) => (
                      <tr key={"m" + m.id}>
                        <td><strong>{m.address}</strong></td>
                        <td>Mailbox</td>
                        <td>{m.quota_mb} MB quota</td>
                        <td className="right nowrap">
                          <button className="ghost small" onClick={() => setModal({ kind: "mailbox", domain: d, box: m })}>Edit</button>
                          <button className="ghost small danger-text" onClick={() => setModal({ kind: "delete", title: `Delete ${m.address}?`, text: "The mailbox can no longer sign in or receive mail. Stored messages stay on disk.", path: `/mail/mailboxes/${m.id}` })}>Delete</button>
                        </td>
                      </tr>
                    ))}
                    {d.aliases.map((a) => (
                      <tr key={"a" + a.id}>
                        <td><strong>{a.source === "*" ? `(anything)@${d.domain}` : `${a.source}@${d.domain}`}</strong></td>
                        <td>{a.source === "*" ? "Catch-all" : "Forwarder"}</td>
                        <td>→ {a.destination.split(",").join(", ")}</td>
                        <td className="right">
                          <button className="ghost small danger-text" onClick={() => setModal({ kind: "delete", title: "Delete forwarder?", text: "Mail to this address will no longer be forwarded.", path: `/mail/aliases/${a.id}` })}>Delete</button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        ))
      )}
      {modal?.kind === "domain" && (
        <FormModal title="Add mail domain" submit="Add domain" done="Mail domain added" onClose={close} action={async () => { await api("POST", "/mail/domains", { domain }); reload(); }}>
          <Field label="Domain" hint="Its MX record must point to this server.">
            <input value={domain} onChange={(e) => setDomain(e.target.value)} required autoFocus spellCheck={false} placeholder="example.com" />
          </Field>
        </FormModal>
      )}
      {modal?.kind === "mailbox" && <MailboxForm domain={modal.domain} box={modal.box} onClose={close} onSaved={reload} />}
      {modal?.kind === "alias" && (
        <FormModal title={`Add forwarder at ${modal.domain.domain}`} submit="Create forwarder" done="Forwarder created" onClose={close}
          action={async () => { await api("POST", "/mail/aliases", { domain_id: modal.domain.id, source, destination: dest }); reload(); }}>
          <Field label="Address" hint="Use * to catch every address without its own mailbox.">
            <div className="row">
              <input value={source} onChange={(e) => setSource(e.target.value)} required autoFocus spellCheck={false} placeholder="sales" />
              <span className="suffix">@{modal.domain.domain}</span>
            </div>
          </Field>
          <Field label="Forward to" hint="One or more full addresses, separated by commas.">
            <input value={dest} onChange={(e) => setDest(e.target.value)} required spellCheck={false} placeholder="someone@example.org" />
          </Field>
        </FormModal>
      )}
      {modal?.kind === "delete" && (
        <FormModal title={modal.title} submit="Delete" danger done="Deleted" onClose={close} action={async () => { await api("DELETE", modal.path); reload(); }}>
          <p>{modal.text}</p>
        </FormModal>
      )}
    </Page>
  );
}
