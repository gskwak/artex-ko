// Channel field definitions and configuration-value parsers.
//
// This data module is separate from the page because it describes each
// channel's fields, their controls, and conversion between form text and JSON.
// Adding a channel only requires changes here, not in the page.
//
// This is a plain data module, not a client component, so it cannot call
// useTranslations. It stores i18n keys in the notifyPage namespace rather
// than hard-coded copy. The rendering components resolve them with t().
// Channel-kind display names use notifyPage.kind.<kind>.

// Configuration fields for each channel kind.
//
// Keep a frontend field table instead of fetching a schema from the backend:
// backend validation covers required fields and formats, while the UI needs
// layout and control types. The only shared detail is secret_keys, supplied by
// the backend because each channel knows which values are credentials (the
// entire WeCom webhook is secret; for DingTalk only its secret is). A missing
// entry for a new channel leaves an empty form and surfaces via hasFields.
//
// label, help, and options[].label hold notifyPage i18n keys.
// Placeholders remain literal URLs or examples.
export type FieldKind = "text" | "password" | "number" | "select" | "textarea" | "switch" | "kv" | "list";
export interface FieldDef {
  key: string;
  /** i18n key in the notifyPage namespace */
  label: string;
  kind: FieldKind;
  placeholder?: string;
  /** i18n key in the notifyPage namespace */
  help?: string;
  /** options[].label is also an i18n key */
  options?: { value: string; label: string }[];
}
export const CHANNEL_FIELDS: Record<string, FieldDef[]> = {
  dingtalk: [
    {
      key: "webhook",
      label: "field.dingtalk.webhook.label",
      kind: "text",
      placeholder: "https://oapi.dingtalk.com/robot/send?access_token=...",
    },
    {
      key: "secret",
      label: "field.dingtalk.secret.label",
      kind: "password",
      help: "field.dingtalk.secret.help",
    },
  ],
  feishu: [
    {
      key: "webhook",
      label: "field.feishu.webhook.label",
      kind: "text",
      placeholder: "https://open.feishu.cn/open-apis/bot/v2/hook/...",
    },
    { key: "secret", label: "field.feishu.secret.label", kind: "password", help: "field.feishu.secret.help" },
  ],
  wecom: [
    {
      key: "webhook",
      label: "field.wecom.webhook.label",
      kind: "text",
      placeholder: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=...",
    },
  ],
  webhook: [
    {
      key: "url",
      label: "field.webhook.url.label",
      kind: "text",
      placeholder: "https://your-endpoint.example.com/hook",
    },
    {
      key: "method",
      label: "field.webhook.method.label",
      kind: "select",
      options: [
        { value: "POST", label: "field.webhook.method.opt.POST" },
        { value: "PUT", label: "field.webhook.method.opt.PUT" },
        { value: "PATCH", label: "field.webhook.method.opt.PATCH" },
        { value: "GET", label: "field.webhook.method.opt.GET" },
      ],
    },
    { key: "headers", label: "field.webhook.headers.label", kind: "kv", help: "field.webhook.headers.help" },
    {
      key: "body_template",
      label: "field.webhook.body_template.label",
      kind: "textarea",
      help: "field.webhook.body_template.help",
    },
  ],
  telegram: [
    { key: "bot_token", label: "field.telegram.bot_token.label", kind: "password", placeholder: "123456:ABC-DEF..." },
    { key: "chat_id", label: "field.telegram.chat_id.label", kind: "text", placeholder: "-1001234567890" },
    {
      key: "base_url",
      label: "field.telegram.base_url.label",
      kind: "text",
      placeholder: "https://api.telegram.org",
      help: "field.telegram.base_url.help",
    },
  ],
  email: [
    { key: "host", label: "field.email.host.label", kind: "text", placeholder: "smtp.example.com" },
    {
      key: "port",
      label: "field.email.port.label",
      kind: "number",
      placeholder: "587",
      help: "field.email.port.help",
    },
    { key: "username", label: "field.email.username.label", kind: "text" },
    { key: "password", label: "field.email.password.label", kind: "password" },
    { key: "from", label: "field.email.from.label", kind: "text", placeholder: "artex@example.com" },
    { key: "to", label: "field.email.to.label", kind: "list", help: "field.email.to.help" },
    { key: "tls", label: "field.email.tls.label", kind: "switch", help: "field.email.tls.help" },
  ],
};

// value is the min_severity filter value; label is a notifyPage.severityOpt.* i18n key.
export const SEVERITY_OPTIONS = [
  { value: "", label: "severityOpt.all" },
  { value: "low", label: "severityOpt.low" },
  { value: "medium", label: "severityOpt.medium" },
  { value: "high", label: "severityOpt.high" },
  { value: "critical", label: "severityOpt.critical" },
];

export type ChannelForm = {
  name: string;
  kind: string;
  mode: "realtime" | "digest";
  enabled: boolean;
  ratePerMin: string;
  config: Record<string, unknown>;
  minSeverity: string;
  includeText: string;
  excludeText: string;
  taskIDsText: string;
  assetIDsText: string;
  onStatusChange: boolean;
};

export const emptyForm = (kind: string): ChannelForm => ({
  name: "",
  kind,
  mode: "realtime",
  enabled: true,
  ratePerMin: "",
  config: {},
  minSeverity: "",
  includeText: "",
  excludeText: "",
  taskIDsText: "",
  assetIDsText: "",
  onStatusChange: false,
});

// Parse a text area containing one KEY=VALUE pair per line.
export function parseKV(text: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const line of text.split("\n")) {
    const t = line.trim();
    if (!t) continue;
    const i = t.indexOf("=");
    if (i > 0) out[t.slice(0, i).trim()] = t.slice(i + 1).trim();
  }
  return out;
}
// Parse IDs separated by commas or whitespace. Keep the full-width comma as
// a delimiter because users may paste lists containing it from elsewhere.
export function parseIDs(text: string): number[] {
  return text
    .split(/[\s,，]+/)
    .map((s) => s.trim())
    .filter(Boolean)
    .map((s) => Number(s))
    .filter((n) => Number.isFinite(n) && n > 0);
}
// Parse keywords separated by lines or commas. Vulnerability class names may
// contain spaces, so whitespace is not a delimiter. Keep full-width commas
// as input delimiters as well.
export function parseKeywords(text: string): string[] {
  return text
    .split(/[\n,，]+/)
    .map((s) => s.trim())
    .filter(Boolean);
}
