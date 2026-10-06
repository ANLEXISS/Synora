import { BrainCircuit, ShieldCheck } from "lucide-react";
import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import { Intelligence } from "./pages/Intelligence";
import { Sidebar } from "./components/Sidebar";
import { RuntimeHealthCard } from "./components/RuntimeHealth";
import { DataReset } from "./components/DataReset";

type Session = { role?: string; csrf?: string };

export default function App() {
  const [session, setSession] = useState<Session | null | undefined>(undefined);
  const [csrfToken, setCsrfToken] = useState("");
  const [authError, setAuthError] = useState("");

  useEffect(() => {
    fetch("/api/auth/me", { credentials: "same-origin", cache: "no-store" })
      .then(async (response) => {
        if (!response.ok) { setSession(null); return; }
        const nextSession = await response.json();
        setCsrfToken(nextSession.csrf ?? "");
        setSession(nextSession);
      })
      .catch(() => setSession(null));
  }, []);

  if (session === undefined) return <AuthLoading />;
  if (!session) return <AuthPanel onAuthenticated={(nextSession, nextCsrf) => { setAuthError(""); setSession(nextSession); setCsrfToken(nextCsrf); }} error={authError} onError={setAuthError} />;
  return (
    <div className="app-shell">
      <Sidebar />
      <main className="main-content">
        <header className="topbar">
          <div>
            <span className="topbar-kicker"><BrainCircuit size={15} /> V1 · Core local</span>
            <h1>Intelligence Synora</h1>
            <p>Lecture honnête des inférences MLP observées.</p>
          </div>
          <span className="dry-run-badge"><ShieldCheck size={15} /> Aucune action physique</span>
        </header>
        <RuntimeHealthCard />
        {session.role === "admin" && <DataReset csrfToken={csrfToken} />}
        <Intelligence />
      </main>
    </div>
  );
}

function AuthLoading() {
  return <main className="auth-shell"><section className="auth-card"><strong>Vérification de la session</strong><span>Synora prépare l’accès local.</span></section></main>;
}

function AuthPanel({ onAuthenticated, error, onError }: { onAuthenticated: (session: Session, csrfToken: string) => void; error: string; onError: (value: string) => void }) {
  const [token, setToken] = useState("");
  const [pending, setPending] = useState(false);
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setPending(true);
    onError("");
    try {
      const response = await fetch("/api/auth/session", { method: "POST", headers: { Authorization: `Bearer ${token}` }, credentials: "same-origin" });
      if (!response.ok) throw new Error(response.status === 401 ? "Jeton refusé." : "Session indisponible.");
      const nextSession = await response.json();
      setToken("");
      onAuthenticated(nextSession, response.headers.get("X-Synora-CSRF") ?? "");
    } catch (caught) {
      onError(caught instanceof Error ? caught.message : "Connexion impossible.");
    } finally {
      setPending(false);
    }
  };
  return <main className="auth-shell"><section className="auth-card"><div className="topbar-kicker"><ShieldCheck size={16} /> Accès local protégé</div><h1>Ouvrir Synora</h1><p>Entrez le jeton d’administration local pour créer une session temporaire. Il reste uniquement en mémoire du navigateur.</p><form onSubmit={submit}><label htmlFor="api-token">Jeton local</label><input id="api-token" type="password" autoComplete="off" value={token} onChange={(event) => setToken(event.target.value)} required /><button type="submit" disabled={pending}>{pending ? "Connexion…" : "Ouvrir la session"}</button></form>{error && <div className="auth-error" role="alert">{error}</div>}</section></main>;
}
