import { ArrowLeft, CheckCircle2, ChevronRight, CircleHelp, X } from "lucide-react";
import { useMemo, useState } from "react";
import { confirmSynoraCameraPairing, startSynoraCameraPairing, type SynoraCameraQRPayload } from "../lib/synora-api";
import { SynoraApiError } from "../lib/api";
import type { ApiTopologyNode } from "../lib/synora-types";
import { QRCodeScanner } from "./QRCodeScanner";

type PairingStart = {
  session_id: string;
  device_id: string;
  serial?: string;
  model?: string;
  expires_at: string;
};

type WizardProps = {
  topology: ApiTopologyNode[];
  onClose: () => void;
  onPaired: (deviceID: string) => Promise<boolean>;
};

function roomsFromTopology(nodes: ApiTopologyNode[]): Array<{ id: string; label: string }> {
  return nodes.flatMap((node) => [
    ...(node.type === "room" ? [{ id: node.id, label: node.name || node.id }] : []),
    ...roomsFromTopology(node.children ?? []),
  ]);
}

function parseQRCode(value: string): SynoraCameraQRPayload {
  if (value.length > 64 * 1024) throw new Error("Le code QR est trop volumineux.");
  let parsed: unknown;
  try {
    parsed = JSON.parse(value);
  } catch {
    throw new Error("Le code QR ne contient pas un JSON valide.");
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error("Le code QR doit être un objet JSON.");
  }
  const payload = parsed as Partial<SynoraCameraQRPayload>;
  if (payload.type !== "synora.camera") throw new Error("Ce QR code n’est pas une caméra Synora.");
  if (typeof payload.version !== "number" || payload.version < 1) throw new Error("Version de QR code non supportée.");
  if (typeof payload.device_id !== "string" || !payload.device_id.trim()) throw new Error("device_id manquant.");
  if (typeof payload.setup_token !== "string" || !payload.setup_token) throw new Error("setup_token manquant.");
  return {
    type: "synora.camera",
    version: payload.version,
    device_id: payload.device_id,
    serial: typeof payload.serial === "string" ? payload.serial : "",
    model: typeof payload.model === "string" ? payload.model : "",
    setup_token: payload.setup_token,
  };
}

function readableError(error: unknown) {
  if (error instanceof SynoraApiError) {
    if (error.status === 403) return "Accès refusé : l’appairage est réservé aux administrateurs.";
    if (error.status === 409) return "Cette caméra est déjà enregistrée.";
    if (error.status === 404) return "La session d’appairage a expiré. Recommencez le scan.";
    if (error.status >= 500) return "Le backend Synora est indisponible.";
    return "Le backend a refusé ce code de caméra.";
  }
  return error instanceof Error ? error.message : "Une erreur est survenue.";
}

