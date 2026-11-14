import { FormEvent, useEffect, useState } from "react";
import { api } from "../api";
import { Field, Loading, Page, useLoad, useToast } from "../ui";

export default function Settings() {
  const { data, error } = useLoad<{ admin_email: string; server_ip: string }>("/settings");
  const [email, setEmail] = useState("");
  const [ip, setIp] = useState("");
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const toast = useToast();

  useEffect(() => {
    if (data) {
      setEmail(data.admin_email);
      setIp(data.server_ip);
    }
  }, [data]);

  const save = async (e: FormEvent) => {
    e.preventDefault();
    try {
      await api("PUT", "/settings", { admin_email: email, server_ip: ip });
      toast("Settings saved");
    } catch (err: any) {
      toast(err.message, "error");
    }
  };
  const changePassword = async (e: FormEvent) => {
    e.preventDefault();
    if (next !== confirm) return toast("The new passwords do not match", "error");
    try {
      await api("POST", "/password", { current, new: next });
      window.dispatchEvent(new Event("hostdeck:signed-out"));
    } catch (err: any) {
      toast(err.message, "error");
    }
  };

  if (!data) return <Page title="Settings"><Loading error={error} /></Page>;
  return (
    <Page title="Settings">
      <div className="grid two">
        <form className="card form" onSubmit={save}>
          <h3>Server</h3>
          <Field label="Admin email" hint="Let's Encrypt sends certificate expiry notices here.">
            <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="you@example.com" />
          </Field>
          <Field label="Server IPv4 address" hint="Default address for new DNS zones.">
            <input value={ip} onChange={(e) => setIp(e.target.value)} placeholder="203.0.113.10" spellCheck={false} />
          </Field>
          <div><button className="primary">Save settings</button></div>
        </form>
        <form className="card form" onSubmit={changePassword}>
          <h3>Admin password</h3>
          <Field label="Current password">
            <input type="password" value={current} onChange={(e) => setCurrent(e.target.value)} required autoComplete="current-password" />
          </Field>
          <Field label="New password" hint="At least 8 characters. You will be signed out everywhere.">
            <input type="password" value={next} onChange={(e) => setNext(e.target.value)} required minLength={8} autoComplete="new-password" />
          </Field>
          <Field label="Repeat new password">
            <input type="password" value={confirm} onChange={(e) => setConfirm(e.target.value)} required minLength={8} autoComplete="new-password" />
          </Field>
          <div><button className="primary">Change password</button></div>
        </form>
      </div>
    </Page>
  );
}
