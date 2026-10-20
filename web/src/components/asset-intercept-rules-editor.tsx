"use client";

import * as React from "react";

import { PlusIcon, Trash2Icon } from "lucide-react";
import { useTranslations } from "next-intl";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import type { AssetInterceptKind, AssetInterceptRuleInput } from "@/lib/types";

// Use NativeSelect (a native <select>) instead of shadcn Select inside the Sheet drawer.
// Shadcn's dropdown portals to body, so an outside click can mistakenly close the drawer; a native dropdown avoids this.
// label 은 렌더 시점에 t(`kind.${value}`) 로 해석(ASSET_INTERCEPT_KIND_OPTIONS 외부 소비처 없음).
export const ASSET_INTERCEPT_KIND_OPTIONS: {
  value: AssetInterceptKind;
  placeholder: string;
}[] = [
  { value: "exact_domain", placeholder: "example.gov.cn" },
  { value: "exact_ip", placeholder: "203.0.113.10" },
  { value: "exact_url", placeholder: "https://example.com/login" },
  { value: "fuzzy_domain", placeholder: ".gov.cn" },
  { value: "fuzzy_ip", placeholder: "203.0.113." },
  { value: "fuzzy_url", placeholder: "/admin" },
  { value: "cidr", placeholder: "192.168.0.0/16" },
];

// AssetInterceptRulesEditor is a controlled multi-row editor for block/allow rules
// (action, match type, pattern, and note). The parent decides when to persist changes.
export function AssetInterceptRulesEditor({
  value,
  onChange,
}: {
  value: AssetInterceptRuleInput[];
  onChange: (v: AssetInterceptRuleInput[]) => void;
}) {
  const t = useTranslations("assetInterceptRules");
  function update(i: number, patch: Partial<AssetInterceptRuleInput>) {
    onChange(value.map((r, idx) => (idx === i ? { ...r, ...patch } : r)));
  }
  function remove(i: number) {
    onChange(value.filter((_, idx) => idx !== i));
  }
  function add() {
    onChange([...value, { action: "block", kind: "fuzzy_domain", pattern: "", note: "", enabled: true }]);
  }
  return (
    <div className="grid gap-2">
      {value.map((r, i) => {
        const ph = ASSET_INTERCEPT_KIND_OPTIONS.find((o) => o.value === r.kind)?.placeholder ?? "";
        return (
          // biome-ignore lint/suspicious/noArrayIndexKey: Rows have no stable ID; indexing is sufficient here.
          <div key={i} className="flex items-center gap-2">
            <NativeSelect
              size="sm"
              className="w-[84px] shrink-0"
              value={r.action}
              onChange={(e) => update(i, { action: e.target.value as "block" | "allow" })}
            >
              <NativeSelectOption value="block">{t("action.block")}</NativeSelectOption>
              <NativeSelectOption value="allow">{t("action.allow")}</NativeSelectOption>
            </NativeSelect>
            <NativeSelect
              size="sm"
              className="w-[120px] shrink-0"
              value={r.kind}
              onChange={(e) => update(i, { kind: e.target.value as AssetInterceptKind })}
            >
              {ASSET_INTERCEPT_KIND_OPTIONS.map((o) => (
                <NativeSelectOption key={o.value} value={o.value}>
                  {t(`kind.${o.value}`)}
                </NativeSelectOption>
              ))}
            </NativeSelect>
            <Input
              className="flex-1"
              placeholder={ph}
              value={r.pattern}
              onChange={(e) => update(i, { pattern: e.target.value })}
            />
            <Input
              className="w-[120px] shrink-0"
              placeholder={t("notePlaceholder")}
              value={r.note}
              onChange={(e) => update(i, { note: e.target.value })}
            />
            <Button
              type="button"
              size="icon"
              variant="ghost"
              className="text-destructive hover:text-destructive size-8 shrink-0"
              onClick={() => remove(i)}
            >
              <Trash2Icon className="size-4" />
            </Button>
          </div>
        );
      })}
      <Button type="button" size="sm" variant="outline" className="w-fit" onClick={add}>
        <PlusIcon className="size-4" /> {t("add")}
      </Button>
    </div>
  );
}
