import {
  BadgeCheck,
  Brain,
  Clock,
  Crown,
  Eye,
  Home,
  MapPin,
  Plus,
  Search,
  Shield,
  ShieldAlert,
  User,
  UserCheck,
  UserMinus,
  Users,
} from "lucide-react";
import { useMemo, useState } from "react";
import { Panel } from "../components/Panel";
import {
  demoApiTopology,
  demoResidents,
  prettyTopologyName,
  type DemoResident,
} from "../data/demo";

type ResidentStateFilter = "all" | DemoResident["state"];
type ResidentRoleFilter = "all" | DemoResident["role"];

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

  return "Inconnu";
}

function stateTone(state: DemoResident["state"]) {
  if (state === "present") return "success";
  if (state === "away") return "neutral";

  return "warning";
}

function scoreTone(score: number) {
  if (score >= 0.75) return "success";
  if (score >= 0.35) return "warning";

  return "danger";
}

function initials(name: string) {
  return name
    .split(" ")
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase())
    .join("");
}

function flattenRooms() {
  return demoApiTopology.flatMap((zone) =>
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

function roomLabel(nodeId: string | null) {
  if (!nodeId) return "Aucune pièce connue";

  const room = flattenRooms().find((item) => item.id === nodeId);

  if (!room) return nodeId;

  return `${room.name} · ${room.floor}`;
}

function presenceDescription(resident: DemoResident) {
  if (resident.state === "present") {
    return `${resident.name} est reconnu dans la maison.`;
  }

  if (resident.state === "away") {
    return `${resident.name} n’est pas actuellement détecté.`;
  }

  return "Aucune présence fiable disponible.";
}

export function Residents() {
  const [search, setSearch] = useState("");
  const [stateFilter, setStateFilter] = useState<ResidentStateFilter>("all");
  const [roleFilter, setRoleFilter] = useState<ResidentRoleFilter>("all");

  const residents = demoResidents;

  const filteredResidents = useMemo(() => {
    const query = search.trim().toLowerCase();

    return residents.filter((resident) => {
      const matchSearch =
        query.length === 0 ||
        resident.id.toLowerCase().includes(query) ||
        resident.name.toLowerCase().includes(query) ||
        roleLabel(resident.role).toLowerCase().includes(query) ||
        roomLabel(resident.node_id).toLowerCase().includes(query);

      const matchState =
        stateFilter === "all" || resident.state === stateFilter;

      const matchRole = roleFilter === "all" || resident.role === roleFilter;

      return matchSearch && matchState && matchRole;
    });
  }, [residents, search, stateFilter, roleFilter]);

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
        action={
          <button className="primary-button residents-add-button">
            <Plus size={16} />
            Ajouter
          </button>
        }
      >
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
              Inconnus
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
                    <strong>{roomLabel(resident.node_id)}</strong>
                  </div>

                  <div>
                    <span>
                      <Clock size={13} />
                      Dernière vue
                    </span>
                    <strong>{resident.last_seen ?? "Jamais"}</strong>
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

                  <button className="secondary-button resident-details-button">
                    <Eye size={15} />
                    Détails
                  </button>
                </div>
              </article>
            );
          })}
        </div>

        {filteredResidents.length === 0 && (
          <div className="empty-state">
            <Home size={24} />
            <strong>Aucun résident</strong>
            <span>Aucun profil ne correspond aux filtres actifs.</span>
          </div>
        )}
      </Panel>
    </div>
  );
}