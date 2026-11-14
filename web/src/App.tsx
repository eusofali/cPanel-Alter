import { useEffect, useState } from "react";
import { api } from "./api";
import { ToastProvider } from "./ui";
import Dashboard from "./pages/Dashboard";
import Sites from "./pages/Sites";
import Php from "./pages/Php";
import Databases from "./pages/Databases";
import Files from "./pages/Files";
import Ftp from "./pages/Ftp";
import Mail from "./pages/Mail";
import Dns from "./pages/Dns";
import Settings from "./pages/Settings";

const pages = [
  { id: "dashboard", label: "Dashboard", el: <Dashboard /> },
  { id: "sites", label: "Websites", el: <Sites /> },
  { id: "php", label: "PHP", el: <Php /> },
  { id: "databases", label: "Databases", el: <Databases /> },
  { id: "files", label: "Files", el: <Files /> },
  { id: "ftp", label: "FTP", el: <Ftp /> },
  { id: "mail", label: "Email", el: <Mail /> },
  { id: "dns", label: "DNS", el: <Dns /> },
  { id: "settings", label: "Settings", el: <Settings /> },
];

type Me = { authenticated: boolean; hostname: string; dev: boolean };

function Login({ onDone }: { onDone: () => void }) {
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  return (
    <main className="login">
      <form
        onSubmit={async (e) => {
          e.preventDefault();
          setBusy(true);
          try {
            await api("POST", "/login", { password });
            onDone();
          } catch (err: any) {
            setError(err.message);
            setBusy(false);
          }
        }}
      >
        <div className="brand">hostdeck</div>
        <p className="muted">Sign in to manage this server.</p>
        <label className="field">
          <span>Admin password</span>
          <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoFocus autoComplete="current-password" required />
        </label>
        {error && <p className="error" role="alert">{error}</p>}
        <button className="primary" disabled={busy}>{busy ? "Signing in…" : "Sign in"}</button>
      </form>
    </main>
  );
}

export default function App() {
  const [me, setMe] = useState<Me | null>(null);
  const [route, setRoute] = useState(() => location.hash.slice(2) || "dashboard");
  const [menu, setMenu] = useState(false);

  const refresh = () => api<Me>("GET", "/me").then(setMe).catch(() => setMe({ authenticated: false, hostname: "", dev: false }));
  useEffect(() => {
    refresh();
    const onHash = () => {
      setRoute(location.hash.slice(2) || "dashboard");
      setMenu(false);
    };
    const onOut = () => setMe((m) => (m ? { ...m, authenticated: false } : m));
    window.addEventListener("hashchange", onHash);
    window.addEventListener("hostdeck:signed-out", onOut);
    return () => {
      window.removeEventListener("hashchange", onHash);
      window.removeEventListener("hostdeck:signed-out", onOut);
    };
  }, []);

  if (!me) return null;
  if (!me.authenticated) return <Login onDone={refresh} />;
  const page = pages.find((p) => p.id === route) || pages[0];

  return (
    <ToastProvider>
      <div className="shell">
        <aside className={menu ? "open" : ""}>
          <div className="brand">hostdeck</div>
          <div className="host" title={me.hostname}>{me.hostname}</div>
          <nav>
            {pages.map((p) => (
              <a key={p.id} href={`#/${p.id}`} className={p.id === page.id ? "active" : ""}>{p.label}</a>
            ))}
          </nav>
          <button
            className="ghost signout"
            onClick={async () => {
              await api("POST", "/logout").catch(() => {});
              refresh();
            }}
          >
            Sign out
          </button>
        </aside>
        <div className="content">
          <div className="topbar">
            <button className="ghost" onClick={() => setMenu(!menu)} aria-label="Menu">☰</button>
            <span className="brand">hostdeck</span>
          </div>
          {me.dev && <div className="banner">Development mode: system changes are simulated, nothing on this machine is modified.</div>}
          <div key={page.id}>{page.el}</div>
        </div>
      </div>
    </ToastProvider>
  );
}
