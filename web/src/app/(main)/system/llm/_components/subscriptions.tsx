"use client";

import * as React from "react";

import { useTranslations } from "next-intl";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { api } from "@/lib/api";

export function SubscriptionConnections() {
  const t = useTranslations("llmSubscriptions");
  const [status, setStatus] = React.useState<Awaited<ReturnType<typeof api.subscriptionStatus>> | null>(null);
  const [busy, setBusy] = React.useState(false);
  const [loginUrl, setLoginUrl] = React.useState("");
  const refresh = React.useCallback(async () => {
    try {
      setStatus(await api.subscriptionStatus());
    } catch (e) {
      toast.error((e as Error).message);
    }
  }, []);
  React.useEffect(() => {
    void refresh();
  }, [refresh]);
  React.useEffect(() => {
    if (!status?.chatgpt.pending) return;
    const timer = setInterval(() => void refresh(), 3000);
    return () => clearInterval(timer);
  }, [status?.chatgpt.pending, refresh]);
  async function action(kind: string, accountId = "") {
    setBusy(true);
    try {
      const result = await api.subscriptionAction(kind, accountId);
      if (result.url) setLoginUrl(result.url);
      else setLoginUrl("");
      if (kind === "disconnect" && result.revoked === false) toast.warning(t("revocationWarning"));
      await refresh();
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <Card className="mb-4">
      <CardContent className="grid gap-4 pt-4">
        <div className="flex items-center justify-between gap-3">
          <h2 className="font-semibold">{t("title")}</h2>
          <Button variant="outline" size="sm" onClick={() => void refresh()}>
            {t("refresh")}
          </Button>
        </div>
        <p className="text-muted-foreground text-sm">{t("steps")}</p>
        <div className="grid gap-4 md:grid-cols-2">
          <section className="grid content-start gap-2 rounded-md border p-3">
            <h3 className="font-medium">ChatGPT</h3>
            <p className="text-muted-foreground text-sm">{t("chatgptHint")}</p>
            <p className="text-sm" role="status">
              {status?.chatgpt.connected ? t("connected") : t("disconnected")}
            </p>
            {status?.chatgpt.accounts.map((a) => (
              <div key={a.id} className="grid gap-1 rounded border p-2 text-xs">
                <span className="break-all">
                  {a.email || a.id} · {a.id.slice(-8)} {status.chatgpt.active === a.id ? `· ${t("active")}` : ""}
                </span>
                <div className="flex flex-wrap gap-2">
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={busy || status.chatgpt.pending}
                    onClick={() => void action("connect", a.id)}
                  >
                    {t("reauthorize")}
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={busy || !a.connected || status.chatgpt.active === a.id}
                    onClick={() => void action("select", a.id)}
                  >
                    {t("select")}
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={busy || !a.connected || status.chatgpt.pending}
                    onClick={() => void action("disconnect", a.id)}
                  >
                    {t("disconnect")}
                  </Button>
                </div>
              </div>
            ))}
            <Button disabled={busy || status?.chatgpt.pending} onClick={() => void action("connect")}>
              Continue with ChatGPT
            </Button>
            {status?.chatgpt.pending && (
              <div className="grid gap-2">
                {loginUrl && (
                  <a className="text-sm underline" href={loginUrl} target="_blank" rel="noreferrer">
                    {t("openLogin")}
                  </a>
                )}
                <p className="text-sm">{t("pending")}</p>
                <Button size="sm" variant="outline" disabled={busy} onClick={() => void action("cancel")}>
                  {t("cancel")}
                </Button>
              </div>
            )}
            {status?.chatgpt.error && (
              <p className="text-destructive text-sm" role="alert">
                {status.chatgpt.error}
              </p>
            )}
            <a className="text-sm underline" href="https://chatgpt.com/settings/usage" target="_blank" rel="noreferrer">
              {t("usage")}
            </a>
          </section>
          <section className="grid content-start gap-2 rounded-md border p-3">
            <h3 className="font-medium">Claude Code</h3>
            <p className="text-muted-foreground text-sm">{t("claudeHint")}</p>
            <p className="text-sm" role="status">
              {status?.claude.connected ? `${t("connected")} ${status.claude.email || ""}` : t("disconnected")}
            </p>
            {status && !status.claude.installed && <p className="text-sm">{t("install")}</p>}
            <p className="text-sm">{t("claudeLogin")}</p>
            <code className="overflow-x-auto rounded bg-muted p-2 text-xs">
              docker compose exec artex claude auth login
            </code>
            <p className="text-muted-foreground text-xs">{t("claudeLocal")}</p>
            <p className="text-muted-foreground text-xs">{t("claudeLogout")}</p>
            <a
              className="text-sm underline"
              href="https://code.claude.com/docs/en/authentication"
              target="_blank"
              rel="noreferrer"
            >
              {t("guide")}
            </a>
          </section>
        </div>
      </CardContent>
    </Card>
  );
}
