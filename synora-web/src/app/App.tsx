import { useMemo, useState, type FormEvent } from "react";
import { Sidebar } from "../components/Sidebar";
import { Topbar } from "../components/Topbar";
import { Dashboard } from "../pages/Dashboard";
import { LiveEvents } from "../pages/LiveEvents";
import { HomeMap } from "../pages/HomeMap";
import { Devices } from "../pages/Devices";
import { Residents } from "../pages/Residents";
import { Automations } from "../pages/Automations";
import { CgeInspector } from "../pages/CgeInspector";
import { SynoraLab } from "../pages/SynoraLab";
import { Settings } from "../pages/Settings";
import { useAuth } from "../hooks/useAuth";
import { Shield } from "lucide-react";

export type PageId =
  | "dashboard"
  | "live"
  | "home"
  | "devices"
  | "residents"
  | "automations"
  | "cge"
  | "lab"
  | "settings";

const pageMeta: Record<PageId, { title: string; subtitle: string }> = {
  dashboard: {
    title: "Dashboard",
    subtitle: "Vue globale de la maison et du moteur Synora.",
  },
  live: {
    title: "Live Events",
    subtitle: "Flux temps réel des événements système et sécurité.",
  },
  home: {
    title: "Maison",
    subtitle: "Topologie logique, zones et périphériques associés.",
  },
  devices: {
    title: "Périphériques",
    subtitle: "Caméras, capteurs, lumières et actionneurs.",
  },
  residents: {
    title: "Résidents",
    subtitle: "Profils, présence et confiance des habitants.",
  },
  automations: {
    title: "Automations",
    subtitle: "Règles locales, réactions et scénarios conditionnels.",
  },
  cge: {
    title: "CGE Inspector",
    subtitle: "Mémoire cognitive, chaînes critiques et comportements appris.",
  },
  lab: {
    title: "Synora Lab",
    subtitle: "Simulation contrôlée des scénarios de sécurité.",
  },
  settings: {
    title: "Settings",
    subtitle: "Configuration locale, sécurité et maintenance.",
  },
};

const pagePermissions: Record<PageId, string> = {
  dashboard: "state:read",
  live: "state:read",
  home: "topology:read",
  devices: "devices:read",
  residents: "residents:read",
  automations: "automations:read",
  cge: "cge:read",
  lab: "simulation:run",
  settings: "settings:read",
};

export default function App() {
	const auth = useAuth();
	const [page, setPage] = useState<PageId>("dashboard");
	const [mobileSidebarOpen, setMobileSidebarOpen] = useState(false);

	const meta = useMemo(() => pageMeta[page], [page]);

	if (auth.loading) {
		return <div className="auth-shell">Vérification de la session…</div>;
	}

	if (!auth.authenticated) {
		return <LoginScreen error={auth.error} onLogin={auth.login} />;
	}

	return (
    <div
      className={`app-shell ${
        mobileSidebarOpen ? "mobile-sidebar-open" : ""
      }`}
    >
		<Sidebar
			page={page}
			can={auth.can}
        setPage={(nextPage) => {
          setPage(nextPage);
          setMobileSidebarOpen(false);
        }}
        mobileOpen={mobileSidebarOpen}
        toggleMobile={() => setMobileSidebarOpen((open) => !open)}
      />

      <main className="main">
        <Topbar
			title={meta.title}
			subtitle={meta.subtitle}
			user={auth.user}
			onLogout={() => void auth.logout()}
		/>

        {!auth.can(pagePermissions[page]) ? (
          <AccessDenied />
        ) : (
          <>
            {page === "dashboard" && <Dashboard />}
            {page === "live" && <LiveEvents />}
            {page === "home" && <HomeMap />}
            {page === "devices" && <Devices />}
            {page === "residents" && <Residents />}
            {page === "automations" && <Automations />}
            {page === "cge" && <CgeInspector />}
            {page === "lab" && <SynoraLab />}
            {page === "settings" && <Settings />}
          </>
        )}
      </main>
    </div>
  );
}

function LoginScreen({
	error,
	onLogin,
}: {
	error: string | null;
	onLogin: (login: string, password: string, token?: string) => Promise<void>;
}) {
	const [login, setLogin] = useState("");
	const [password, setPassword] = useState("");
	const [token, setToken] = useState("");
	const [advanced, setAdvanced] = useState(false);
	const [submitting, setSubmitting] = useState(false);
	const [message, setMessage] = useState<string | null>(error);

	async function submit(event: FormEvent<HTMLFormElement>) {
		event.preventDefault();
		if (advanced ? !token.trim() : !login.trim() || !password) {
			setMessage(advanced ? "Saisissez le token local." : "Saisissez votre login et votre mot de passe.");
			return;
		}
		setSubmitting(true);
		setMessage(null);
		try {
			await onLogin(login, password, advanced ? token : undefined);
			setLogin("");
			setPassword("");
			setToken("");
		} catch {
			setMessage("Token invalide ou API indisponible.");
		} finally {
			setSubmitting(false);
		}
	}

	return (
		<div className="auth-shell">
			<form className="auth-card" onSubmit={submit}>
				<div className="auth-icon"><Shield size={24} /></div>
				<h1>Synora</h1>
				<p>Connectez-vous avec votre compte local.</p>
				{advanced ? (
					<>
						<label htmlFor="synora-token">Token local / bootstrap admin</label>
						<input id="synora-token" type="password" autoComplete="off" value={token} onChange={(event) => setToken(event.target.value)} placeholder="Token local" />
					</>
				) : (
					<>
						<label htmlFor="synora-login">Login</label>
						<input id="synora-login" type="text" autoComplete="username" value={login} onChange={(event) => setLogin(event.target.value)} placeholder="alexis" />
						<label htmlFor="synora-password">Mot de passe</label>
						<input id="synora-password" type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} placeholder="Mot de passe" />
					</>
				)}
				{message && <div className="auth-error">{message}</div>}
				<button className="primary-button" type="submit" disabled={submitting}>
					{submitting ? "Connexion…" : "Se connecter"}
				</button>
				<button className="secondary-button" type="button" onClick={() => setAdvanced((value) => !value)}>
					{advanced ? "Utiliser un compte" : "Bootstrap token admin"}
				</button>
			</form>
		</div>
	);
}

function AccessDenied() {
	return (
		<section className="page-placeholder">
			<h2>Accès réservé</h2>
			<p>Votre rôle ne possède pas la permission nécessaire pour cette vue.</p>
		</section>
	);
}
