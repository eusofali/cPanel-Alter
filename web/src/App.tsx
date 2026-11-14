import { useEffect, useRef, useState } from "react";
import { api } from "./api";
import { Icon } from "./icons";
import { groups, searchTools } from "./tools";
import { ToastProvider } from "./ui";
import Home from "./pages/Home";
import Sites from "./pages/Sites";
import Php from "./pages/Php";
import Databases from "./pages/Databases";
import Files from "./pages/Files";
import Ftp from "./pages/Ftp";
import Mail from "./pages/Mail";
import Dns from "./pages/Dns";
import Server from "./pages/Server";
import Settings from "./pages/Settings";

const pages: Record<string, JSX.Element> = {
  home: <Home />, sites: <Sites />, php: <Php />, databases: <Databases />, files: <Files />,
  ftp: <Ftp />, mail: <Mail />, dns: <Dns />, server: <Server />, settings: <Settings />,
};

// One sidebar entry per page (several tools can share a page).
const seenRoutes = new Set<string>();
const navGroups = groups.map((g) => ({
  label: g.label,
  items: g.tools.filter((t) => !seenRoutes.has(t.route) && seenRoutes.add(t.route)),
}));

type Me = { authenticated: boolean; hostname: string; dev: boolean };
const currentRoute = () => location.hash.slice(2) || "home";

function Logo() {
  return (
    <a className="logo" href="#/home" aria-label="hostdeck home">
      <span className="logo-mark" aria-hidden="true"><Icon name="server" size={18} /></span>
      <span>host<b>deck</b></span>
    </a>
  );
}

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
        <div className="login-brand"><span className="logo-mark"><Icon name="server" size={22} /></span><span>host<b>deck</b></span></div>
        <h1>Sign in</h1>
        <p className="muted">Manage websites, email, databases and DNS on this server.</p>
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

function Search() {
  const [text, setText] = useState("");
  const [open, setOpen] = useState(false);
  const [index, setIndex] = useState(0);
  const input = useRef<HTMLInputElement>(null);
  const results = searchTools(text).slice(0, 8);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement;
      if (e.key === "/" && !/^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName)) {
        e.preventDefault();
        input.current?.focus();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const go = (route: string) => {
    location.hash = `#/${route}`;
    setText("");
    setOpen(false);
    input.current?.blur();
  };

  return (
    <div className="search">
      <Icon name="search" size={16} />
      <input
        ref={input}
        type="search"
        placeholder="Search tools ( / )"
        aria-label="Search tools"
        value={text}
        onChange={(e) => { setText(e.target.value); setOpen(true); setIndex(0); }}
        onFocus={() => setOpen(true)}
        onBlur={() => setTimeout(() => setOpen(false), 150)}
        onKeyDown={(e) => {
          if (e.key === "ArrowDown") { e.preventDefault(); setIndex((i) => Math.min(i + 1, results.length - 1)); }
          if (e.key === "ArrowUp") { e.preventDefault(); setIndex((i) => Math.max(i - 1, 0)); }
          if (e.key === "Enter" && results[index]) go(results[index].route);
          if (e.key === "Escape") { setText(""); input.current?.blur(); }
        }}
      />
      {open && text.trim() && (
        <ul className="search-results" role="listbox">
          {results.length === 0 && <li className="none">No tool matches “{text}”.</li>}
          {results.map((t, i) => (
            <li key={t.id} role="option" aria-selected={i === index} className={i === index ? "active" : ""} onMouseDown={() => go(t.route)} onMouseEnter={() => setIndex(i)}>
              <span className={`tile-icon small ${t.tone}`}><Icon name={t.icon} size={16} /></span>
              <span><strong>{t.label}</strong><small>{t.group} · {t.desc}</small></span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

export default function App() {
  const [me, setMe] = useState<Me | null>(null);
  const [route, setRoute] = useState(currentRoute);
  const [menu, setMenu] = useState(false);
  const [userMenu, setUserMenu] = useState(false);

  const refresh = () => api<Me>("GET", "/me").then(setMe).catch(() => setMe({ authenticated: false, hostname: "", dev: false }));
  useEffect(() => {
    refresh();
    const onHash = () => {
      setRoute(currentRoute());
      setMenu(false);
      setUserMenu(false);
      window.scrollTo(0, 0);
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
  const id = pages[route] ? route : "home";
  const signOut = async () => {
    await api("POST", "/logout").catch(() => {});
    refresh();
  };

  return (
    <ToastProvider>
      <header className="topbar">
        <button className="top-btn menu-btn" onClick={() => setMenu(!menu)} aria-label="Menu" aria-expanded={menu}><Icon name="menu" /></button>
        <Logo />
        <Search />
        <div className="user">
          <button className="top-btn user-btn" onClick={() => setUserMenu(!userMenu)} aria-expanded={userMenu} aria-haspopup="menu">
            <span className="avatar" aria-hidden="true">A</span>
            <span className="user-name"><b>admin</b><small>{me.hostname}</small></span>
            <Icon name="chevron" size={16} />
          </button>
          {userMenu && (
            <div className="user-menu" role="menu">
              <a role="menuitem" href="#/settings"><Icon name="settings" size={16} /> Settings</a>
              <a role="menuitem" href="#/settings"><Icon name="key" size={16} /> Change password</a>
              <button role="menuitem" onClick={signOut}><Icon name="logout" size={16} /> Sign out</button>
            </div>
          )}
        </div>
      </header>
      <div className="shell">
        <nav className={`sidebar${menu ? " open" : ""}`} aria-label="Main">
          <a href="#/home" className={id === "home" ? "active" : ""}><Icon name="home" size={18} /> Tools</a>
          {navGroups.map((g) => (
            <div key={g.label}>
              <div className="nav-label">{g.label}</div>
              {g.items.map((t) => (
                <a key={t.id} href={`#/${t.route}`} className={id === t.route ? "active" : ""}><Icon name={t.icon} size={18} /> {t.label}</a>
              ))}
            </div>
          ))}
        </nav>
        {menu && <div className="scrim" onClick={() => setMenu(false)} />}
        <main className="content" onClick={() => userMenu && setUserMenu(false)}>
          {me.dev && <div className="banner">Development mode: system changes are simulated, nothing on this machine is modified.</div>}
          <div key={id}>{pages[id]}</div>
        </main>
      </div>
    </ToastProvider>
  );
}
