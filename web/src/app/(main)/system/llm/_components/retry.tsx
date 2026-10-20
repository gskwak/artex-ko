"use client";

// Shared LLM retry settings: attempt count and interval for each of five layers.
//
// From innermost to outermost: connection (SDK) → empty response (SDK) → same-provider safety
// window → pool circuit breaker → intent rerun. The first three follow an endpoint, so each
// model profile can override the global defaults. The last two are process-wide.
//
// All inputs use the same "empty = unset" semantics as backend db.RetryRule:
//   Attempts: empty/0 = built-in default | -1 = disable this layer | >0 = use this count
//   Interval: empty/0 = existing exponential backoff | >0 = fixed interval in milliseconds

import * as React from "react";

import { Loader2Icon, SaveIcon } from "lucide-react";
import { useTranslations } from "next-intl";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api } from "@/lib/api";
import type { LLMRetryOverride, LLMRetryPolicy, LLMRetryRule } from "@/lib/types";

export const ZERO_RULE: LLMRetryRule = { attempts: 0, interval_ms: 0 };
export const ZERO_OVERRIDE: LLMRetryOverride = {
  connect: ZERO_RULE,
  empty: ZERO_RULE,
  stream: ZERO_RULE,
};
const ZERO_POLICY: LLMRetryPolicy = {
  ...ZERO_OVERRIDE,
  breaker: ZERO_RULE,
  intent: ZERO_RULE,
};

// Default attempt counts for placeholders only. Titles, triggers, descriptions, and other text
// use i18n (llmPage.retryLayer.<layer>.*) and render in the current language.
export const RETRY_LAYERS = {
  connect: { defAttempts: 3 },
  empty: { defAttempts: 2 },
  stream: { defAttempts: 2 },
  breaker: { defAttempts: 3 },
  intent: { defAttempts: 2 },
} satisfies Record<string, { defAttempts: number }>;

type LayerKey = keyof typeof RETRY_LAYERS;

/** Human-readable milliseconds shown beside the input so users need not count zeroes. */
function humanMs(ms: number) {
  if (!Number.isFinite(ms) || ms <= 0) return "";
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60_000) return `${Number((ms / 1000).toFixed(2))}s`;
  return `${Number((ms / 60_000).toFixed(2))}min`;
}

/** Controlled number input: empty string ↔ 0. Keep intermediate values ("-", "1e") locally. */
function NumField({
  id,
  value,
  onChange,
  placeholder,
  min,
}: {
  id: string;
  value: number;
  onChange: (n: number) => void;
  placeholder: string;
  min: number;
}) {
  const [text, setText] = React.useState(value === 0 ? "" : String(value));
  // Follow complete value changes from the parent (loading a policy or switching profiles).
  // Local typing leaves value equal to the parsed text, so it does not trigger this update.
  React.useEffect(() => {
    const incoming = value === 0 ? "" : String(value);
    setText((cur) => (Number(cur || 0) === value ? cur : incoming));
  }, [value]);
  return (
    <Input
      id={id}
      type="number"
      min={min}
      className="w-28 shrink-0"
      value={text}
      placeholder={placeholder}
      onChange={(e) => {
        setText(e.target.value);
        const n = Number(e.target.value);
        onChange(e.target.value.trim() === "" || !Number.isFinite(n) ? 0 : Math.trunc(n));
      }}
    />
  );
}

/** Controls for one retry layer. idPrefix keeps label htmlFor values unique on the page. */
export function RetryRuleFields({
  layer,
  idPrefix,
  value,
  onChange,
  compact,
}: {
  layer: LayerKey;
  idPrefix: string;
  value: LLMRetryRule;
  onChange: (r: LLMRetryRule) => void;
  /** true = compact profile-sheet version with only the error trigger, without details. */
  compact?: boolean;
}) {
  const t = useTranslations("llmPage");
  const meta = RETRY_LAYERS[layer];
  const human = humanMs(value.interval_ms);
  return (
    <div className={compact ? "grid gap-2" : "grid gap-3 rounded-lg border p-3"}>
      <div className="grid gap-0.5">
        <div className="flex flex-wrap items-baseline gap-2">
          <Label className="text-sm">{t(`retryLayer.${layer}.title`)}</Label>
          <span className="text-muted-foreground text-xs">{t(`retryLayer.${layer}.where`)}</span>
        </div>
        {/* Show which errors reach this layer, down to status codes, to explain when its controls take effect. */}
        <p className="text-muted-foreground text-xs">
          <span className="font-medium text-foreground">{t("retry.triggerLabel")}</span>:{" "}
          {t(`retryLayer.${layer}.trigger`)}
        </p>
        {!compact && (
          <p className="text-muted-foreground text-xs">
            <span className="font-medium text-foreground">{t("retry.skipsLabel")}</span>:{" "}
            {t(`retryLayer.${layer}.skips`)}
          </p>
        )}
        {!compact && <p className="text-muted-foreground text-xs">{t(`retryLayer.${layer}.desc`)}</p>}
      </div>
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <div className="flex items-center gap-2">
          <Label htmlFor={`${idPrefix}-${layer}-n`} className="text-muted-foreground text-xs">
            {t(`retryLayer.${layer}.attemptsLabel`)}
          </Label>
          <NumField
            id={`${idPrefix}-${layer}-n`}
            min={-1}
            value={value.attempts}
            placeholder={t("retry.defaultN", { n: meta.defAttempts })}
            onChange={(n) => onChange({ ...value, attempts: n })}
          />
        </div>
        <div className="flex items-center gap-2">
          <Label htmlFor={`${idPrefix}-${layer}-ms`} className="text-muted-foreground text-xs">
            {t("retry.intervalMs")}
          </Label>
          <NumField
            id={`${idPrefix}-${layer}-ms`}
            min={0}
            value={value.interval_ms}
            placeholder={t("retry.defaultBackoff")}
            onChange={(n) => onChange({ ...value, interval_ms: n })}
          />
          <span className="text-muted-foreground text-xs">
            {human ? t("retry.fixed", { human }) : t(`retryLayer.${layer}.defInterval`)}
          </span>
        </div>
      </div>
      {!compact && (
        <p className="text-muted-foreground text-xs">
          {t("retry.emptyLine", { hint: t(`retryLayer.${layer}.offHint`) })}
        </p>
      )}
    </div>
  );
}

