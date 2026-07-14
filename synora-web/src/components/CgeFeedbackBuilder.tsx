import { ArrowDown, ArrowUp, Plus, Trash2, X } from "lucide-react";
import { useState, type FormEvent } from "react";
import type {
  CgeChainFeedbackPayload,
  CgeCorrectionType,
  CgeEvaluationFeedbackPayload,
  CgeFeedbackScope,
  CgePreferredAction,
} from "../lib/synora-types";

type CgeFeedbackBuilderProps = {
  mode: "evaluation" | "chain";
  chainId: string;
  eventId?: string;
  evaluationIndex?: number;
  initialCorrectionType?: CgeCorrectionType;
  initialScope?: CgeFeedbackScope;
  initialPreferredActions?: CgePreferredAction[];
  onSubmit: (payload: CgeEvaluationFeedbackPayload | CgeChainFeedbackPayload) => Promise<void>;
  onCancel: () => void;
};

const correctionOptions: Array<{ value: CgeCorrectionType; label: string }> = [
  { value: "false_positive", label: "Faux positif" },
  { value: "false_negative", label: "Faux négatif" },
  { value: "reaction_too_strong", label: "Réaction trop forte" },
  { value: "reaction_too_weak", label: "Réaction insuffisante" },
  { value: "correct_but_tune_actions", label: "Évaluation correcte, ajuster la réaction" },
];

const actionOptions: Array<{ value: CgePreferredAction; label: string }> = [
  { value: "observe", label: "Observer seulement" },
  { value: "notify_owner", label: "Notifier le propriétaire" },
  { value: "notify_emergency_contact", label: "Notifier un contact d’urgence" },
  { value: "record_clip", label: "Enregistrer un clip" },
  { value: "lock_evidence", label: "Verrouiller la preuve" },
  { value: "create_alert", label: "Créer une alerte" },
  { value: "request_user_validation", label: "Demander validation utilisateur" },
  { value: "ignore_pattern", label: "Ignorer ce pattern" },
  { value: "activate_related_automation", label: "Activer une automation liée" },
  { value: "notify.whatsapp", label: "Suggérer notify.whatsapp" },
  { value: "record.clip", label: "Suggérer record.clip" },
  { value: "mark_intrusion_candidate", label: "Marquer candidat intrusion" },
  { value: "store_evidence", label: "Conserver la preuve" },
];

const actionLabels = Object.fromEntries(actionOptions.map((item) => [item.value, item.label])) as Record<CgePreferredAction, string>;

