import {
  BASE_FACE_VIEWS,
  getBasePhotoByView,
  isBaseComplete,
  type BaseFaceView,
} from "../lib/face";
import {
  BadgeCheck,
  Brain,
  Clock,
  Crown,
  Eye,
  Home,
  ImagePlus,
  MapPin,
  Plus,
  RefreshCw,
  Search,
  Shield,
  ShieldAlert,
  Trash2,
  User,
  UserCheck,
  UserMinus,
  Users,
  X,
} from "lucide-react";
import { useMemo, useState, type ChangeEvent, type FormEvent } from "react";
import { Panel } from "../components/Panel";
import {
  acceptResidentFaceReview,
  createResident,
  buildResidentMutationPayload,
  deleteResidentFaceReview,
  deleteResidentBaseFace,
  deleteResident,
  getResidentFace,
  getResidentFaceImageUrl,
  getResidentFaceReview,
  getResidentFaceReviewImageUrl,
  getResidents,
  rebuildResidentFace,
  replaceResidentBaseFace,
  updateResident,
  uploadResidentBaseFace,
} from "../lib/synora-api";
import type { ApiTopologyNode, ResidentCreatePayload, ResidentMutationPayload, SynoraFaceProfile, SynoraResident } from "../lib/synora-types";
import { useSynoraData } from "../hooks/useSynoraData";
import { useAuth } from "../hooks/useAuth";
import { formatRelativeDateTime } from "../lib/format";
import {
  demoApiTopology,
  prettyTopologyName,
  type DemoResident,
} from "../data/demo";

type ResidentStateFilter = "all" | DemoResident["state"];
type ResidentRoleFilter = "all" | DemoResident["role"];

type ResidentForm = {
  id: string;
  first_name: string;
  last_name: string;
  display_name: string;
  role: DemoResident["role"];
  admin: boolean;
  trusted: boolean;
  reference_node_id: string;
  account_id: string;
};

function normalizeResidentRole(value: unknown): DemoResident["role"] {
  return ["owner", "resident", "guest", "child"].includes(String(value))
    ? (String(value) as DemoResident["role"])
    : "resident";
}

function normalizeResidentState(value: string | undefined): DemoResident["state"] {
  if (value === "present") return "present";
  if (value === "away" || value === "absent") return "away";
  return "unknown";
}

function roleLabel(role: DemoResident["role"]) {
  if (role === "owner") return "Propriétaire";
  if (role === "resident") return "Résident";
  if (role === "guest") return "Invité";
  if (role === "child") return "Enfant";

  return role;
}

function stateLabel(state: DemoResident["state"]) {
  if (state === "present") return "Présent";
  if (state === "away") return "Absent";

  return "—";
}

function stateTone(state: DemoResident["state"]) {
  if (state === "present") return "success";
  if (state === "away") return "neutral";

  return "neutral";
}

function scoreTone(score: number) {
  if (score >= 0.75) return "success";
  if (score >= 0.35) return "warning";

  return "danger";
}

function residentMatchesMutation(resident: SynoraResident, payload: ResidentMutationPayload) {
  const values = resident as Record<string, unknown>;
  for (const key of [
    "first_name", "last_name", "display_name", "role", "admin", "trusted",
    "enabled", "reference_node_id", "account_id",
  ] as const) {
    const expected = payload[key];
    if (expected === undefined) continue;
    const actual = values[key];
    if (typeof expected === "string") {
      if (String(actual ?? "").trim() !== expected.trim()) return false;
    } else if (actual !== expected) {
      return false;
    }
  }
  return true;
}

function initials(name: string) {
  return name
    .split(" ")
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase())
    .join("");
}

function flattenRooms(topology: ApiTopologyNode[] = demoApiTopology) {
  return topology.flatMap((zone) =>
    (zone.children ?? []).flatMap((floor) =>
      (floor.children ?? [])
        .filter((node) => node.type === "room")
        .map((room) => ({
          id: room.id,
          name: prettyTopologyName(room.name),
          floor: floor.name,
          zone: prettyTopologyName(zone.name),
        }))
    )
  );
}

function roomLabel(nodeId: string | null, topology: ApiTopologyNode[] = demoApiTopology) {
  if (!nodeId) return "Aucune pièce connue";

  const room = flattenRooms(topology).find((item) => item.id === nodeId);

  if (!room) return nodeId;

  return `${room.name} · ${room.floor}`;
}

