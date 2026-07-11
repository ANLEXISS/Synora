import { Moon, Wifi } from "lucide-react";
import type { SynoraUser } from "../lib/auth";

type TopbarProps = {
  title: string;
  subtitle: string;
  user?: SynoraUser | null;
  onLogout?: () => void;
};

export function Topbar({ title, subtitle, user, onLogout }: TopbarProps) {
  return (
    <header className="topbar">
      <div className="page-title">
        <h1>{title}</h1>
        <p>{subtitle}</p>
      </div>

      <div className="topbar-actions">
        <div className="status-pill">
          <span className="status-dot" />
          Système nominal
        </div>

        <div className="mode-pill">
          <Moon size={16} />
          Night-ready
        </div>

        <div className="mode-pill">
          <Wifi size={16} />
          Connecté
        </div>

        {user && (
          <div className="mode-pill">
            {user.login} · {user.role === "admin" ? "Admin" : user.role === "resident" ? "Résident" : "Invité"}
          </div>
        )}

        {onLogout && (
          <button className="secondary-button" type="button" onClick={onLogout}>
            Déconnexion
          </button>
        )}
      </div>
    </header>
  );
}
