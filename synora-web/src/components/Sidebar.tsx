import { BrainCircuit, Shield } from "lucide-react";

export function Sidebar() {
  return (
    <aside className="sidebar">
      <div className="brand">
        <span className="brand-mark"><Shield size={20} /></span>
        <span><strong>Synora</strong><small>Local Intelligence</small></span>
      </div>
      <nav aria-label="Navigation principale">
        <a className="nav-item active" href="/intelligence" aria-current="page">
          <BrainCircuit size={18} /> Intelligence
        </a>
      </nav>
      <div className="sidebar-note">Aucune donnée cloud. Les actions physiques restent désactivées.</div>
    </aside>
  );
}