function residentFormFrom(value?: DemoResident): ResidentForm {
  return {
    id: value?.id ?? "",
    first_name: value?.first_name ?? "",
    last_name: value?.last_name ?? "",
    display_name: value?.display_name ?? value?.name ?? "",
    role: value?.role ?? "resident",
    admin: value?.admin ?? false,
    trusted: value?.trusted ?? false,
    reference_node_id: value?.reference_node_id ?? "",
    account_id: value?.account_id ?? "",
  };
}

function faceStatusLabel(status: string | undefined) {
  switch (status) {
    case "ready":
      return "Profil prêt";
    case "needs_rebuild":
      return "Recalcul nécessaire";
    case "error":
      return "Erreur profil";
    default:
      return "Aucune photo de base";
  }
}

function presenceDescription(resident: DemoResident) {
  if (resident.state === "present") {
    return `${resident.name} est reconnu dans la maison.`;
  }

  if (resident.state === "away") {
    return `${resident.name} n’est pas actuellement détecté.`;
  }

  return "Aucun état de présence disponible.";
}

export function Residents() {
  const [search, setSearch] = useState("");
  const [stateFilter, setStateFilter] = useState<ResidentStateFilter>("all");
  const [roleFilter, setRoleFilter] = useState<ResidentRoleFilter>("all");
  const [notice, setNotice] = useState<string | null>(null);
  const [editor, setEditor] = useState<ResidentForm | null>(null);
  const [editingID, setEditingID] = useState<string | null>(null);
  const [photoResident, setPhotoResident] = useState<DemoResident | null>(null);
  const [faceProfile, setFaceProfile] = useState<SynoraFaceProfile | null>(null);
  const [reviewPhotos, setReviewPhotos] = useState<import("../lib/synora-types").SynoraFacePhoto[]>([]);
  const [busy, setBusy] = useState(false);

  const data = useSynoraData();
  const auth = useAuth();
  const residents: DemoResident[] = data.residents.map((resident) => ({
        id: resident.id,
        name: String(resident.display_name ?? resident.name ?? resident.id),
        first_name: typeof resident.first_name === "string" ? resident.first_name : "",
        last_name: typeof resident.last_name === "string" ? resident.last_name : "",
        display_name: typeof resident.display_name === "string" ? resident.display_name : undefined,
        role: normalizeResidentRole(resident["role"]),
        state: normalizeResidentState(resident.state),
        node_id: resident.node_id ?? null,
        last_seen: typeof resident["last_seen"] === "string" ? resident["last_seen"] : null,
        presence_score: typeof resident["presence_score"] === "number"
          ? resident["presence_score"] as number
          : typeof resident["confidence"] === "number"
            ? resident["confidence"] as number
            : 0,
        admin: Boolean(resident["admin"]),
        trusted: Boolean(resident["trusted"]),
        reference_node_id: resident.reference_node_id ?? null,
        account_id: resident.account_id ?? null,
        face_profile: resident.face_profile,
      }));

  const filteredResidents = useMemo(() => {
    const query = search.trim().toLowerCase();

    return residents.filter((resident) => {
      const matchSearch =
        query.length === 0 ||
        resident.id.toLowerCase().includes(query) ||
        resident.name.toLowerCase().includes(query) ||
        roleLabel(resident.role).toLowerCase().includes(query) ||
        roomLabel(resident.node_id, data.topology).toLowerCase().includes(query);

      const matchState =
        stateFilter === "all" || resident.state === stateFilter;

      const matchRole = roleFilter === "all" || resident.role === roleFilter;

      return matchSearch && matchState && matchRole;
    });
  }, [residents, search, stateFilter, roleFilter]);

  async function refreshFace(residentID: string): Promise<SynoraFaceProfile | null> {
    try {
      const [profile, review] = await Promise.all([getResidentFace(residentID), getResidentFaceReview(residentID)]);
      setFaceProfile(profile);
      setReviewPhotos(review);
      return profile;
    } catch (error) {
      setNotice(error instanceof Error ? error.message : "Impossible de charger le profil visage");
      return null;
    }
  }

  function openPhotos(resident: DemoResident) {
    setPhotoResident(resident);
    setFaceProfile(null);
    setReviewPhotos([]);
    void refreshFace(resident.id);
  }

  function openCreate() {
    setNotice(null);
    setEditingID(null);
    setEditor(residentFormFrom());
  }

  function openEdit(resident: DemoResident) {
    setNotice(null);
    setEditingID(resident.id);
    setEditor(residentFormFrom(resident));
  }

  async function saveResident(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!editor) return;
    if (!/^[a-z0-9][a-z0-9_-]{0,127}$/.test(editor.id)) {
      setNotice("L’identifiant doit être un slug minuscule (a-z, 0-9, _ ou -).");
      return;
    }
    if (!editor.first_name.trim() && !editor.display_name.trim()) {
      setNotice("Le prénom ou le nom affiché est obligatoire.");
      return;
    }
    setBusy(true);
    setNotice(null);
    try {
      const payload: ResidentMutationPayload = buildResidentMutationPayload(editor);
      if (import.meta.env.DEV) {
        console.debug("[Residents] PATCH payload", payload);
      }
      let response: SynoraResident;
      if (editingID) {
        response = await updateResident(editingID, payload);
      } else {
        const createPayload: ResidentCreatePayload = { id: editor.id, ...payload };
        response = await createResident(createPayload);
      }
      if (import.meta.env.DEV) {
        console.debug("[Residents] PATCH response", response);
      }
      if (!residentMatchesMutation(response, payload)) {
        throw new Error("La sauvegarde n’a pas été confirmée par le backend.");
      }
      const confirmedResidents = await getResidents();
      const confirmed = confirmedResidents.find((resident) => resident.id === (editingID ?? editor.id));
      if (import.meta.env.DEV) {
        console.debug("[Residents] GET confirmation", confirmed);
      }
      if (!confirmed || !residentMatchesMutation(confirmed, payload)) {
        throw new Error("La sauvegarde n’a pas été confirmée par le backend.");
      }
      await data.refresh();
      setEditor(null);
      setEditingID(null);
      setNotice("Résident enregistré.");
    } catch (error) {
      setNotice(error instanceof Error ? error.message : "Enregistrement impossible");
    } finally {
      setBusy(false);
    }
  }

  async function removeResident(resident: DemoResident) {
    if (!window.confirm(`Désactiver le résident ${resident.name} ?`)) return;
    setBusy(true);
    setNotice(null);
    try {
      await deleteResident(resident.id);
      await data.refresh();
      setNotice("Résident désactivé.");
    } catch (error) {
      setNotice(error instanceof Error ? error.message : "Suppression impossible");
    } finally {
      setBusy(false);
    }
  }

  async function uploadFace(file: File, view: BaseFaceView) {
    if (!photoResident) return;
    setBusy(true);
    setNotice(null);
    try {
      await uploadResidentBaseFace(photoResident.id, view, file);
      const profile = await refreshFace(photoResident.id);
      if (!getBasePhotoByView(profile, view)) {
        throw new Error("La modification photo n’a pas été confirmée par le backend.");
      }
      await data.refresh();
    } catch (error) {
      setNotice(error instanceof Error ? error.message : "Upload impossible");
    } finally {
      setBusy(false);
    }
  }

  async function removeFace(photoID: string) {
    if (!photoResident) return;
    setBusy(true);
    try {
      await deleteResidentBaseFace(photoResident.id, photoID);
      const profile = await refreshFace(photoResident.id);
      if (profile?.base_photos.some((photo) => photo.id === photoID)) {
        throw new Error("La modification photo n’a pas été confirmée par le backend.");
      }
      await data.refresh();
    } catch (error) {
      setNotice(error instanceof Error ? error.message : "Suppression photo impossible");
    } finally {
      setBusy(false);
    }
  }

  async function replaceFace(photoID: string, file: File, view: BaseFaceView) {
    if (!photoResident) return;
    setBusy(true);
    try {
      await replaceResidentBaseFace(photoResident.id, photoID, view, file);
      const profile = await refreshFace(photoResident.id);
      const replacement = profile?.base_photos.find((photo) => photo.id === photoID || photo.view === view);
      if (!replacement || replacement.view !== view) {
        throw new Error("La modification photo n’a pas été confirmée par le backend.");
      }
      await data.refresh();
    } catch (error) {
      setNotice(error instanceof Error ? error.message : "Remplacement photo impossible");
    } finally {
      setBusy(false);
    }
  }

  async function rebuildFace() {
    if (!photoResident) return;
    setBusy(true);
    try {
      await rebuildResidentFace(photoResident.id);
      const profile = await refreshFace(photoResident.id);
      if (!profile || (profile.base_photos.length > 0 && profile.status !== "ready")) {
        throw new Error("La modification photo n’a pas été confirmée par le backend.");
      }
      await data.refresh();
    } catch (error) {
      setNotice(error instanceof Error ? error.message : "Recalcul impossible");
    } finally {
      setBusy(false);
    }
  }

  async function acceptReview(cropID: string) {
    if (!photoResident) return;
    setBusy(true);
    try { await acceptResidentFaceReview(photoResident.id, cropID); await refreshFace(photoResident.id); }
    catch (error) { setNotice(error instanceof Error ? error.message : "Validation impossible"); }
    finally { setBusy(false); }
  }

  async function deleteReview(cropID: string) {
    if (!photoResident) return;
    setBusy(true);
    try { await deleteResidentFaceReview(photoResident.id, cropID); await refreshFace(photoResident.id); }
    catch (error) { setNotice(error instanceof Error ? error.message : "Suppression impossible"); }
    finally { setBusy(false); }
  }

  const present = residents.filter((resident) => resident.state === "present").length;
  const away = residents.filter((resident) => resident.state === "away").length;
  const trusted = residents.filter((resident) => resident.trusted).length;
  const admins = residents.filter((resident) => resident.admin).length;

  return (
    <div className="residents-layout">
      <div className="residents-stats">
        <Panel className="resident-stat-card">
          <div className="resident-stat-content">
            <div className="resident-stat-icon success">
              <UserCheck size={18} />
            </div>
            <div>
              <strong>{present}</strong>
              <span>Présent</span>
            </div>
          </div>
        </Panel>

        <Panel className="resident-stat-card">
          <div className="resident-stat-content">
            <div className="resident-stat-icon neutral">
              <UserMinus size={18} />
            </div>
            <div>
              <strong>{away}</strong>
              <span>Absent</span>
            </div>
          </div>
        </Panel>

        <Panel className="resident-stat-card">
          <div className="resident-stat-content">
            <div className="resident-stat-icon success">
              <BadgeCheck size={18} />
            </div>
            <div>
              <strong>{trusted}</strong>
              <span>Trusted</span>
            </div>
          </div>
        </Panel>

        <Panel className="resident-stat-card">
          <div className="resident-stat-content">
            <div className="resident-stat-icon warning">
              <Crown size={18} />
            </div>
            <div>
              <strong>{admins}</strong>
              <span>Admins</span>
            </div>
          </div>
        </Panel>
      </div>

      <Panel
        title="Résidents"
        className="residents-main-panel"
        action={auth.isAdmin ? (
          <button className="primary-button residents-add-button" onClick={openCreate}>
            <Plus size={16} />
            Ajouter
          </button>
        ) : undefined}
      >
        {data.error && <div className="auth-error" role="alert">{data.error} <button type="button" className="secondary-button" onClick={() => void data.refresh()}>Réessayer</button></div>}
        {notice && <div className="auth-error">{notice}</div>}
        <div className="residents-toolbar">
          <label className="resident-search">
            <Search size={16} />
            <input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder="Rechercher un résident, une pièce, un rôle..."
            />
          </label>

          <div className="resident-filters">
            <button
              className={stateFilter === "all" ? "active" : ""}
              onClick={() => setStateFilter("all")}
            >
              Tous
            </button>
            <button
              className={stateFilter === "present" ? "active" : ""}
              onClick={() => setStateFilter("present")}
            >
              Présents
            </button>
            <button
              className={stateFilter === "away" ? "active" : ""}
              onClick={() => setStateFilter("away")}
            >
              Absents
            </button>
            <button
              className={stateFilter === "unknown" ? "active" : ""}
              onClick={() => setStateFilter("unknown")}
            >
              Sans donnée
            </button>
          </div>

          <div className="resident-role-filter">
            <select
              value={roleFilter}
              onChange={(event) =>
                setRoleFilter(event.target.value as ResidentRoleFilter)
              }
            >
              <option value="all">Tous les rôles</option>
              <option value="owner">Propriétaires</option>
              <option value="resident">Résidents</option>
              <option value="guest">Invités</option>
              <option value="child">Enfants</option>
            </select>
          </div>
        </div>

        <div className="residents-grid">
          {filteredResidents.map((resident) => {
            const tone = stateTone(resident.state);
            const score = Math.round(resident.presence_score * 100);
            const confidenceTone = scoreTone(resident.presence_score);

            return (
              <article
                className={`resident-card resident-${tone}`}
                key={resident.id}
              >
                <div className="resident-card-header">
                  <div className={`resident-avatar ${tone}`}>
                    {initials(resident.name) || <User size={18} />}
                  </div>

                  <div className="resident-title">
                    <div className="resident-name-row">
                      <strong>{resident.name}</strong>

                      {resident.admin && (
                        <span className="resident-mini-badge admin">
                          <Crown size={12} />
                        </span>
                      )}

                      {resident.trusted && (
                        <span className="resident-mini-badge trusted">
                          <Shield size={12} />
                        </span>
                      )}
                    </div>

                    <span>{resident.id}</span>
                  </div>

                  <span className={`badge ${tone === "neutral" ? "" : tone}`}>
                    {stateLabel(resident.state)}
                  </span>
                </div>

                <p className="resident-summary">
                  {presenceDescription(resident)}
                </p>

                <div className="resident-meta-grid">
                  <div>
                    <span>
                      <Users size={13} />
                      Rôle
                    </span>
                    <strong>{roleLabel(resident.role)}</strong>
                  </div>

                  <div>
                    <span>
                      <MapPin size={13} />
                      Dernière pièce
                    </span>
                    <strong>{roomLabel(resident.node_id, data.topology)}</strong>
                  </div>

                  <div>
                    <span>
                      <Home size={13} />
                      Pièce de référence
                    </span>
                    <strong>{roomLabel(resident.reference_node_id ?? null, data.topology)}</strong>
                  </div>

                  <div>
                    <span>
                      <Clock size={13} />
                      Dernière vue
                    </span>
                    <strong>{formatRelativeDateTime(resident.last_seen)}</strong>
                  </div>

                  <div>
                    <span>
                      <Brain size={13} />
                      Présence
                    </span>
                    <strong>{score}%</strong>
                  </div>
                </div>

                <div className="resident-confidence">
                  <div className="resident-confidence-row">
                    <span>Confiance de présence</span>
                    <strong className={confidenceTone}>{score}%</strong>
                  </div>

                  <div className={`resident-meter ${confidenceTone}`}>
                    <span style={{ width: `${score}%` }} />
                  </div>
                </div>

                <div className="resident-card-footer">
                  <span className="resident-small-info">
                    {resident.trusted ? (
                      <>
                        <Shield size={14} />
                        Profil fiable
                      </>
                    ) : (
                      <>
                        <ShieldAlert size={14} />
                        Profil à vérifier
                      </>
                    )}
                  </span>

                  <div className="resident-admin-actions">
                    {resident.face_profile && <div className="resident-face-summary"><span className={`badge ${resident.face_profile.status === "ready" ? "success" : "warning"}`}>{faceStatusLabel(resident.face_profile.status)}</span><span className="resident-face-counter">{resident.face_profile.base_photos?.length ?? 0}/4 photos</span><span className="resident-face-counter">Auto {resident.face_profile.auto_count ?? 0}</span><span className="resident-face-counter">À valider {resident.face_profile.review_count ?? resident.face_profile.pending_count ?? 0}</span></div>}
                    {auth.isAdmin && (
                      <>
                        <button className="secondary-button resident-details-button" onClick={() => openEdit(resident)} disabled={busy}>
                          <Eye size={15} />
                          Modifier
                        </button>
                        <button className="secondary-button resident-details-button" onClick={() => openPhotos(resident)} disabled={busy}>
                          <ImagePlus size={15} />
                          Photos
                        </button>
                        <button className="icon-button danger" onClick={() => void removeResident(resident)} disabled={busy} aria-label={`Désactiver ${resident.name}`}>
                          <Trash2 size={15} />
                        </button>
                      </>
                    )}
                  </div>
                </div>
              </article>
            );
          })}
        </div>

        {filteredResidents.length === 0 && (
          <div className="empty-state">
            <Home size={24} />
            <strong>Aucun résident</strong>
            <span>{data.error ? "Les résidents ne sont pas disponibles." : "Aucun résident configuré ou aucun profil ne correspond aux filtres actifs."}</span>
            {auth.isAdmin && !data.error && <button type="button" className="primary-button" onClick={openCreate}>Ajouter un résident</button>}
          </div>
        )}
      </Panel>

      {editor && (
        <ResidentEditorModal
          value={editor}
          editing={Boolean(editingID)}
          rooms={flattenRooms(data.topology)}
          busy={busy}
          onChange={setEditor}
          onClose={() => {
            setEditor(null);
            setEditingID(null);
          }}
          onSubmit={saveResident}
        />
      )}

      {photoResident && (
        <ResidentPhotosModal
          resident={photoResident}
          profile={faceProfile}
          reviewPhotos={reviewPhotos}
          busy={busy}
          onClose={() => {
            setPhotoResident(null);
            setFaceProfile(null);
          }}
          onUpload={(file, view) => void uploadFace(file, view)}
          onReplace={(photoID, file, view) => void replaceFace(photoID, file, view)}
          onDelete={(photoID) => void removeFace(photoID)}
          onRebuild={() => void rebuildFace()}
          onAcceptReview={(cropID) => void acceptReview(cropID)}
          onDeleteReview={(cropID) => void deleteReview(cropID)}
        />
      )}
    </div>
  );
}

