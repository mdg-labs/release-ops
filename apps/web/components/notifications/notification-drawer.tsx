"use client";

import { useTranslations } from "next-intl";
import { useEffect, useId, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { CheckboxGroup } from "@/components/ui/checkbox-group";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Sheet,
  SheetClose,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetPanel,
  SheetPopup,
  SheetTitle,
} from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { toastManager } from "@/components/ui/toast";
import { ApiError } from "@/lib/api/client";
import {
  DEFAULT_NOTIFICATION_EVENTS,
  NOTIFICATION_EVENT_OPTIONS,
  type NotificationEvent,
} from "@/lib/notifications/events";
import type {
  CreateNotificationTargetInput,
  NotificationTarget,
  UpdateNotificationTargetInput,
} from "@/lib/query/types";

type NotificationDrawerMode = "create" | "edit";

type NotificationDrawerProps = {
  mode: NotificationDrawerMode;
  target: NotificationTarget | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreate: (
    input: CreateNotificationTargetInput,
  ) => Promise<NotificationTarget>;
  onUpdate: (
    id: string,
    input: UpdateNotificationTargetInput,
  ) => Promise<NotificationTarget>;
  onTest: (id: string) => Promise<{ success: boolean; message?: string }>;
  isSaving: boolean;
};

export function NotificationDrawer({
  mode,
  target,
  open,
  onOpenChange,
  onCreate,
  onUpdate,
  onTest,
  isSaving,
}: NotificationDrawerProps): React.ReactElement {
  const t = useTranslations("notifications");
  const tCommon = useTranslations("common");
  const nameId = useId();
  const shoutrrrUrlId = useId();
  const enabledId = useId();

  const [name, setName] = useState("");
  const [shoutrrrUrl, setShoutrrrUrl] = useState("");
  const [events, setEvents] = useState<NotificationEvent[]>(
    DEFAULT_NOTIFICATION_EVENTS,
  );
  const [enabled, setEnabled] = useState(true);
  const [formError, setFormError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) {
      return;
    }

    setFormError(null);
    if (mode === "edit" && target) {
      setName(target.name);
      setShoutrrrUrl("");
      setEvents(
        target.events.filter((event): event is NotificationEvent =>
          NOTIFICATION_EVENT_OPTIONS.includes(event as NotificationEvent),
        ),
      );
      setEnabled(target.enabled);
      return;
    }

    setName("");
    setShoutrrrUrl("");
    setEvents(DEFAULT_NOTIFICATION_EVENTS);
    setEnabled(true);
  }, [open, mode, target]);

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFormError(null);

    const trimmedName = name.trim();
    if (!trimmedName) {
      setFormError(t("validation.nameRequired"));
      return;
    }

    const trimmedUrl = shoutrrrUrl.trim();
    const urlRequired = mode === "create" || !target?.hasSecret;
    if (urlRequired && !trimmedUrl) {
      setFormError(t("validation.urlRequired"));
      return;
    }

    if (events.length === 0) {
      setFormError(t("validation.eventsRequired"));
      return;
    }

    try {
      if (mode === "create") {
        await onCreate({
          name: trimmedName,
          shoutrrrUrl: trimmedUrl,
          events,
          enabled,
        });
      } else if (target) {
        await onUpdate(target.id, {
          name: trimmedName,
          shoutrrrUrl: trimmedUrl ? trimmedUrl : null,
          events,
          enabled,
        });
      }
      onOpenChange(false);
    } catch (error) {
      if (error instanceof ApiError) {
        setFormError(error.message);
      } else {
        setFormError(t("saveFailed"));
      }
    }
  }

  function handleTestNotification() {
    if (!target) {
      return;
    }

    toastManager.promise(
      (async () => {
        const result = await onTest(target.id);
        if (!result.success) {
          throw new Error(result.message ?? t("testFailedDescription"));
        }
        return result;
      })(),
      {
        loading: {
          title: t("testLoading"),
          description: t("testLoadingDescription"),
        },
        success: (result) => ({
          title: t("testSuccess"),
          description: result.message ?? t("testSuccessDescription"),
        }),
        error: (error: Error) => ({
          title: t("testFailed"),
          description: error.message || t("testFailedDescription"),
        }),
      },
    );
  }

  return (
    <Sheet onOpenChange={onOpenChange} open={open}>
      <SheetPopup side="right">
        <SheetHeader>
          <SheetTitle>
            {mode === "create" ? t("createTitle") : t("editTitle")}
          </SheetTitle>
          <SheetDescription>
            {mode === "create" ? t("createDescription") : t("editDescription")}
          </SheetDescription>
        </SheetHeader>
        <form className="flex min-h-0 flex-1 flex-col" onSubmit={handleSubmit}>
          <SheetPanel className="flex flex-col gap-4">
            {formError ? (
              <p className="text-destructive-foreground text-sm" role="alert">
                {formError}
              </p>
            ) : null}

            <Field name="name">
              <FieldLabel htmlFor={nameId}>
                {t("name")} <span aria-hidden="true">*</span>
              </FieldLabel>
              <Input
                id={nameId}
                name="name"
                onChange={(event) => setName(event.target.value)}
                required
                value={name}
              />
            </Field>

            <Field name="shoutrrrUrl">
              <FieldLabel htmlFor={shoutrrrUrlId}>
                {t("shoutrrrUrl")}{" "}
                {mode === "create" || !target?.hasSecret ? (
                  <span aria-hidden="true">*</span>
                ) : null}
              </FieldLabel>
              {mode === "edit" && target?.hasSecret ? (
                <div className="flex items-center gap-2">
                  <Badge variant="success">{t("urlConfigured")}</Badge>
                </div>
              ) : null}
              <Input
                autoComplete="new-password"
                id={shoutrrrUrlId}
                name="shoutrrrUrl"
                onChange={(event) => setShoutrrrUrl(event.target.value)}
                placeholder={
                  mode === "edit" && target?.hasSecret
                    ? t("urlPlaceholderEdit")
                    : t("urlPlaceholder")
                }
                required={mode === "create" || !target?.hasSecret}
                type="password"
                value={shoutrrrUrl}
              />
              {mode === "edit" && target?.hasSecret ? (
                <FieldDescription>{t("urlEditHint")}</FieldDescription>
              ) : (
                <FieldDescription>{t("urlHint")}</FieldDescription>
              )}
            </Field>

            <Field name="events">
              <FieldLabel>{t("events")}</FieldLabel>
              <FieldDescription>{t("eventsHint")}</FieldDescription>
              <CheckboxGroup
                aria-label={t("events")}
                onValueChange={(value) => {
                  setEvents(value as NotificationEvent[]);
                }}
                value={events}
              >
                {NOTIFICATION_EVENT_OPTIONS.map((event) => (
                  <Label key={event}>
                    <Checkbox
                      aria-label={t(`eventOptions.${event}`)}
                      value={event}
                    />
                    {t(`eventOptions.${event}`)}
                  </Label>
                ))}
              </CheckboxGroup>
            </Field>

            <Field name="enabled">
              <div className="flex items-center justify-between gap-4">
                <div className="flex flex-col gap-1">
                  <FieldLabel htmlFor={enabledId}>{t("enabled")}</FieldLabel>
                  <FieldDescription>{t("enabledHint")}</FieldDescription>
                </div>
                <Switch
                  checked={enabled}
                  id={enabledId}
                  onCheckedChange={setEnabled}
                />
              </div>
            </Field>
          </SheetPanel>
          <SheetFooter variant="bare">
            {mode === "edit" && target ? (
              <Button
                onClick={handleTestNotification}
                type="button"
                variant="outline"
              >
                {t("testNotification")}
              </Button>
            ) : null}
            <SheetClose render={<Button variant="ghost" type="button" />}>
              {tCommon("cancel")}
            </SheetClose>
            <Button loading={isSaving} type="submit">
              {tCommon("save")}
            </Button>
          </SheetFooter>
        </form>
      </SheetPopup>
    </Sheet>
  );
}