export function SynoraCameraPairingWizard({ topology, onClose, onPaired }: WizardProps) {
  const [step, setStep] = useState<1 | 2 | 3 | 4 | 5>(1);
  const [manualCode, setManualCode] = useState("");
  const [pairing, setPairing] = useState<PairingStart | null>(null);
  const [name, setName] = useState("");
  const [nodeID, setNodeID] = useState("");
  const [enabled, setEnabled] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const rooms = useMemo(() => roomsFromTopology(topology), [topology]);

  async function submitCode(value: string) {
    setError(null);
    try {
      const payload = parseQRValue(value);
      setBusy(true);
      const result = await startSynoraCameraPairing({ qr_payload: payload });
      setPairing(result);
      setManualCode("");
      setName(payload.device_id);
      setStep(3);
    } catch (caught) {
      setError(readableError(caught));
    } finally {
      setBusy(false);
    }
  }

  function parseQRValue(value: string) {
    return parseQRJson(value);
  }

  async function confirm() {
    if (!pairing) return;
    if (!name.trim()) {
      setError("Le nom affiché est obligatoire.");
      return;
    }
    if (!nodeID) {
      setError("Sélectionnez une pièce avant de continuer.");
      return;
    }
    setError(null);
    setBusy(true);
    try {
      await confirmSynoraCameraPairing({
        session_id: pairing.session_id,
        name: name.trim(),
        node_id: nodeID,
        enabled,
      });
      const verified = await onPaired(pairing.device_id);
      if (!verified) {
        setError("L’ajout de la caméra n’a pas été confirmé par le backend.");
        return;
      }
      setStep(5);
    } catch (caught) {
      setError(readableError(caught));
    } finally {
      setBusy(false);
    }
  }

  const stepTitle = step === 1 ? "Ajouter un périphérique" : step === 2 ? "Scanner la caméra" : step === 3 ? "Configurer la caméra" : step === 4 ? "Confirmer l’appairage" : "Caméra appairée";

  return (
    <div className="pairing-modal-backdrop" role="presentation">
      <section className="pairing-modal" role="dialog" aria-modal="true" aria-labelledby="pairing-title">
        <header className="pairing-modal-header">
          <div><span>Appairage · étape {Math.min(step, 4)}/4</span><h2 id="pairing-title">{stepTitle}</h2></div>
          <button type="button" className="icon-button" onClick={onClose} aria-label="Fermer"><X size={19} /></button>
        </header>

        {error && <div className="wizard-error" role="alert">{error}</div>}

        <div className="pairing-modal-content">
          {step === 1 && (
            <div className="pairing-choice-grid">
              <button type="button" className="pairing-choice selected" onClick={() => setStep(2)}>
                <span className="pairing-choice-icon">◉</span><strong>Caméra Synora</strong><span>Appairage QR sécurisé</span><ChevronRight size={17} />
              </button>
              <button type="button" className="pairing-choice disabled" disabled><span className="pairing-choice-icon">◇</span><strong>Matter / Thread</strong><span>Bientôt disponible</span><CircleHelp size={17} /></button>
            </div>
          )}

          {step === 2 && (
            <div className="pairing-source-grid">
              <QRCodeScanner onPayload={(value) => void submitCode(value)} />
              <div className="pairing-manual">
                <label htmlFor="camera-qr-code">Ou coller le JSON du QR code</label>
                <textarea id="camera-qr-code" value={manualCode} onChange={(event) => setManualCode(event.target.value)} placeholder={'{"type":"synora.camera", ...}'} rows={6} />
                <button type="button" className="primary-button" disabled={busy || !manualCode.trim()} onClick={() => void submitCode(manualCode)}>{busy ? "Validation…" : "Valider le code"}</button>
                <p className="wizard-note">Le secret de configuration sert uniquement à établir la session et ne sera pas affiché après validation.</p>
              </div>
            </div>
          )}

          {step === 3 && pairing && (
            <div className="pairing-details">
              <div className="pairing-device-summary"><strong>Informations détectées</strong><span>Device ID · {pairing.device_id}</span><span>Serial · {pairing.serial || "Non renseigné"}</span><span>Modèle · {pairing.model || "Non renseigné"}</span></div>
              <label>Nom affiché<input value={name} onChange={(event) => setName(event.target.value)} maxLength={128} placeholder="Caméra entrée" /></label>
              <label>Pièce<select value={nodeID} onChange={(event) => setNodeID(event.target.value)}><option value="">Sélectionner une pièce</option>{rooms.map((room) => <option key={room.id} value={room.id}>{room.label}</option>)}</select></label>
              {rooms.length === 0 && <p className="wizard-note">Aucune pièce n’est disponible dans la topologie.</p>}
              <label className="pairing-checkbox"><input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} /> Activer la caméra immédiatement</label>
            </div>
          )}

          {step === 4 && pairing && (
            <div className="pairing-confirm"><div className="pairing-confirm-icon"><CheckCircle2 size={28} /></div><h3>{name}</h3><p>{pairing.device_id} sera ajouté dans {rooms.find((room) => room.id === nodeID)?.label ?? nodeID}.</p><span>La caméra sera marquée comme approuvée et apparaîtra dans Devices.</span></div>
          )}

          {step === 5 && <div className="pairing-success"><CheckCircle2 size={48} /><h3>Caméra ajoutée</h3><p>La caméra apparaît maintenant dans la liste des périphériques.</p></div>}
        </div>

        <footer className="pairing-modal-footer">
          {step > 1 && step < 5 && <button type="button" className="secondary-button" onClick={() => { setError(null); setStep((current) => (current - 1) as 1 | 2 | 3 | 4); }}><ArrowLeft size={15} /> Retour</button>}
          <span />
          {step === 1 && <button type="button" className="primary-button" onClick={() => setStep(2)}>Commencer <ChevronRight size={15} /></button>}
          {step === 3 && <button type="button" className="primary-button" disabled={busy || !nodeID || !name.trim()} onClick={() => setStep(4)}>Vérifier <ChevronRight size={15} /></button>}
          {step === 4 && <button type="button" className="primary-button" disabled={busy} onClick={() => void confirm()}>{busy ? "Ajout…" : "Confirmer l’appairage"}</button>}
          {step === 5 && <button type="button" className="primary-button" onClick={onClose}>Fermer</button>}
        </footer>
      </section>
    </div>
  );
}

function parseQRJson(value: string): SynoraCameraQRPayload {
  return parseQRCode(value);
}
