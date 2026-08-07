"use client";

import { useTranslations } from "next-intl";
import { useEffect, useId, useState } from "react";
import { Button } from "@/components/ui/button";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectItem,
  SelectPopup,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
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
import { Badge } from "@/components/ui/badge";
import { toastManager } from "@/components/ui/toast";
import { ApiError } from "@/lib/api/client";
import {
  INTEGRATION_KINDS,
  kindIsJira,
  kindRequiresBaseUrl,
  kindUsesApiKeyLabel,
  type IntegrationKind,
} from "@/lib/integrations/kinds";
import { buildIntegrationSecret } from "@/lib/integrations/secret";
import type { Integration } from "@/lib/query/types";

type IntegrationDrawerMode = "create" | "edit";

type IntegrationDrawerProps = {
  mode: IntegrationDrawerMode;
  integration: Integration | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreate: (input: {
    kind: string;
    name: string;
    baseUrl?: string | null;
    secret: string;
  }) => Promise<Integration>;
  onUpdate: (
    id: string,
    input: {
      name: string;
      baseUrl?: string | null;
      secret?: string | null;
    },
  ) => Promise<Integration>;
  onTest: (id: string) => Promise<{ success: boolean; message?: string }>;
  isSaving: boolean;
};

type KindOption = { label: string; value: IntegrationKind };

