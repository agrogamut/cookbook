import type { ReactNode } from "react";

interface PageHeaderProps {
  title: string;
  description?: string;
  meta?: ReactNode;
}

export function PageHeader({ title, description, meta }: PageHeaderProps) {
  return (
    <div className="mb-5 flex items-start justify-between gap-4 border-b pb-3">
      <div className="space-y-0.5">
        <h1 className="font-mono text-base font-medium tracking-tight">{title}</h1>
        {description && <p className="text-xs text-muted-foreground">{description}</p>}
      </div>
      {meta && <div className="shrink-0 text-xs text-muted-foreground">{meta}</div>}
    </div>
  );
}
