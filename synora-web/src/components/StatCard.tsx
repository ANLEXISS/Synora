type StatCardProps = {
  title: string;
  value: string | number;
  label: string;
  tone?: "neutral" | "success" | "warning" | "danger";
};

export function StatCard({
  title,
  value,
  label,
  tone = "neutral",
}: StatCardProps) {
  return (
    <section className={`panel card-stat stat-card stat-${tone}`}>
      <div className="panel-header">
        <h2 className="panel-title">{title}</h2>
      </div>

      <div className="panel-body">
        <div className="stat-value">{value}</div>
        <div className="stat-label">{label}</div>
      </div>
    </section>
  );
}