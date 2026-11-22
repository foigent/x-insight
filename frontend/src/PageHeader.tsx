import type { ReactNode } from "react";

export function PageHeader({
  eyebrow,
  title,
  titleID,
  description,
  action,
}: {
  eyebrow: string;
  title: string;
  titleID?: string;
  description: string;
  action?: ReactNode;
}) {
  return (
    <header className="page-header">
      <div>
        <span className="eyebrow">{eyebrow}</span>
        <h1 id={titleID}>{title}</h1>
        <p>{description}</p>
      </div>
      {action ? <div className="page-header-action">{action}</div> : null}
    </header>
  );
}
