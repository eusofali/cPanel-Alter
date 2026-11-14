import { FormEvent, useEffect, useState } from "react";
import { api } from "../api";
import qrcode from "qrcode-generator";
import { Badge, Field, FormModal, Loading, Page, useLoad, useToast } from "../ui";

function QR({ text }: { text: string }) {
  const qr = qrcode(0, "M");
  qr.addData(text);
  qr.make();
  const n = qr.getModuleCount();
  const cells: JSX.Element[] = [];
  for (let y = 0; y < n; y++) for (let x = 0; x < n; x++) if (qr.isDark(y, x)) cells.push(<rect key={`${x}-${y}`} x={x} y={y} width="1" height="1" />);
  return (
    <span className="qr">
      <svg width="176" height="176" viewBox={`0 0 ${n} ${n}`} shapeRendering="crispEdges" role="img" aria-label="QR code for your authenticator app" fill="#000">{cells}</svg>
    </span>
  );
}

function TwoFactor() {
  const { data, reload } = useLoad<{ enabled: boolean }>("/2fa");
  const [setup, setSetup] = useState<{ secret: string; uri: string } | null>(null);
  const [off, setOff] = useState(false);
  const [code, setCode] = useState("");
  const [password, setPassword] = useState("");
  const toast = useToast();
  const begin = async () => {
    try {
      setCode("");
      setSetup(await api("POST", "/2fa/setup"));
    } catch (e: any) {
      toast(e.message, "error");
    }
  };
  return (
    <div className="card form">
      <h3>Two-factor sign-in {data && <Badge kind={data.enabled ? "ok" : "off"}>{data.enabled ? "On" : "Off"}</Badge>}</h3>
      <p className="muted">Ask for a 6-digit code from an authenticator app (Google Authenticator, Authy, 1Password and others) in addition to the password. Strongly recommended for a panel reachable from the internet.</p>
      <div>
        {data?.enabled
          ? <button className="ghost" onClick={() => { setPassword(""); setOff(true); }}>Turn off two-factor sign-in</button>
          : <button className="primary" onClick={begin} disabled={!data}>Set up two-factor sign-in</button>}
      </div>
      {setup && (
        <FormModal title="Set up two-factor sign-in" submit="Turn on" done="Two-factor sign-in is on" onClose={() => setSetup(null)}
          action={async () => { await api("POST", "/2fa/enable", { code }); reload(); }}>
          <div className="twofa">
            <QR text={setup.uri} />
            <div>
              <p><strong>1.</strong> Scan this code with your authenticator app, or type this key into it:</p>
              <code className="secret">{setup.secret.replace(/(.{4})/g, "$1 ").trim()}</code>
              <p><strong>2.</strong> Enter the 6-digit code the app shows.</p>
            </div>
          </div>
          <Field label="Code from the app" hint="If you lose your phone, run “hmea-panel disable-2fa” on the server to turn this off.">
            <input value={code} onChange={(e) => setCode(e.target.value)} inputMode="numeric" autoComplete="one-time-code" maxLength={7} required autoFocus />
          </Field>
        </FormModal>
      )}
      {off && (
        <FormModal title="Turn off two-factor sign-in?" submit="Turn off" danger done="Two-factor sign-in is off" onClose={() => setOff(false)}
          action={async () => { await api("POST", "/2fa/disable", { password }); reload(); }}>
          <p>Signing in will only need the password again.</p>
          <Field label="Admin password">
            <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required autoFocus autoComplete="current-password" />
          </Field>
        </FormModal>
      )}
    </div>
  );
}

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
      window.dispatchEvent(new Event("hmea-panel:signed-out"));
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
        <TwoFactor />
      </div>
    </Page>
  );
}