type RoomOption = { id: string; name: string; floor: string; zone: string };

function ResidentEditorModal({
  value,
  editing,
  rooms,
  busy,
  onChange,
  onClose,
  onSubmit,
}: {
  value: ResidentForm;
  editing: boolean;
  rooms: RoomOption[];
  busy: boolean;
  onChange: (value: ResidentForm) => void;
  onClose: () => void;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
}) {
  const set = <K extends keyof ResidentForm>(key: K, fieldValue: ResidentForm[K]) =>
    onChange({ ...value, [key]: fieldValue });

  return (
    <div className="resident-modal-backdrop" role="presentation">
      <form className="resident-modal" onSubmit={onSubmit}>
        <div className="resident-modal-header">
          <div>
            <strong>{editing ? "Modifier le résident" : "Ajouter un résident"}</strong>
            <span>Les données de compte restent dans auth.yaml.</span>
          </div>
          <button type="button" className="icon-button" onClick={onClose} aria-label="Fermer">
            <X size={18} />
          </button>
        </div>

        <div className="resident-form-grid">
          <label>
            <span>Id immuable</span>
            <input value={value.id} disabled={editing} onChange={(event) => set("id", event.target.value.toLowerCase())} required />
          </label>
          <label>
            <span>Prénom</span>
            <input value={value.first_name} onChange={(event) => set("first_name", event.target.value)} />
          </label>
          <label>
            <span>Nom</span>
            <input value={value.last_name} onChange={(event) => set("last_name", event.target.value)} />
          </label>
          <label>
            <span>Nom affiché</span>
            <input value={value.display_name} onChange={(event) => set("display_name", event.target.value)} />
          </label>
          <label>
            <span>Rôle maison</span>
            <select value={value.role} onChange={(event) => set("role", event.target.value as ResidentForm["role"]) }>
              <option value="owner">Propriétaire</option>
              <option value="resident">Résident</option>
              <option value="guest">Invité</option>
              <option value="child">Enfant</option>
            </select>
          </label>
          <label>
            <span>Pièce de référence</span>
            <select value={value.reference_node_id} onChange={(event) => set("reference_node_id", event.target.value)}>
              <option value="">Aucune</option>
              {rooms.map((room) => (
                <option value={room.id} key={room.id}>{room.name} · {room.floor}</option>
              ))}
            </select>
          </label>
          <label>
            <span>Compte auth lié</span>
            <input value={value.account_id} placeholder="user_alexis" onChange={(event) => set("account_id", event.target.value)} />
          </label>
        </div>

        <div className="resident-form-checks">
          <label><input type="checkbox" checked={value.admin} onChange={(event) => set("admin", event.target.checked)} /> Admin applicatif</label>
          <label><input type="checkbox" checked={value.trusted} onChange={(event) => set("trusted", event.target.checked)} /> Profil trusted</label>
        </div>

        <div className="resident-modal-actions">
          <button type="button" className="secondary-button" onClick={onClose}>Annuler</button>
          <button type="submit" className="primary-button" disabled={busy}>{busy ? "Enregistrement…" : "Enregistrer"}</button>
        </div>
      </form>
    </div>
  );
}

