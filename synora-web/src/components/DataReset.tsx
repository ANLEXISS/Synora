import { AlertTriangle, CheckCircle2, Trash2 } from "lucide-react";
import { useState } from "react";
import type { FormEvent } from "react";

type ResetState = "idle" | "pending" | "erased" | "unknown";

export function DataReset({ csrfToken }: { csrfToken: string }) {
  const [reason, setReason] = useState("");
  const [state, setState] = useState<ResetState>("idle");
  const [message, setMessage] = useState("");
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (reason.trim().length < 8 || !window.confirm("Effacer les données locales Core et Discovery ?")) return;
    setState("pending");
    setMessage("");
    try {
      const response = await fetch("/api/system/data", {
        method: "DELETE",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json", "X-Synora-CSRF": csrfToken },
        body: JSON.stringify({ reason: reason.trim() }),
      });
      const body = await response.json().catch(() => ({ status: "unknown" }));
      if (!response.ok || body.status !== "erased" || body.scope !== "global") {
        setState("unknown");
        setMessage("Effacement non confirmé : état inconnu, aucune réussite supposée.");
        return;
      }
      setState("erased");
      setMessage("Données locales Core et Discovery effacées.");
      setReason("");
    } catch {
      setState("unknown");
      setMessage("Effacement non confirmé : service indisponible.");
    }
  };
  return <section className={`data-reset-card reset-${state}`} aria-live="polite">
    <div className="data-reset-heading"><div><span><Trash2 size={15} /> Contrôle des données</span><strong>Effacement local coordonné</strong></div><AlertTriangle size={18} /></div>
    <p>Supprime les états Core, clips, queue Vision et données faciales locales. La configuration de déploiement est conservée.</p>
    <form onSubmit={submit}><label htmlFor="data-reset-reason">Raison obligatoire</label><div className="data-reset-controls"><input id="data-reset-reason" value={reason} minLength={8} onChange={(event) => setReason(event.target.value)} placeholder="Ex. demande de réinitialisation" required /><button type="submit" disabled={state === "pending" || reason.trim().length < 8}>{state === "pending" ? "Effacement…" : "Effacer"}</button></div></form>
    {message && <div className="data-reset-result">{state === "erased" ? <CheckCircle2 size={15} /> : <AlertTriangle size={15} />}{message}</div>}
  </section>;
}
