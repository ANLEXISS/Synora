import { useEffect, useMemo, useState, type FormEvent } from "react";
import { Sidebar } from "../components/Sidebar";
import { Topbar } from "../components/Topbar";
import { Dashboard } from "../pages/Dashboard";
import { Cge } from "../pages/Cge";
import { HomeMap } from "../pages/HomeMap";
import { Devices } from "../pages/Devices";
import { Residents } from "../pages/Residents";
import { Automations } from "../pages/Automations";
import { SynoraLab } from "../pages/SynoraLab";
import { Settings } from "../pages/Settings";
import { useAuth } from "../hooks/useAuth";
import { Shield } from "lucide-react";
import { ErrorBoundary } from "../components/ErrorBoundary";
import { V1Onboarding } from "../components/V1Onboarding";

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
    title: "CGE — Cognitive Guard Engine",
    subtitle: "Chaînes d’événements, raisonnement moteur et réglages de sécurité.",
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
    title: "CGE — Cognitive Guard Engine",
    subtitle: "Chaînes d’événements, raisonnement moteur et réglages de sécurité.",
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
  live: "cge:read",
  home: "topology:read",
  devices: "devices:read",
  residents: "residents:read",
  automations: "automations:read",
  cge: "cge:read",
  lab: "lab:use",
  settings: "settings:read",
};

export default function App() {
	const auth = useAuth();
	const [page, setPage] = useState<PageId | "not-found">(pageFromPath);
	const [mobileSidebarOpen, setMobileSidebarOpen] = useState(false);
	const meta = useMemo(() => page === "not-found" ? pageMeta.dashboard : pageMeta[page], [page]);

	function navigateTo(nextPage: PageId) {
		setPage(nextPage);
		setMobileSidebarOpen(false);
		const path = nextPage === "live" ? "/cge" : `/${nextPage}`;
		if (window.location.pathname !== path) window.history.pushState({}, "", path);
	}

	useEffect(() => {
		const handleNavigate = (event: Event) => {
			const nextPage = (event as CustomEvent<unknown>).detail;
			if (typeof nextPage === "string" && nextPage in pageMeta) navigateTo(nextPage as PageId);
		};
		const handlePopState = () => setPage(pageFromPath());
		window.addEventListener("synora:navigate", handleNavigate);
		window.addEventListener("popstate", handlePopState);
		return () => {
			window.removeEventListener("synora:navigate", handleNavigate);
			window.removeEventListener("popstate", handlePopState);
		};
	}, []);

	if (page === "not-found") return <NotFound onDashboard={() => navigateTo("dashboard")} />;

	if (auth.loading) {
		return <div className="auth-shell">Vérification de la session…</div>;
	}

	if (!auth.authenticated) {
		return <LoginScreen error={auth.error} onLogin={auth.login} onBootstrap={auth.bootstrap} />;
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
			navigateTo(nextPage);
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

        <V1Onboarding navigate={navigateTo} />

        {!auth.can(pagePermissions[page]) ? (
          <AccessDenied />
        ) : (
          <ErrorBoundary key={page} name={page}>
            {page === "dashboard" && <Dashboard />}
            {page === "live" && <Cge />}
            {page === "home" && <HomeMap />}
            {page === "devices" && <Devices />}
            {page === "residents" && <Residents />}
            {page === "automations" && <Automations />}
            {page === "cge" && <Cge />}
            {page === "lab" && <SynoraLab />}
            {page === "settings" && <Settings />}
          </ErrorBoundary>
        )}
      </main>
    </div>
  );
}