/** Overrides for the three endpoint-specific layers in the model configuration sheet. */
export function ProfileRetryFields({
  value,
  onChange,
}: {
  value: LLMRetryOverride;
  onChange: (o: LLMRetryOverride) => void;
}) {
  const t = useTranslations("llmPage");
  return (
    <div className="grid gap-3 rounded-lg border p-3">
      <div className="grid gap-0.5">
        <Label className="text-sm">{t("retry.override.label")}</Label>
        <p className="text-muted-foreground text-xs">{t("retry.override.hint")}</p>
      </div>
      {(["connect", "empty", "stream"] as const).map((k) => (
        <div key={k} className="border-t pt-3 first:border-t-0 first:pt-0">
          <RetryRuleFields
            compact
            layer={k}
            idPrefix="pf"
            value={value[k]}
            onChange={(r) => onChange({ ...value, [k]: r })}
          />
        </div>
      ))}
    </div>
  );
}

/** Global defaults for all five layers in the Retry and Backoff tab. */
export function RetryPolicyPanel() {
  const t = useTranslations("llmPage");
  const [policy, setPolicy] = React.useState<LLMRetryPolicy>(ZERO_POLICY);
  const [loading, setLoading] = React.useState(true);
  const [saving, setSaving] = React.useState(false);

  const load = React.useCallback(async () => {
    setLoading(true);
    try {
      const p = await api.llmRetryPolicy();
      setPolicy({ ...ZERO_POLICY, ...p });
    } catch (e) {
      toast.error(t("retry.toast.loadFailed", { msg: (e as Error).message }));
    } finally {
      setLoading(false);
    }
  }, [t]);

  React.useEffect(() => {
    void load();
  }, [load]);

  async function save() {
    if (saving) return;
    setSaving(true);
    try {
      // The backend clamps out-of-range values and returns them. Refresh from its response
      // so the displayed values match what was saved.
      const saved = await api.saveLLMRetryPolicy(policy);
      setPolicy({ ...ZERO_POLICY, ...saved });
      toast.success(t("retry.toast.saved"));
    } catch (e) {
      toast.error(t("retry.toast.saveFailed", { msg: (e as Error).message }));
    } finally {
      setSaving(false);
    }
  }

  const set = (k: LayerKey) => (r: LLMRetryRule) => setPolicy((p) => ({ ...p, [k]: r }));

  if (loading) {
    return (
      <div className="flex items-center gap-2 rounded-lg border border-dashed p-10 text-muted-foreground text-sm">
        <Loader2Icon className="size-4 animate-spin" /> {t("retry.loading")}
      </div>
    );
  }

  return (
    <div className="grid gap-4">
      <div className="rounded-lg border bg-muted/30 p-3 text-muted-foreground text-xs leading-relaxed">
        {t.rich("retry.intro", {
          chain: (c) => <span className="text-foreground">{c}</span>,
          emph: (c) => <span className="text-foreground">{c}</span>,
        })}
      </div>

      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        {(Object.keys(RETRY_LAYERS) as LayerKey[]).map((k) => (
          <RetryRuleFields key={k} layer={k} idPrefix="gl" value={policy[k]} onChange={set(k)} />
        ))}
      </div>

      <div className="flex gap-2">
        <Button onClick={save} disabled={saving}>
          {saving ? <Loader2Icon className="animate-spin" /> : <SaveIcon />}
          {t("retry.save")}
        </Button>
        <Button variant="outline" onClick={() => setPolicy(ZERO_POLICY)} disabled={saving}>
          {t("retry.resetAll")}
        </Button>
      </div>
    </div>
  );
}