export function CgeFeedbackBuilder({
  mode,
  chainId,
  eventId,
  evaluationIndex,
  initialCorrectionType = "false_positive",
  initialScope = "case_only",
  initialPreferredActions = [],
  onSubmit,
  onCancel,
}: CgeFeedbackBuilderProps) {
  const [correctionType, setCorrectionType] = useState(initialCorrectionType);
  const [scope, setScope] = useState(initialScope);
  const [actions, setActions] = useState<CgePreferredAction[]>(initialPreferredActions);
  const [nextAction, setNextAction] = useState<CgePreferredAction>("observe");
  const [adminNote, setAdminNote] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  function addAction() {
    setActions((current) => [...current, nextAction]);
  }

  function removeAction(index: number) {
    setActions((current) => current.filter((_, currentIndex) => currentIndex !== index));
  }

  function moveAction(index: number, direction: -1 | 1) {
    setActions((current) => {
      const target = index + direction;
      if (target < 0 || target >= current.length) return current;
      const next = [...current];
      [next[index], next[target]] = [next[target], next[index]];
      return next;
    });
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!chainId.trim() || (mode === "evaluation" && !eventId?.trim())) {
      setError("La chaîne et le maillon sont nécessaires pour enregistrer cette correction.");
      return;
    }
    setSaving(true);
    setError(null);
    try {
      const common = { correction_type: correctionType, scope, preferred_actions: actions, preferred_action_details: actions.map((command) => ({ command, target: command.includes("notify") ? "owner" : undefined, enabled: true })), admin_note: adminNote.trim() || undefined };
      if (mode === "evaluation") {
        await onSubmit({ ...common, chain_id: chainId, event_id: eventId as string, evaluation_index: evaluationIndex });
      } else {
        await onSubmit({ ...common, chain_id: chainId });
      }
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "La correction n’a pas été enregistrée.");
    } finally {
      setSaving(false);
    }
  }

  const previewActions = actions.length > 0 ? actions.map((action) => actionLabels[action]).join(", ") : "aucune action spécifique";
  return (
    <div className="cge-modal-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) onCancel(); }}>
      <section className="cge-modal cge-correction-modal" role="dialog" aria-modal="true" aria-labelledby="cge-feedback-builder-title">
        <header>
          <div><span>{mode === "evaluation" ? "Correction d’évaluation" : "Correction de fin de chaîne"}</span><h2 id="cge-feedback-builder-title">Ajuster l’intention Synora</h2></div>
          <button className="icon-button" type="button" onClick={onCancel} aria-label="Fermer"><X size={18} /></button>
        </header>
        <form className="cge-modal-content cge-correction-form" onSubmit={(event) => void submit(event)}>
          <p>L’événement brut et l’historique resteront inchangés. Cette annotation guidera les futures évaluations selon sa portée.</p>
          {error && <div className="auth-error" role="alert">{error}</div>}
          <label>Type de correction<select value={correctionType} onChange={(event) => setCorrectionType(event.target.value as CgeCorrectionType)}>{correctionOptions.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}</select></label>
          <label>Portée<select value={scope} onChange={(event) => setScope(event.target.value as CgeFeedbackScope)}><option value="case_only">Seulement ce cas</option><option value="apply_to_similar_future_chains">Appliquer aux cas similaires futurs</option></select></label>
          <fieldset className="cge-feedback-actions-builder"><legend>Réaction souhaitée</legend><div className="cge-feedback-action-add"><select aria-label="Action à ajouter" value={nextAction} onChange={(event) => setNextAction(event.target.value as CgePreferredAction)}>{actionOptions.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}</select><button type="button" className="secondary-button" onClick={addAction}><Plus size={15} /> Ajouter</button></div>{actions.length === 0 ? <small>Aucune action préférée. Synora conservera son comportement par défaut.</small> : <ol>{actions.map((action, index) => <li key={`${action}-${index}`}><span>{actionLabels[action]}</span><button type="button" className="icon-button" onClick={() => moveAction(index, -1)} disabled={index === 0} aria-label="Monter l’action"><ArrowUp size={14} /></button><button type="button" className="icon-button" onClick={() => moveAction(index, 1)} disabled={index === actions.length - 1} aria-label="Descendre l’action"><ArrowDown size={14} /></button><button type="button" className="icon-button danger" onClick={() => removeAction(index)} aria-label={`Retirer ${actionLabels[action]}`}><Trash2 size={14} /></button></li>)}</ol>}</fieldset>
          <label>Note admin<textarea value={adminNote} onChange={(event) => setAdminNote(event.target.value)} maxLength={4000} rows={4} placeholder="Expliquez la correction…" /></label>
          <div className="cge-feedback-preview"><strong>Prévisualisation</strong><p>Pour un cas similaire, Synora privilégiera : {previewActions}.</p><p>Cette correction s’applique {scope === "case_only" ? "seulement à cette chaîne" : "aux cas similaires futurs"}.</p><p>L’événement brut restera inchangé.</p></div>
          <div className="cge-correction-actions"><button type="button" className="secondary-button" onClick={onCancel}>Annuler</button><button type="submit" className="primary-button" disabled={saving}>{saving ? "Enregistrement…" : "Enregistrer la correction"}</button></div>
        </form>
      </section>
    </div>
  );
}
