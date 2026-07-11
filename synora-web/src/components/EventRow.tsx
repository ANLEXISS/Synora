import { AlertTriangle, Camera, CircleDot, ShieldAlert } from "lucide-react";

type EventRowProps = {
  type: string;
  title: string;
  subtitle: string;
  tone?: "neutral" | "warning" | "danger";
};

export function EventRow({
  type,
  title,
  subtitle,
  tone = "neutral",
}: EventRowProps) {
  const Icon =
    tone === "danger" ? ShieldAlert : tone === "warning" ? AlertTriangle : Camera;

  return (
    <div className="event-row">
      <div className={`event-icon ${tone}`}>
        <Icon size={18} />
      </div>

      <div className="event-main">
        <strong>{title}</strong>
        <span>{subtitle}</span>
      </div>

      <span className={`badge ${tone}`}>
        <CircleDot size={10} />
        {type}
      </span>
    </div>
  );
}