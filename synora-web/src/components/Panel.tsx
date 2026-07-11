import type { ReactNode } from "react";

type PanelProps = {
  title?: string;
  action?: ReactNode;
  className?: string;
  children: ReactNode;
};

export function Panel({ title, action, className = "", children }: PanelProps) {
  return (
    <section className={`panel ${className}`}>
      {(title || action) && (
        <div className="panel-header">
          {title && <h2 className="panel-title">{title}</h2>}
          {action}
        </div>
      )}

      <div className="panel-body">{children}</div>
    </section>
  );
}