function LoginScreen({
	error,
	onLogin,
	onBootstrap,
}: {
	error: string | null;
	onLogin: (login: string, password: string, token?: string) => Promise<void>;
	onBootstrap: (login: string, password: string) => Promise<void>;
}) {
	const [login, setLogin] = useState("");
	const [password, setPassword] = useState("");
	const [token, setToken] = useState("");
	const [advanced, setAdvanced] = useState(false);
	const [bootstrap, setBootstrap] = useState(false);
	const [submitting, setSubmitting] = useState(false);
	const [message, setMessage] = useState<string | null>(error);

	async function submit(event: FormEvent<HTMLFormElement>) {
		event.preventDefault();
		const invalidCredentials = advanced ? !token.trim() : !login.trim() || !password || (bootstrap && password.length < 12);
		if (invalidCredentials) {
			setMessage(advanced ? "Saisissez le token local." : bootstrap && password.length < 12 ? "Le mot de passe administrateur doit contenir au moins 12 caractères." : "Saisissez votre login et votre mot de passe.");
			return;
		}
		setSubmitting(true);
		setMessage(null);
		try {
			if (bootstrap) {
				await onBootstrap(login, password);
			} else {
				await onLogin(login, password, advanced ? token : undefined);
			}
			setLogin("");
			setPassword("");
			setToken("");
		} catch {
			setMessage(bootstrap ? "Bootstrap refusé ou API indisponible." : "Token invalide ou API indisponible.");
		} finally {
			setSubmitting(false);
		}
	}

	return (
		<div className="auth-shell">
			<form className="auth-card" onSubmit={submit}>
				<div className="auth-icon"><Shield size={24} /></div>
				<h1>Synora</h1>
				<p>{bootstrap ? "Créez le premier administrateur local, une seule fois." : "Connectez-vous avec votre compte local."}</p>
				{advanced ? (
					<>
						<label htmlFor="synora-token">Token local / bootstrap admin</label>
						<input id="synora-token" type="password" autoComplete="off" value={token} onChange={(event) => setToken(event.target.value)} placeholder="Token local" />
					</>
				) : (
					<>
						<label htmlFor="synora-login">{bootstrap ? "Login administrateur" : "Login"}</label>
						<input id="synora-login" type="text" autoComplete="username" value={login} onChange={(event) => setLogin(event.target.value)} placeholder="alexis" />
						<label htmlFor="synora-password">Mot de passe {bootstrap && "(12 caractères minimum)"}</label>
						<input id="synora-password" type="password" autoComplete={bootstrap ? "new-password" : "current-password"} value={password} onChange={(event) => setPassword(event.target.value)} placeholder="Mot de passe" />
					</>
				)}
				{message && <div className="auth-error">{message}</div>}
				<button className="primary-button" type="submit" disabled={submitting}>
					{submitting ? (bootstrap ? "Création…" : "Connexion…") : (bootstrap ? "Créer l’administrateur" : "Se connecter")}
				</button>
				<button className="secondary-button" type="button" onClick={() => { setAdvanced((value) => !value); setBootstrap(false); }}>
					{advanced ? "Utiliser un compte" : "Connexion par token"}
				</button>
				{!advanced && <button className="secondary-button" type="button" onClick={() => { setBootstrap((value) => !value); setMessage(null); }}>
					{bootstrap ? "J’ai déjà un compte" : "Première installation"}
				</button>}
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

function pageFromPath(): PageId | "not-found" {
	const path = window.location.pathname.replace(/\/+$/, "") || "/";
	const routes: Record<string, PageId> = {
		"/": "dashboard",
		"/dashboard": "dashboard",
		"/live-events": "live",
		"/cge": "live",
		"/home": "home",
		"/devices": "devices",
		"/residents": "residents",
		"/automations": "automations",
		"/lab": "lab",
		"/settings": "settings",
	};
	return routes[path] ?? "not-found";
}

function NotFound({ onDashboard }: { onDashboard: () => void }) {
	return <section className="page-placeholder"><h2>Page introuvable</h2><p>Cette route Synora n’existe pas ou n’est plus disponible.</p><div className="page-placeholder-actions"><button type="button" className="primary-button" onClick={onDashboard}>Retour au tableau de bord</button></div></section>;
}
