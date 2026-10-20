"use client";

import { CheckIcon } from "lucide-react";
import { useTranslations } from "next-intl";

import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import type { NotificationFilter } from "@/lib/types";

// Keep asText and inputType here because they support control rendering,
// rather than placing them with the field definitions.
import { type FieldDef, type FieldKind, SEVERITY_OPTIONS } from "./channel-fields";

// Render a configuration value as text suitable for an input. Values from JSON
// may be strings, numbers, booleans, arrays, or null; buildConfig handles
// their final serialization.
function asText(v: unknown): string {
  if (typeof v === "string") return v;
  if (v === null || v === undefined) return "";
  return String(v);
}

// Map a field kind to an input type.
function inputType(kind: FieldKind): "text" | "password" | "number" {
  if (kind === "password") return "password";
  if (kind === "number") return "number";
  return "text";
}

// Render the control specified by the field definition.
//
// For masked fields, show a saved hint instead of the masked value in the
// input. Then text in the input always comes from the user, and an empty input
// means an empty value. Showing "__masked__:…abc123" inside the input could
// look like placeholder text to remove and lead to accidental credential loss.
//
// Resolve the notifyPage i18n keys in def.label, def.help, and
// def.options[].label here with t() (see channel-fields.ts).
export function ConfigField({
  def,
  value,
  isSecret,
  onChange,
}: {
  def: FieldDef;
  value: unknown;
  isSecret: boolean;
  onChange: (v: unknown) => void;
}) {
  const t = useTranslations("notifyPage");
  const id = `n-cfg-${def.key}`;
  const raw = asText(value);
  // A masked value from the backend looks like "__masked__:…abc123";
  // its suffix is a recognizable fragment of the original value.
  const masked = isSecret && raw.startsWith("__masked__");
  const maskedTail = masked ? (raw.split("…")[1] ?? "") : "";

  if (def.kind === "switch") {
    return (
      <div className="flex items-center gap-2 text-sm">
        <Switch checked={value === true} onCheckedChange={onChange} aria-label={t(def.label)} />
        {t(def.label)}
        {def.help && <span className="text-muted-foreground">({t(def.help)})</span>}
      </div>
    );
  }

  if (def.kind === "select") {
    return (
      <div className="grid gap-2">
        <Label>{t(def.label)}</Label>
        <Select value={raw || def.options?.[0]?.value} onValueChange={onChange}>
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {(def.options ?? []).map((o) => (
              <SelectItem key={o.value} value={o.value}>
                {t(o.label)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
    );
  }

  // Select among four controls with an if chain; nested ternaries would be
  // harder to read here.
  function control() {
    if (def.kind === "textarea" || def.kind === "kv") {
      return (
        <Textarea
          id={id}
          className="font-mono"
          placeholder={def.placeholder}
          value={masked ? "" : raw}
          onChange={(e) => onChange(e.target.value)}
        />
      );
    }
    if (def.kind === "list") {
      return (
        <Input
          id={id}
          value={Array.isArray(value) ? (value as string[]).join(", ") : raw}
          onChange={(e) => onChange(e.target.value)}
          placeholder={def.placeholder}
        />
      );
    }
    return (
      <Input
        id={id}
        className={def.kind === "text" ? "font-mono" : ""}
        type={inputType(def.kind)}
        placeholder={def.placeholder}
        value={masked ? "" : raw}
        onChange={(e) => onChange(e.target.value)}
      />
    );
  }

  const hint = masked ? (
    <p className="text-muted-foreground flex items-center gap-1 text-xs">
      <CheckIcon className="size-3" />
      {maskedTail ? t("masked.savedTail", { tail: maskedTail }) : t("masked.saved")}
    </p>
  ) : (
    def.help && <p className="text-muted-foreground text-xs">{t(def.help)}</p>
  );

  return (
    <div className="grid gap-2">
      <Label htmlFor={id}>{t(def.label)}</Label>
      {control()}
      {hint}
    </div>
  );
}

// Summarize filters in one line so a collapsed card shows what the channel sends.
export function FilterSummary({ filter }: { filter: NotificationFilter }) {
  const t = useTranslations("notifyPage");
  const parts: string[] = [];
  if (filter.min_severity) {
    const opt = SEVERITY_OPTIONS.find((o) => o.value === filter.min_severity);
    parts.push(opt ? t(opt.label) : filter.min_severity);
  }
  if (filter.vulnclass_include?.length) parts.push(t("summary.includeCount", { n: filter.vulnclass_include.length }));
  if (filter.vulnclass_exclude?.length) parts.push(t("summary.excludeCount", { n: filter.vulnclass_exclude.length }));
  if (filter.task_ids?.length) parts.push(t("summary.taskCount", { n: filter.task_ids.length }));
  if (filter.asset_ids?.length) parts.push(t("summary.assetCount", { n: filter.asset_ids.length }));
  if (filter.on_status_change) parts.push(t("summary.statusChange"));
  if (parts.length === 0) {
    return <p className="text-muted-foreground text-sm">{t("summary.all")}</p>;
  }
  return <p className="text-muted-foreground text-sm">{parts.join(" · ")}</p>;
}
