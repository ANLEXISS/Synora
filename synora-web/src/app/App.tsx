import { useMemo, useState } from "react";
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

export default function App() {
  const [page, setPage] = useState<PageId>("dashboard");
  const [mobileSidebarOpen, setMobileSidebarOpen] = useState(false);

  const meta = useMemo(() => pageMeta[page], [page]);

  return (
    <div
      className={`app-shell ${
        mobileSidebarOpen ? "mobile-sidebar-open" : ""
      }`}
    >
      <Sidebar
        page={page}
        setPage={(nextPage) => {
          setPage(nextPage);
          setMobileSidebarOpen(false);
        }}
        mobileOpen={mobileSidebarOpen}
        toggleMobile={() => setMobileSidebarOpen((open) => !open)}
      />

      <main className="main">
        <Topbar title={meta.title} subtitle={meta.subtitle} />

        {page === "dashboard" && <Dashboard />}
        {page === "live" && <LiveEvents />}
        {page === "home" && <HomeMap />}
        {page === "devices" && <Devices />}
        {page === "residents" && <Residents />}
        {page === "automations" && <Automations />}
        {page === "cge" && <CgeInspector />}
        {page === "lab" && <SynoraLab />}
        {page === "settings" && <Settings />}
      </main>
    </div>
  );
}