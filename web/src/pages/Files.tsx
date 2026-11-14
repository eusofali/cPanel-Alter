import { useRef, useState } from "react";
import { api, q } from "../api";
import { Empty, Field, fmtBytes, fmtDate, FormModal, Loading, Modal, Page, useLoad, useToast } from "../ui";

type Entry = { name: string; dir: boolean; size: number; modified: number; mode: string };
type Listing = { path: string; entries: Entry[] };

const join = (dir: string, name: string) => (dir === "/" ? "/" + name : dir + "/" + name);

function Editor({ path, onClose }: { path: string; onClose: () => void }) {
  const { data, error } = useLoad<{ content: string }>(`/files/content?path=${q(path)}`);
  const [text, setText] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const toast = useToast();
  const value = text ?? data?.content ?? "";
  const save = async () => {
    setSaving(true);
    try {
      await api("PUT", "/files/content", { path, content: value });
      toast("File saved");
      onClose();
    } catch (e: any) {
      toast(e.message, "error");
      setSaving(false);
    }
  };
  return (
    <Modal title={path} onClose={onClose} wide>
      <div className="modal-body">
        {!data ? <Loading error={error} /> : (
          <textarea className="editor" value={value} onChange={(e) => setText(e.target.value)} spellCheck={false} aria-label="File contents" />
        )}
      </div>
      <footer>
        <button className="ghost" onClick={onClose}>Close</button>
        <button className="primary" onClick={save} disabled={!data || saving}>{saving ? "Saving…" : "Save"}</button>
      </footer>
    </Modal>
  );
}

export default function Files() {
  const [path, setPath] = useState(() => {
    const start = sessionStorage.getItem("hmea-panel:path") || "/";
    sessionStorage.removeItem("hmea-panel:path");
    return start;
  });
  const { data, error, reload } = useLoad<Listing>(`/files?path=${q(path)}`);
  const [editing, setEditing] = useState("");
  const [modal, setModal] = useState<null | { kind: "mkdir" | "newfile" } | { kind: "rename" | "delete"; entry: Entry }>(null);
  const [name, setName] = useState("");
  const [uploading, setUploading] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const toast = useToast();
  const close = () => setModal(null);

  const parts = path.split("/").filter(Boolean);
  const upload = async (files: FileList | null) => {
    if (!files?.length) return;
    const form = new FormData();
    Array.from(files).forEach((f) => form.append("file", f));
    setUploading(true);
    try {
      const r = await api<{ uploaded: number }>("POST", `/files/upload?path=${q(path)}`, form);
      toast(`${r.uploaded} file${r.uploaded === 1 ? "" : "s"} uploaded`);
      reload();
    } catch (e: any) {
      toast(e.message, "error");
    } finally {
      setUploading(false);
      if (input.current) input.current.value = "";
    }
  };

  return (
    <Page title="Files" subtitle="Everything under /var/www."
      actions={<>
        <button className="ghost" onClick={() => { setName(""); setModal({ kind: "newfile" }); }}>New file</button>
        <button className="ghost" onClick={() => { setName(""); setModal({ kind: "mkdir" }); }}>New folder</button>
        <button className="primary" onClick={() => input.current?.click()} disabled={uploading}>{uploading ? "Uploading…" : "Upload"}</button>
        <input ref={input} type="file" multiple hidden onChange={(e) => upload(e.target.files)} />
      </>}>
      <nav className="crumbs" aria-label="Path">
        <button className="link" onClick={() => setPath("/")}>/var/www</button>
        {parts.map((p, i) => (
          <span key={i}> / <button className="link" onClick={() => setPath("/" + parts.slice(0, i + 1).join("/"))}>{p}</button></span>
        ))}
      </nav>
      {!data ? <Loading error={error} /> : data.entries.length === 0 ? <Empty>This folder is empty.</Empty> : (
        <div className="card table-wrap">
          <table>
            <thead><tr><th>Name</th><th>Size</th><th>Modified</th><th>Mode</th><th /></tr></thead>
            <tbody>
              {data.entries.map((e) => (
                <tr key={e.name}>
                  <td>
                    <button className="link file" onClick={() => (e.dir ? setPath(join(path, e.name)) : setEditing(join(path, e.name)))}>
                      <span aria-hidden="true">{e.dir ? "📁" : "📄"}</span> {e.name}
                    </button>
                  </td>
                  <td>{e.dir ? "–" : fmtBytes(e.size)}</td>
                  <td className="nowrap">{fmtDate(e.modified)}</td>
                  <td className="mono small">{e.mode}</td>
                  <td className="right nowrap">
                    {!e.dir && <a className="btn ghost small" href={`/api/files/download?path=${q(join(path, e.name))}`}>Download</a>}
                    <button className="ghost small" onClick={() => { setName(e.name); setModal({ kind: "rename", entry: e }); }}>Rename</button>
                    <button className="ghost small danger-text" onClick={() => setModal({ kind: "delete", entry: e })}>Delete</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {editing && <Editor path={editing} onClose={() => { setEditing(""); reload(); }} />}
      {(modal?.kind === "mkdir" || modal?.kind === "newfile") && (
        <FormModal title={modal.kind === "mkdir" ? "New folder" : "New file"} submit="Create" onClose={close}
          action={async () => {
            if (modal.kind === "mkdir") await api("POST", "/files/mkdir", { path, name });
            else await api("PUT", "/files/content", { path: join(path, name), content: "" });
            reload();
          }}>
          <Field label="Name">
            <input value={name} onChange={(e) => setName(e.target.value)} required autoFocus spellCheck={false} pattern="[^/\\]+" />
          </Field>
        </FormModal>
      )}
      {modal?.kind === "rename" && (
        <FormModal title={`Rename ${modal.entry.name}`} submit="Rename" onClose={close}
          action={async () => { await api("POST", "/files/rename", { path: join(path, modal.entry.name), name }); reload(); }}>
          <Field label="New name">
            <input value={name} onChange={(e) => setName(e.target.value)} required autoFocus spellCheck={false} pattern="[^/\\]+" />
          </Field>
        </FormModal>
      )}
      {modal?.kind === "delete" && (
        <FormModal title={`Delete ${modal.entry.name}?`} submit="Delete" danger done="Deleted" onClose={close}
          action={async () => { await api("DELETE", `/files?path=${q(join(path, modal.entry.name))}`); reload(); }}>
          <p>{modal.entry.dir ? "The folder and everything inside it are" : "The file is"} permanently removed. This cannot be undone.</p>
        </FormModal>
      )}
    </Page>
  );
}
