import { Plus, RefreshCw, RotateCcw, Send, ShieldAlert, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useAuth } from "../hooks/useAuth";
import { Panel } from "../components/Panel";
import { getActionPolicy, resetActionPolicy, testAction, updateActionPolicy } from "../lib/synora-api";
import type { ActionPolicy, ActionPolicyEntry, DangerLevel } from "../lib/synora-types";

const levels: DangerLevel[] = ["low", "medium", "medium_high", "high", "critical"];
const commands = ["observe", "store_evidence", "record.clip", "notify", "notify.whatsapp", "mark_intrusion_candidate", "increase_retention", "siren", "light.on", "device.command"];

function clonePolicy(value: ActionPolicy): ActionPolicy {
  return { ...value, levels: Object.fromEntries(Object.entries(value.levels ?? {}).map(([key, item]) => [key, { ...item, actions: (item?.actions ?? []).map((action) => ({ ...action })) }])) as ActionPolicy["levels"] };
}

function label(level: DangerLevel) {
  return level === "medium_high" ? "Moyen élevé" : level === "high" ? "Élevé" : level === "critical" ? "Critique" : level === "medium" ? "Moyen" : "Faible";
}

export function Settings() {
  const auth = useAuth();
  const [policy, setPolicy] = useState<ActionPolicy | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  async function load() {
    setBusy(true); setError(null);
    try { setPolicy(clonePolicy(await getActionPolicy())); } catch (cause) { setError(cause instanceof Error ? cause.message : "Policy indisponible."); } finally { setBusy(false); }
  }
  useEffect(() => { void load(); }, []);

  function updateLevel(level: DangerLevel, change: Partial<NonNullable<ActionPolicy["levels"][DangerLevel]>>) {
    setPolicy((current) => current ? { ...current, levels: { ...current.levels, [level]: { ...current.levels[level], ...change } } } : current);
  }
  function updateAction(level: DangerLevel, index: number, change: Partial<ActionPolicyEntry>) {
    const item = policy?.levels[level]; if (!item) return;
    updateLevel(level, { actions: item.actions.map((action, actionIndex) => actionIndex === index ? { ...action, ...change } : action) });
  }
  function addAction(level: DangerLevel) {
    const item = policy?.levels[level]; if (!item) return;
    updateLevel(level, { actions: [...item.actions, { id: `${level}-action-${item.actions.length + 1}`, command: "observe", target: "", enabled: true, priority: 10 }] });
  }
  function removeAction(level: DangerLevel, index: number) {
    const item = policy?.levels[level]; if (!item) return;
    updateLevel(level, { actions: item.actions.filter((_, actionIndex) => actionIndex !== index) });
  }
  async function save() {
    if (!policy || !auth.isAdmin) return;
    setBusy(true); setError(null); setMessage(null);
    try { setPolicy(clonePolicy(await updateActionPolicy(policy))); setMessage("Action Policy enregistrée avec backup atomique."); } catch (cause) { setError(cause instanceof Error ? cause.message : "Enregistrement impossible."); } finally { setBusy(false); }
  }
  async function reset() {
    if (!auth.isAdmin || !window.confirm("Réinitialiser les paliers sûrs par défaut ?")) return;
    setBusy(true); setError(null);
    try { setPolicy(clonePolicy(await resetActionPolicy())); setMessage("Defaults sûrs restaurés."); } catch (cause) { setError(cause instanceof Error ? cause.message : "Réinitialisation impossible."); } finally { setBusy(false); }
  }
  async function dryRun() {
    setBusy(true); setError(null); setMessage(null);
    try { const result = await testAction({ command: "notify.whatsapp", target: "owner", message: "Test Synora WhatsApp", template: "synora_security_alert", dry_run: true }); setMessage(`Dry-run prêt · ${String(result.provider ?? "provider inconnu")} · destinataire masqué.`); } catch (cause) { setError(cause instanceof Error ? cause.message : "Test dry-run impossible."); } finally { setBusy(false); }
  }
  async function sendTest() {
    setBusy(true); setError(null); setMessage(null);
    try { const result = await testAction({ command: "notify.whatsapp", target: "owner", message: "Test Synora WhatsApp", template: "synora_security_alert", dry_run: false }); setMessage(`Test WhatsApp : ${String(result.status ?? "résultat reçu")}.`); } catch (cause) { setError(cause instanceof Error ? cause.message : "Envoi de test impossible."); } finally { setBusy(false); }
  }

  if (!policy) return <section className="page-placeholder"><h2>Actions & notifications</h2>{error ? <p className="auth-error">{error}</p> : <p>Chargement…</p>}</section>;
  const whatsapp = policy.notifications?.whatsapp;
  return <div className="cge-page">
    <div className="cge-page-heading"><div className="cge-page-heading-icon"><ShieldAlert size={22} /></div><div><h2>Actions & notifications</h2><p>Les paliers rendent la réaction du moteur lisible ; les Automations restent prioritaires pour le contexte précis.</p></div></div>
    {!auth.isAdmin && <div className="readonly-label">Lecture seule — les réglages sont réservés aux administrateurs.</div>}
    {error && <div className="auth-error" role="alert">{error}</div>}{message && <div className="cge-success" role="status">{message}</div>}
    <Panel title="Paliers d’action" action={<div><button className="secondary-button" type="button" onClick={() => void load()} disabled={busy}><RefreshCw size={14} /> Actualiser</button> <button className="secondary-button" type="button" onClick={() => void reset()} disabled={busy || !auth.isAdmin}><RotateCcw size={14} /> Defaults sûrs</button></div>}>
      <p>Une policy propose et explique. Elle n’active pas automatiquement la sirène ; les actions physiques doivent être explicitement activées.</p>
      <div className="action-policy-levels">{levels.map((level) => { const item = policy.levels[level] ?? { danger_level: level, enabled: false, actions: [] }; return <section className="action-policy-level" key={level}><header><div><h3>{label(level)}</h3><small><code>{level}</code></small></div><label className="checkbox-line"><input type="checkbox" checked={item.enabled} disabled={!auth.isAdmin || busy} onChange={(event) => updateLevel(level, { enabled: event.target.checked })} /> Palier actif</label></header><div className="action-policy-actions">{item.actions.map((action, index) => <div className="action-policy-row" key={`${action.id}-${index}`}><input aria-label="Identifiant action" value={action.id} disabled={!auth.isAdmin || busy} onChange={(event) => updateAction(level, index, { id: event.target.value })} /><select aria-label="Commande action" value={action.command} disabled={!auth.isAdmin || busy} onChange={(event) => updateAction(level, index, { command: event.target.value })}>{commands.map((command) => <option key={command} value={command}>{command}</option>)}</select><input aria-label="Cible action" placeholder="cible" value={action.target ?? ""} disabled={!auth.isAdmin || busy} onChange={(event) => updateAction(level, index, { target: event.target.value })} /><input aria-label="Priorité action" type="number" min="0" max="100" value={action.priority} disabled={!auth.isAdmin || busy} onChange={(event) => updateAction(level, index, { priority: Number(event.target.value) })} /><input aria-label="Cooldown secondes" type="number" min="0" value={action.cooldown_seconds ?? 0} disabled={!auth.isAdmin || busy} onChange={(event) => updateAction(level, index, { cooldown_seconds: Number(event.target.value) })} /><label className="checkbox-line"><input type="checkbox" checked={action.enabled} disabled={!auth.isAdmin || busy} onChange={(event) => updateAction(level, index, { enabled: event.target.checked })} /> actif</label><button className="icon-button danger-icon" type="button" aria-label="Supprimer l’action" disabled={!auth.isAdmin || busy} onClick={() => removeAction(level, index)}><Trash2 size={15} /></button></div>)}{item.actions.length === 0 && <small>Aucune action proposée.</small>}</div><button className="secondary-button" type="button" disabled={!auth.isAdmin || busy} onClick={() => addAction(level)}><Plus size={14} /> Ajouter une action</button></section>; })}</div>
    </Panel>
    <Panel title="Notifications WhatsApp"><div className="action-policy-whatsapp"><div><strong>WhatsApp Cloud API</strong><p>Provider : {whatsapp?.enabled ? whatsapp.dry_run ? "dry-run" : "actif" : "désactivé"} · Phone Number ID : {whatsapp?.phone_number_id_configured ? "configuré" : "non configuré"} · Destinataire : {whatsapp?.default_to_configured ? "configuré" : "non configuré"}</p><small>Le token est lu par synora-actions depuis un fichier secret ou `SYNORA_WHATSAPP_ACCESS_TOKEN` et n’est jamais affiché.</small></div><div><button className="secondary-button" type="button" disabled={!auth.isAdmin || busy} onClick={() => void dryRun()}><Send size={14} /> Tester en dry-run</button><button className="primary-button" type="button" disabled={!auth.isAdmin || busy || !whatsapp?.enabled || whatsapp.dry_run} onClick={() => void sendTest()}><Send size={14} /> Envoyer test WhatsApp</button></div></div></Panel>
    <div className="page-placeholder-actions"><button className="primary-button" type="button" disabled={!auth.isAdmin || busy} onClick={() => void save()}>Enregistrer la policy</button></div>
  </div>;
}
