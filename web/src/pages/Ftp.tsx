import { useState } from "react";
import { api } from "../api";
import { Empty, Field, FormModal, Loading, Page, PasswordInput, useLoad } from "../ui";

type Account = { id: number; username: string; home: string };

function AccountForm({ account, onClose, onSaved }: { account?: Account; onClose: () => void; onSaved: () => void }) {
  const sites = useLoad<{ sites: { domain: string }[] }>("/sites");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [home, setHome] = useState(account?.home || "");
  return (
    <FormModal title={account ? `Edit ${account.username}` : "Add FTP account"} submit={account ? "Save changes" : "Create account"} done={account ? "Account updated" : "Account created"} onClose={onClose}
      action={async () => {
        if (account) await api("PUT", `/ftp/${account.id}`, { password, home });
        else await api("POST", "/ftp", { username, password, home });
        onSaved();
      }}>
      {!account && (
        <Field label="Username">
          <input value={username} onChange={(e) => setUsername(e.target.value)} required autoFocus spellCheck={false} />
        </Field>
      )}
      <Field label={account ? "New password" : "Password"} hint={account ? "Leave empty to keep the current password." : "Copy it now; it is not shown again."}>
        <PasswordInput value={password} onChange={setPassword} required={!account} />
      </Field>
      <Field label="Home folder" hint="Relative to /var/www. The account cannot leave this folder.">
        <input value={home} onChange={(e) => setHome(e.target.value)} required list="ftp-homes" placeholder="/example.com" spellCheck={false} />
        <datalist id="ftp-homes">
          {sites.data?.sites.flatMap((s) => [<option key={s.domain} value={`/${s.domain}`} />, <option key={s.domain + "p"} value={`/${s.domain}/public_html`} />])}
        </datalist>
      </Field>
    </FormModal>
  );
}

export default function Ftp() {
  const { data, error, reload } = useLoad<{ accounts: Account[] }>("/ftp");
  const [modal, setModal] = useState<null | { kind: "form"; account?: Account } | { kind: "delete"; account: Account }>(null);
  return (
    <Page title="FTP" subtitle="FTP accounts, each locked to one folder. Connect with explicit TLS (FTPES) on port 21."
      actions={<button className="primary" onClick={() => setModal({ kind: "form" })}>Add account</button>}>
      {!data ? <Loading error={error} /> : data.accounts.length === 0 ? <Empty>No FTP accounts yet.</Empty> : (
        <div className="card table-wrap">
          <table>
            <thead><tr><th>Username</th><th>Home folder</th><th /></tr></thead>
            <tbody>
              {data.accounts.map((a) => (
                <tr key={a.id}>
                  <td><strong className="mono">{a.username}</strong></td>
                  <td className="mono small">/var/www{a.home}</td>
                  <td className="right nowrap">
                    <button className="ghost small" onClick={() => setModal({ kind: "form", account: a })}>Edit</button>
                    <button className="ghost small danger-text" onClick={() => setModal({ kind: "delete", account: a })}>Delete</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {modal?.kind === "form" && <AccountForm account={modal.account} onClose={() => setModal(null)} onSaved={reload} />}
      {modal?.kind === "delete" && (
        <FormModal title={`Delete ${modal.account.username}?`} submit="Delete account" danger done="Account deleted" onClose={() => setModal(null)}
          action={async () => { await api("DELETE", `/ftp/${modal.account.id}`); reload(); }}>
          <p>The login stops working. Files in its home folder are kept.</p>
        </FormModal>
      )}
    </Page>
  );
}
