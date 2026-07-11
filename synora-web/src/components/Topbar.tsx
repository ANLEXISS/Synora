import { Moon, Wifi } from "lucide-react";

type TopbarProps = {
  title: string;
  subtitle: string;
};

export function Topbar({ title, subtitle }: TopbarProps) {
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
          Local API
        </div>
      </div>
    </header>
  );
}