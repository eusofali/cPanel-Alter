import { useState } from "react";
import { api } from "../api";
import { Empty, Field, fmtBytes, FormModal, Loading, Page, PasswordInput, useLoad } from "../ui";

type DB = { name: string; users: string[]; size: number };
type User = { name: string; databases: string[] };
type Data = { databases: DB[]; users: User[] };

function UserForm({ user, dbs, onClose, onSaved }: { user?: User; dbs: DB[]; onClose: () => void; onSaved: () => void }) {
  const [name, setName] = useState(user?.name || "");
  const [password, setPassword] = useState("");
  const [granted, setGranted] = useState<string[]>(user?.databases || []);
  const flip = (db: string) => setGranted((g) => (g.includes(db) ? g.filter((x) => x !== db) : [...g, db]));
  return (
    <FormModal title={user ? `Edit user ${user.name}` : "Add database user"} submit={user ? "Save changes" : "Create user"} done={user ? "User updated" : "User created"} onClose={onClose}
      action={async () => {
        if (user) await api("PUT", `/dbusers/${user.name}`, { password, databases: granted });
        else await api("POST", "/dbusers", { name, password, databases: granted });
        onSaved();
      }}>
      {!user && (
        <Field label="User name">
          <input value={name} onChange={(e) => setName(e.target.value)} required autoFocus spellCheck={false} maxLength={32} />
        </Field>
      )}
      <Field label={user ? "New password" : "Password"} hint={user ? "Leave empty to keep the current password." : "Copy it now; it is not shown again."}>
        <PasswordInput value={password} onChange={setPassword} required={!user} />
      </Field>
      <fieldset>
        <legend>Full access to</legend>
        {dbs.length === 0 && <p className="muted small">Create a database first.</p>}
        {dbs.map((d) => (
          <label key={d.name} className="check">
            <input type="checkbox" checked={granted.includes(d.name)} onChange={() => flip(d.name)} /> {d.name}
          </label>
        ))}
      </fieldset>
    </FormModal>
  );
}

export default function Databases() {
  const { data, error, reload } = useLoad<Data>("/databases");
  const [modal, setModal] = useState<null | { kind: "db" } | { kind: "user"; user?: User } | { kind: "dropdb"; name: string } | { kind: "dropuser"; name: string }>(null);
  const [dbName, setDbName] = useState("");
  const close = () => setModal(null);

  return (
    <Page title="Databases" subtitle="MariaDB databases and the users that can access them."
      actions={<>
        <button className="ghost" onClick={() => setModal({ kind: "user" })}>Add user</button>
        <button className="primary" onClick={() => { setDbName(""); setModal({ kind: "db" }); }}>Add database</button>
      </>}>
      {!data ? <Loading error={error} /> : (
        <>
          <h3>Databases</h3>
          {data.databases.length === 0 ? <Empty>No databases yet.</Empty> : (
            <div className="card table-wrap">
              <table>
                <thead><tr><th>Name</th><th>Users</th><th>Size</th><th /></tr></thead>
                <tbody>
                  {data.databases.map((d) => (
                    <tr key={d.name}>
                      <td><strong className="mono">{d.name}</strong></td>
                      <td>{d.users.join(", ") || <span className="muted">none</span>}</td>
                      <td>{fmtBytes(d.size)}</td>
                      <td className="right"><button className="ghost small danger-text" onClick={() => setModal({ kind: "dropdb", name: d.name })}>Delete</button></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <h3>Users</h3>
          {data.users.length === 0 ? <Empty>No database users yet.</Empty> : (
            <div className="card table-wrap">
              <table>
                <thead><tr><th>User</th><th>Host</th><th>Databases</th><th /></tr></thead>
                <tbody>
                  {data.users.map((u) => (
                    <tr key={u.name}>
                      <td><strong className="mono">{u.name}</strong></td>
                      <td className="muted">localhost</td>
                      <td>{u.databases.join(", ") || <span className="muted">none</span>}</td>
                      <td className="right nowrap">
                        <button className="ghost small" onClick={() => setModal({ kind: "user", user: u })}>Edit</button>
                        <button className="ghost small danger-text" onClick={() => setModal({ kind: "dropuser", name: u.name })}>Delete</button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}
      {modal?.kind === "db" && (
        <FormModal title="Add database" submit="Create database" done="Database created" onClose={close} action={async () => { await api("POST", "/databases", { name: dbName }); reload(); }}>
          <Field label="Database name" hint="Letters, digits and underscores.">
            <input value={dbName} onChange={(e) => setDbName(e.target.value)} required autoFocus spellCheck={false} maxLength={64} />
          </Field>
        </FormModal>
      )}
      {modal?.kind === "user" && data && <UserForm user={modal.user} dbs={data.databases} onClose={close} onSaved={reload} />}
      {modal?.kind === "dropdb" && (
        <FormModal title={`Delete database ${modal.name}?`} submit="Delete database" danger done="Database deleted" onClose={close} action={async () => { await api("DELETE", `/databases/${modal.name}`); reload(); }}>
          <p>All tables and data in <strong className="mono">{modal.name}</strong> are permanently removed. This cannot be undone.</p>
        </FormModal>
      )}
      {modal?.kind === "dropuser" && (
        <FormModal title={`Delete user ${modal.name}?`} submit="Delete user" danger done="User deleted" onClose={close} action={async () => { await api("DELETE", `/dbusers/${modal.name}`); reload(); }}>
          <p>Applications using this login will lose access to their databases.</p>
        </FormModal>
      )}
    </Page>
  );
}