export function IntegrationDrawer({
  mode,
  integration,
  open,
  onOpenChange,
  onCreate,
  onUpdate,
  onTest,
  isSaving,
}: IntegrationDrawerProps): React.ReactElement {
  const t = useTranslations("integrations");
  const tCommon = useTranslations("common");
  const nameId = useId();
  const baseUrlId = useId();
  const emailId = useId();
  const secretId = useId();

  const [kind, setKind] = useState<IntegrationKind>("github");
  const [name, setName] = useState("");
  const [baseUrl, setBaseUrl] = useState("");
  const [email, setEmail] = useState("");
  const [secret, setSecret] = useState("");
  const [formError, setFormError] = useState<string | null>(null);

  const kindItems: KindOption[] = INTEGRATION_KINDS.map((value) => ({
    label: t(`kinds.${value}`),
    value,
  }));

  const activeKind = mode === "edit" ? (integration?.kind ?? kind) : kind;
  const requiresBaseUrl = kindRequiresBaseUrl(activeKind);
  const showEmail = kindIsJira(activeKind);
  const secretLabel = kindUsesApiKeyLabel(activeKind)
    ? t("apiKey")
    : kindIsJira(activeKind)
      ? t("apiToken")
      : t("token");

  useEffect(() => {
    if (!open) {
      return;
    }

    setFormError(null);
    if (mode === "edit" && integration) {
      setKind(integration.kind as IntegrationKind);
      setName(integration.name);
      setBaseUrl(integration.baseUrl ?? "");
      setEmail("");
      setSecret("");
      return;
    }

    setKind("github");
    setName("");
    setBaseUrl("");
    setEmail("");
    setSecret("");
  }, [open, mode, integration]);

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFormError(null);

    const trimmedName = name.trim();
    if (!trimmedName) {
      setFormError(t("validation.nameRequired"));
      return;
    }

    const trimmedBaseUrl = baseUrl.trim();
    if (requiresBaseUrl && !trimmedBaseUrl) {
      setFormError(t("validation.baseUrlRequired"));
      return;
    }

    const trimmedEmail = email.trim();
    if (showEmail && mode === "create" && !trimmedEmail) {
      setFormError(t("validation.emailRequired"));
      return;
    }

    const trimmedSecret = secret.trim();
    const secretRequired = mode === "create" || !integration?.hasSecret;
    if (secretRequired && !trimmedSecret) {
      setFormError(t("validation.secretRequired"));
      return;
    }

    try {
      if (mode === "create") {
        await onCreate({
          kind: activeKind,
          name: trimmedName,
          baseUrl: requiresBaseUrl ? trimmedBaseUrl : null,
          secret: buildIntegrationSecret(
            activeKind,
            trimmedSecret,
            trimmedEmail,
          ),
        });
      } else if (integration) {
        await onUpdate(integration.id, {
          name: trimmedName,
          baseUrl: requiresBaseUrl ? trimmedBaseUrl : null,
          secret: trimmedSecret
            ? buildIntegrationSecret(activeKind, trimmedSecret, trimmedEmail)
            : null,
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

  function handleTestConnection() {
    if (!integration) {
      return;
    }

    toastManager.promise(
      (async () => {
        const result = await onTest(integration.id);
        if (!result.success) {
          throw new Error(result.message ?? t("testConnectionFailed"));
        }
        return result;
      })(),
      {
        loading: {
          title: t("testConnectionLoading"),
          description: t("testConnectionLoadingDescription"),
        },
        success: (result) => ({
          title: t("testConnectionSuccess"),
          description: result.message ?? t("testConnectionSuccessDescription"),
        }),
        error: (error: Error) => ({
          title: t("testConnectionFailed"),
          description: error.message || t("testConnectionFailedDescription"),
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

            {mode === "create" ? (
              <Field name="kind">
                <FieldLabel>{t("kind")}</FieldLabel>
                <Select
                  itemToStringValue={(item) => item.value}
                  items={kindItems}
                  onValueChange={(value) => {
                    if (value) {
                      setKind(value.value);
                    }
                  }}
                  value={kindItems.find((item) => item.value === kind) ?? null}
                >
                  <SelectTrigger>
                    <SelectValue placeholder={t("kindPlaceholder")} />
                  </SelectTrigger>
                  <SelectPopup>
                    {kindItems.map((item) => (
                      <SelectItem key={item.value} value={item}>
                        {item.label}
                      </SelectItem>
                    ))}
                  </SelectPopup>
                </Select>
              </Field>
            ) : (
              <Field name="kind">
                <FieldLabel>{t("kind")}</FieldLabel>
                <Input disabled readOnly value={t(`kinds.${activeKind}`)} />
              </Field>
            )}

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

            {requiresBaseUrl ? (
              <Field name="baseUrl">
                <FieldLabel htmlFor={baseUrlId}>
                  {t("baseUrl")} <span aria-hidden="true">*</span>
                </FieldLabel>
                <Input
                  id={baseUrlId}
                  name="baseUrl"
                  onChange={(event) => setBaseUrl(event.target.value)}
                  placeholder={t("baseUrlPlaceholder")}
                  required
                  type="url"
                  value={baseUrl}
                />
              </Field>
            ) : null}

            {showEmail ? (
              <Field name="email">
                <FieldLabel htmlFor={emailId}>
                  {t("jiraEmail")} <span aria-hidden="true">*</span>
                </FieldLabel>
                <Input
                  autoComplete="email"
                  id={emailId}
                  name="email"
                  onChange={(event) => setEmail(event.target.value)}
                  required={mode === "create"}
                  type="email"
                  value={email}
                />
                {mode === "edit" ? (
                  <FieldDescription>{t("jiraEmailEditHint")}</FieldDescription>
                ) : null}
              </Field>
            ) : null}

            <Field name="secret">
              <FieldLabel htmlFor={secretId}>
                {secretLabel}{" "}
                {mode === "create" || !integration?.hasSecret ? (
                  <span aria-hidden="true">*</span>
                ) : null}
              </FieldLabel>
              {mode === "edit" && integration?.hasSecret ? (
                <div className="flex items-center gap-2">
                  <Badge variant="success">{t("secretConfigured")}</Badge>
                </div>
              ) : null}
              <Input
                autoComplete="new-password"
                id={secretId}
                name="secret"
                onChange={(event) => setSecret(event.target.value)}
                placeholder={
                  mode === "edit" && integration?.hasSecret
                    ? t("secretPlaceholderEdit")
                    : t("secretPlaceholder")
                }
                required={mode === "create" || !integration?.hasSecret}
                type="password"
                value={secret}
              />
              {mode === "edit" && integration?.hasSecret ? (
                <FieldDescription>{t("secretEditHint")}</FieldDescription>
              ) : null}
            </Field>
          </SheetPanel>
          <SheetFooter variant="bare">
            {mode === "edit" && integration ? (
              <Button
                onClick={handleTestConnection}
                type="button"
                variant="outline"
              >
                {t("testConnection")}
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
