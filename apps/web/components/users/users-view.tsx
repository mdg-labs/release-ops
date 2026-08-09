"use client";

import { useFormatter, useTranslations } from "next-intl";
import { useState } from "react";
import { MailIcon, Trash2Icon, UserXIcon } from "lucide-react";
import { DeleteUserDialog } from "@/components/users/delete-user-dialog";
import { InviteDialog } from "@/components/users/invite-dialog";
import { RevokeInviteDialog } from "@/components/users/revoke-invite-dialog";
import { SettingsNav } from "@/components/settings/settings-nav";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Frame, FramePanel } from "@/components/ui/frame";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { toastManager } from "@/components/ui/toast";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { ApiError } from "@/lib/api/client";
import { formatAppDateTime } from "@/lib/format/datetime";
import { useSettings } from "@/lib/hooks/use-settings";
import { useSession } from "@/lib/hooks/use-session";
import { useInvitations } from "@/hooks/useInvitations";
import { useUsers } from "@/hooks/useUsers";
import type { Invitation, UserListItem } from "@/lib/query/types";

function formatDate(
  value: string,
  format: ReturnType<typeof useFormatter>,
): string {
  return formatAppDateTime(format, value, value);
}

export function UsersView(): React.ReactElement {
  const t = useTranslations("users");
  const format = useFormatter();
  const { data: session } = useSession();
  const { data: settings } = useSettings();
  const {
    users,
    isLoading: usersLoading,
    isError: usersError,
    deleteUser,
  } = useUsers();
  const {
    invitations,
    isLoading: invitationsLoading,
    isError: invitationsError,
    revokeInvitation,
    sendInvitationEmail,
  } = useInvitations();

  const [inviteOpen, setInviteOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<UserListItem | null>(null);
  const [revokeTarget, setRevokeTarget] = useState<Invitation | null>(null);

  const currentUserId = session?.user?.id ?? null;
  const smtpConfigured = settings?.smtpConfigured ?? false;
  const isLastUser = users.length <= 1;

  async function copyInviteLink(url: string): Promise<void> {
    try {
      await navigator.clipboard.writeText(url);
      toastManager.add({
        title: t("inviteLinkCopied"),
        type: "success",
      });
    } catch {
      toastManager.add({
        title: t("inviteLinkCopyFailed"),
        type: "error",
      });
    }
  }

  function handleSendEmail(invitation: Invitation): void {
    toastManager.promise(sendInvitationEmail.mutateAsync(invitation.id), {
      loading: {
        title: t("sendEmailLoading"),
        description: t("sendEmailLoadingDescription"),
      },
      success: {
        title: t("sendEmailSuccess"),
        description: t("sendEmailSuccessDescription"),
      },
      error: (error: Error) => ({
        title: t("sendEmailFailed"),
        description:
          error instanceof ApiError
            ? error.message
            : t("sendEmailFailedDescription"),
      }),
    });
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="font-semibold text-2xl">{t("title")}</h1>
        <Button onClick={() => setInviteOpen(true)}>{t("invite")}</Button>
      </div>
      <SettingsNav />

      {usersError || invitationsError ? (
        <p className="text-destructive-foreground text-sm" role="alert">
          {t("loadFailed")}
        </p>
      ) : null}

      <Frame>
        <FramePanel className="p-0">
          <div className="border-b px-6 py-4">
            <h2 className="font-medium text-base">{t("usersSection")}</h2>
            <p className="text-muted-foreground text-sm">
              {t("usersSectionDescription")}
            </p>
          </div>
          <Table variant="card">
            <TableHeader>
              <TableRow>
                <TableHead>{t("columns.email")}</TableHead>
                <TableHead>{t("columns.createdAt")}</TableHead>
                <TableHead className="text-end">
                  {t("columns.actions")}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {usersLoading
                ? Array.from({ length: 2 }, (_, index) => (
                    <TableRow key={`user-skeleton-${index}`}>
                      <TableCell colSpan={3}>
                        <Skeleton className="h-8 w-full" />
                      </TableCell>
                    </TableRow>
                  ))
                : null}

              {!usersLoading && users.length === 0 ? (
                <TableRow>
                  <TableCell className="text-muted-foreground" colSpan={3}>
                    {t("usersEmpty")}
                  </TableCell>
                </TableRow>
              ) : null}

              {!usersLoading
                ? users.map((user) => {
                    const isSelf = user.id === currentUserId;
                    const cannotDelete = isSelf || isLastUser;

                    return (
                      <TableRow key={user.id}>
                        <TableCell>
                          <div className="flex items-center gap-2">
                            <span>{user.email}</span>
                            {isSelf ? (
                              <Badge variant="secondary">{t("you")}</Badge>
                            ) : null}
                          </div>
                        </TableCell>
                        <TableCell>
                          {formatDate(user.createdAt, format)}
                        </TableCell>
                        <TableCell className="text-end">
                          {cannotDelete ? (
                            <Tooltip>
                              <TooltipTrigger
                                render={
                                  <Button
                                    aria-label={t("removeAria", {
                                      email: user.email,
                                    })}
                                    disabled
                                    size="icon"
                                    variant="ghost"
                                  />
                                }
                              >
                                <UserXIcon aria-hidden="true" />
                              </TooltipTrigger>
                              <TooltipPopup>
                                {isSelf
                                  ? t("removeSelfDisabled")
                                  : t("removeLastUserDisabled")}
                              </TooltipPopup>
                            </Tooltip>
                          ) : (
                            <Button
                              aria-label={t("removeAria", {
                                email: user.email,
                              })}
                              onClick={() => setDeleteTarget(user)}
                              size="icon"
                              variant="ghost"
                            >
                              <UserXIcon aria-hidden="true" />
                            </Button>
                          )}
                        </TableCell>
                      </TableRow>
                    );
                  })
                : null}
            </TableBody>
          </Table>
        </FramePanel>
      </Frame>

      <Frame>
        <FramePanel className="p-0">
          <div className="border-b px-6 py-4">
            <h2 className="font-medium text-base">{t("invitationsSection")}</h2>
            <p className="text-muted-foreground text-sm">
              {t("invitationsSectionDescription")}
            </p>
          </div>
          <Table variant="card">
            <TableHeader>
              <TableRow>
                <TableHead>{t("columns.email")}</TableHead>
                <TableHead>{t("columns.expiresAt")}</TableHead>
                <TableHead className="text-end">
                  {t("columns.actions")}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {invitationsLoading
                ? Array.from({ length: 2 }, (_, index) => (
                    <TableRow key={`invite-skeleton-${index}`}>
                      <TableCell colSpan={3}>
                        <Skeleton className="h-8 w-full" />
                      </TableCell>
                    </TableRow>
                  ))
                : null}

              {!invitationsLoading && invitations.length === 0 ? (
                <TableRow>
                  <TableCell className="text-muted-foreground" colSpan={3}>
                    {t("invitationsEmpty")}
                  </TableCell>
                </TableRow>
              ) : null}

              {!invitationsLoading
                ? invitations.map((invitation) => (
                    <TableRow key={invitation.id}>
                      <TableCell>{invitation.email}</TableCell>
                      <TableCell>
                        {formatDate(invitation.expiresAt, format)}
                      </TableCell>
                      <TableCell>
                        <div className="flex justify-end gap-1">
                          {smtpConfigured ? (
                            <Button
                              aria-label={t("sendEmailAria", {
                                email: invitation.email,
                              })}
                              loading={sendInvitationEmail.isPending}
                              onClick={() => handleSendEmail(invitation)}
                              size="icon"
                              variant="ghost"
                            >
                              <MailIcon aria-hidden="true" />
                            </Button>
                          ) : (
                            <Tooltip>
                              <TooltipTrigger
                                render={
                                  <Button
                                    aria-label={t("sendEmailAria", {
                                      email: invitation.email,
                                    })}
                                    disabled
                                    size="icon"
                                    variant="ghost"
                                  />
                                }
                              >
                                <MailIcon aria-hidden="true" />
                              </TooltipTrigger>
                              <TooltipPopup>
                                {t("sendEmailSmtpDisabled")}
                              </TooltipPopup>
                            </Tooltip>
                          )}
                          <Button
                            aria-label={t("revokeAria", {
                              email: invitation.email,
                            })}
                            onClick={() => setRevokeTarget(invitation)}
                            size="icon"
                            variant="ghost"
                          >
                            <Trash2Icon aria-hidden="true" />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))
                : null}
            </TableBody>
          </Table>
        </FramePanel>
      </Frame>

      <InviteDialog
        onCopyLink={copyInviteLink}
        onOpenChange={setInviteOpen}
        open={inviteOpen}
      />

      <DeleteUserDialog
        isDeleting={deleteUser.isPending}
        onConfirm={() => {
          if (!deleteTarget) {
            return;
          }
          void deleteUser.mutateAsync(deleteTarget.id).then(() => {
            setDeleteTarget(null);
          });
        }}
        onOpenChange={(open) => {
          if (!open) {
            setDeleteTarget(null);
          }
        }}
        open={deleteTarget !== null}
        user={deleteTarget}
      />

      <RevokeInviteDialog
        invitation={revokeTarget}
        isRevoking={revokeInvitation.isPending}
        onConfirm={() => {
          if (!revokeTarget) {
            return;
          }
          void revokeInvitation.mutateAsync(revokeTarget.id).then(() => {
            setRevokeTarget(null);
          });
        }}
        onOpenChange={(open) => {
          if (!open) {
            setRevokeTarget(null);
          }
        }}
        open={revokeTarget !== null}
      />
    </div>
  );
}