function ResidentPhotosModal({
  resident,
  profile,
  reviewPhotos,
  busy,
  onClose,
  onUpload,
  onReplace,
  onDelete,
  onRebuild,
  onAcceptReview,
  onDeleteReview,
}: {
  resident: DemoResident;
  profile: SynoraFaceProfile | null;
  reviewPhotos: import("../lib/synora-types").SynoraFacePhoto[];
  busy: boolean;
  onClose: () => void;
  onUpload: (file: File, view: BaseFaceView) => void;
  onReplace: (photoID: string, file: File, view: BaseFaceView) => void;
  onDelete: (photoID: string) => void;
  onRebuild: () => void;
  onAcceptReview: (cropID: string) => void;
  onDeleteReview: (cropID: string) => void;
}) {
  const photos = profile?.base_photos ?? resident.face_profile?.base_photos ?? [];
  const baseCount = BASE_FACE_VIEWS.filter(({ id }) => Boolean(photos.find((photo) => photo.view === id))).length;
  const complete = isBaseComplete(profile ?? resident.face_profile);
  const nextView = BASE_FACE_VIEWS.find(({ id }) => !photos.some((photo) => photo.view === id));
  const chooseFile = (callback: (file: File) => void) => (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (file) callback(file);
  };

  return (
    <div className="resident-modal-backdrop" role="presentation">
      <section className="resident-modal resident-photos-modal">
        <div className="resident-modal-header">
          <div>
            <strong>Configurer la base faciale · {resident.name}</strong>
            <span>{complete ? "Base complète" : `Progression : ${baseCount}/4`} · {faceStatusLabel(profile?.status ?? resident.face_profile?.status)}</span>
          </div>
          <button type="button" className="icon-button" onClick={onClose} aria-label="Fermer"><X size={18} /></button>
        </div>

        <div className={`face-setup-progress ${complete ? "complete" : ""}`}>
          <strong>{complete ? "Base complète" : `Prochain slot : ${nextView?.label ?? "—"}`}</strong>
          <span>Ajoutez les quatre vues pour initialiser la reconnaissance faciale.</span>
        </div>

        <div className="face-photo-grid">
          {BASE_FACE_VIEWS.map((view) => {
            const photo = photos.find((candidate) => candidate.view === view.id);
            const inputID = `face-upload-${resident.id}-${view.id}`;
            return photo ? (
              <div className="face-photo-slot" key={view.id}>
                <strong>{view.label}</strong>
                <img src={getResidentFaceImageUrl(resident.id, photo.id)} alt={`Vue ${view.label} de ${resident.name}`} />
                <small>{view.help}</small>
                <small>{photo.filename}</small>
                <div>
                  <label className="secondary-button">Remplacer<input type="file" accept="image/*" capture="user" hidden onChange={chooseFile((file) => onReplace(photo.id, file, view.id))} disabled={busy} /></label>
                  <button type="button" className="icon-button danger" onClick={() => onDelete(photo.id)} disabled={busy} aria-label="Supprimer photo"><Trash2 size={15} /></button>
                </div>
              </div>
            ) : (
              <label className="face-photo-empty" htmlFor={inputID} key={view.id}>
                <strong>{view.label}</strong><ImagePlus size={22} /><span>{view.help}</span><small>Ajouter une photo</small>
                <input id={inputID} type="file" accept="image/*" capture="user" hidden onChange={chooseFile((file) => onUpload(file, view.id))} disabled={busy} />
              </label>
            );
          })}
        </div>

        <div className="face-pending-box">
          <strong>Enrichissement automatique</strong>
          <span>Auto : {profile?.auto_count ?? resident.face_profile?.auto_count ?? 0} photo(s)</span>
          <strong>Photos à valider · {reviewPhotos.length}</strong>
          {reviewPhotos.length === 0 ? <small>Aucune photo en attente.</small> : reviewPhotos.map((photo) => (
            <div className="face-review-row" key={photo.id}>
              <img src={getResidentFaceReviewImageUrl(resident.id, photo.id)} alt="Crop à valider" />
              <span>{photo.filename}</span>
              <button type="button" className="secondary-button" onClick={() => onAcceptReview(photo.id)} disabled={busy}>Accepter</button>
              <button type="button" className="icon-button danger" onClick={() => onDeleteReview(photo.id)} disabled={busy} aria-label="Supprimer crop"><Trash2 size={15} /></button>
            </div>
          ))}
        </div>

        <div className="resident-modal-actions">
          <button type="button" className="secondary-button" onClick={onRebuild} disabled={busy}><RefreshCw size={15} /> Recalculer le profil</button>
          <button type="button" className="primary-button" onClick={onClose}>Fermer</button>
        </div>
      </section>
    </div>
  );
}
