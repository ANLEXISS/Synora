import type { ComponentType } from "react";
import {
  Bot,
  Brain,
  Cpu,
  Home,
  LayoutDashboard,
  Settings,
  Shield,
  Sparkles,
  Users,
  Workflow,
} from "lucide-react";
import type { PageId } from "../app/App";

type SidebarProps = {
  page: PageId;
  can: (permission: string) => boolean;
  setPage: (page: PageId) => void;
  mobileOpen: boolean;
  toggleMobile: () => void;
};

const items: {
  id: PageId;
  label: string;
  icon: ComponentType<{ size?: number }>;
}[] = [
  { id: "dashboard", label: "Dashboard", icon: LayoutDashboard },
  { id: "live", label: "CGE", icon: Brain },
  { id: "home", label: "Maison", icon: Home },
  { id: "devices", label: "Périphériques", icon: Cpu },
  { id: "residents", label: "Résidents", icon: Users },
  { id: "automations", label: "Automations", icon: Workflow },
  { id: "lab", label: "Synora Lab", icon: Bot },
  { id: "settings", label: "Settings", icon: Settings },
];

export function Sidebar({
  page,
  can,
  setPage,
  mobileOpen,
  toggleMobile,
}: SidebarProps) {
  return (
    <aside className={`sidebar ${mobileOpen ? "mobile-open" : ""}`}>
      <button
        className="brand brand-button"
        type="button"
        onClick={toggleMobile}
        aria-label="Ouvrir ou fermer le menu"
      >
        <div className="brand-mark">
          <Shield size={22} />
        </div>

        <div className="brand-text">
          <strong>Synora</strong>
          <span>Local Intelligence</span>
        </div>
      </button>

      <nav className="nav">
        {items.filter((item) => can(permissionForPage(item.id))).map((item) => {
          const Icon = item.icon;

          return (
            <button
              key={item.id}
              className={page === item.id ? "active" : ""}
              onClick={() => setPage(item.id)}
              type="button"
              title={item.label}
            >
              <Icon size={18} />
              <span>{item.label}</span>
            </button>
          );
        })}
      </nav>

      <div className="sidebar-card">
        <div className="sidebar-card-icon">
          <Sparkles size={18} />
        </div>

        <strong>Core local</strong>
        <span>Aucune donnée cloud requise.</span>
      </div>
    </aside>
  );
}

function permissionForPage(page: PageId) {
  switch (page) {
    case "dashboard": return "state:read";
    case "live": return "cge:read";
    case "home":
      return "topology:read";
    case "devices":
      return "devices:read";
    case "residents":
      return "residents:read";
    case "automations":
      return "automations:read";
    case "cge":
      return "cge:read";
    case "lab":
      return "lab:use";
    case "settings":
      return "settings:read";
  }
}
