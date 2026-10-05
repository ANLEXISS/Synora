import { BrainCircuit, ShieldCheck } from "lucide-react";
import { Intelligence } from "./pages/Intelligence";
import { Sidebar } from "./components/Sidebar";

export default function App() {
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
        <Intelligence />
      </main>
    </div>
  );
}
