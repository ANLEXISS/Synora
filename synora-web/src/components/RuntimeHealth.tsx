import { CircleAlert, CircleCheck, CircleHelp, RefreshCw } from "lucide-react";
import { useEffect, useState } from "react";
import { getRuntimeHealth, type RuntimeHealth } from "../lib/health";

const labels: Record<string, string> = { "synora-bus": "Bus", "synora-core": "Core", "synora-discovery": "Discovery", mediamtx: "Média" };

export function RuntimeHealthCard() {
  const [health, setHealth] = useState<RuntimeHealth | null>(null);
  const [loading, setLoading] = useState(true);
  useEffect(() => {
    const controller = new AbortController();
    const refresh = () => { void getRuntimeHealth(controller.signal).then(setHealth).finally(() => setLoading(false)); };
    refresh();
    const timer = window.setInterval(refresh, 10000);
    return () => { controller.abort(); window.clearInterval(timer); };
  }, []);
  const status = health?.status ?? "unknown";
  const StatusIcon = status === "ok" ? CircleCheck : status === "unknown" ? CircleHelp : CircleAlert;
  const services = Object.entries(health?.services ?? {}).filter(([name]) => ["synora-bus", "synora-core", "synora-discovery", "mediamtx"].includes(name));
  return <section className={`runtime-health-card status-${status}`} aria-live="polite"><div className="runtime-health-heading"><div><span className="runtime-health-kicker"><StatusIcon size={15} /> Santé runtime</span><strong>{loading && !health ? "Vérification…" : statusLabel(status)}</strong></div><small>{health?.disk?.status ? `Disque : ${health.disk.status}` : "Aucune donnée sensible exposée"}</small></div>{services.length > 0 && <div className="runtime-health-services">{services.map(([name, service]) => <span key={name} title={service.message ?? service.error ?? ""}><i className={`health-dot status-${service.status ?? "unknown"}`} />{labels[name] ?? name}: {statusLabel(service.status ?? "unknown")}</span>)}</div>}{!health && !loading && <div className="runtime-health-foot"><RefreshCw size={14} /> État runtime indisponible · aucune réussite supposée.</div>}</section>;
}

function statusLabel(status: string) { return status === "ok" ? "Opérationnel" : status === "degraded" ? "Dégradé" : status === "offline" ? "Hors ligne" : "Inconnu"; }

