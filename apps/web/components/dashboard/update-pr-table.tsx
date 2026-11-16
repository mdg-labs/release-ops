"use client";

import { useFormatter, useTranslations } from "next-intl";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { formatAppDateTime } from "@/lib/format/datetime";
import type {
  UpdatePr,
  UpdatePrTicket,
  UpdatePrTicketState,
  UpdatePrType,
} from "@/lib/query/types";

export type UpdatePrTimeColumn = "age" | "mergedAt";

const TYPE_VARIANTS: Record<UpdatePrType, BadgeProps["variant"]> = {
  major: "warning",
  minor: "info",
  patch: "success",
  digest: "outline",
  lockfile: "outline",
  other: "outline",
};

const TICKET_VARIANTS: Record<UpdatePrTicketState, BadgeProps["variant"]> = {
  pending: "outline",
  open: "info",
  done: "success",
  cancelled: "secondary",
  superseded: "secondary",
};

/** An external link is only rendered for an http(s) address. */
function webUrl(value: string | null): string | null {
  if (!value) {
    return null;
  }
  try {
    const url = new URL(value);
    return url.protocol === "https:" || url.protocol === "http:" ? value : null;
  } catch {
    return null;
  }
}

function ExternalLink({
  href,
  children,
}: {
  href: string | null;
  children: React.ReactNode;
}): React.ReactElement {
  const safe = webUrl(href);
  if (!safe) {
    return <>{children}</>;
  }
  return (
    <a
      className="underline-offset-4 hover:underline"
      href={safe}
      rel="noopener noreferrer"
      target="_blank"
    >
      {children}
    </a>
  );
}

function TicketCell({
  ticket,
}: {
  ticket: UpdatePrTicket | null;
}): React.ReactElement {
  const t = useTranslations("dashboard");

  if (!ticket) {
    return <span className="text-muted-foreground">{t("noTicket")}</span>;
  }
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {ticket.externalId ? (
        <ExternalLink href={ticket.url}>{ticket.externalId}</ExternalLink>
      ) : null}
      <Badge variant={TICKET_VARIANTS[ticket.state]}>
        {t(`ticketStates.${ticket.state}`)}
      </Badge>
      {ticket.hasError ? (
        <Badge variant="error">{t("ticketError")}</Badge>
      ) : null}
    </div>
  );
}

function TimeCell({
  pr,
  column,
}: {
  pr: UpdatePr;
  column: UpdatePrTimeColumn;
}): React.ReactElement {
  const t = useTranslations("dashboard");
  const format = useFormatter();
  const value = column === "age" ? pr.openedAt : pr.mergedAt;

  if (!value) {
    return <span className="text-muted-foreground">{t("noDate")}</span>;
  }
  if (column === "age") {
    return (
      <time dateTime={value} title={formatAppDateTime(format, value, "")}>
        {format.relativeTime(new Date(value), new Date())}
      </time>
    );
  }
  return <time dateTime={value}>{formatAppDateTime(format, value, "")}</time>;
}

export function UpdatePrTable({
  items,
  timeColumn,
}: {
  items: UpdatePr[];
  timeColumn: UpdatePrTimeColumn;
}): React.ReactElement {
  const t = useTranslations("dashboard");

  return (
    <Table variant="card">
      <TableHeader>
        <TableRow>
          <TableHead>{t("columns.repository")}</TableHead>
          <TableHead>{t("columns.title")}</TableHead>
          <TableHead>{t("columns.updateTypes")}</TableHead>
          <TableHead>
            {timeColumn === "age" ? t("columns.age") : t("columns.mergedAt")}
          </TableHead>
          <TableHead>{t("columns.ticket")}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {items.map((pr) => (
          <TableRow key={pr.id}>
            <TableCell className="whitespace-nowrap">
              {pr.repository.fullName}
            </TableCell>
            <TableCell className="max-w-96 truncate font-medium">
              <ExternalLink href={pr.url}>{pr.title}</ExternalLink>
            </TableCell>
            <TableCell>
              <div className="flex flex-wrap gap-1">
                {pr.updateTypes.length === 0 ? (
                  <Badge variant="outline">{t("updateTypes.unknown")}</Badge>
                ) : (
                  pr.updateTypes.map((type) => (
                    <Badge key={type} variant={TYPE_VARIANTS[type]}>
                      {t(`updateTypes.${type}`)}
                    </Badge>
                  ))
                )}
              </div>
            </TableCell>
            <TableCell className="whitespace-nowrap">
              <TimeCell column={timeColumn} pr={pr} />
            </TableCell>
            <TableCell>
              <TicketCell ticket={pr.ticket} />
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
